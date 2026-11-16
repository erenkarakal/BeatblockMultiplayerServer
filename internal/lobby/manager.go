package lobby

import (
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"bb-mp-server/internal/protocol"
)

const (
	defaultMaxPlayers   = 32
	defaultRoundTimeout = 15 * time.Minute
)

// owns every lobby and connection on the server
type Manager struct {
	mu      sync.Mutex
	lobbies map[string]*Lobby
	all     map[*Client]bool
	opts    Options
	nextID  int64
}

// normalises opts and creates the default lobby
func NewManager(opts Options) *Manager {
	if opts.MaxPlayers <= 0 {
		opts.MaxPlayers = defaultMaxPlayers
	}
	if opts.RoundTimeout <= 0 {
		opts.RoundTimeout = defaultRoundTimeout
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	m := &Manager{
		lobbies: make(map[string]*Lobby),
		all:     make(map[*Client]bool),
		opts:    opts,
	}
	m.lobbies[""] = newLobby("", opts)
	return m
}

func (m *Manager) nextIDLocked() string {
	id := fmt.Sprintf("p%d", m.nextID)
	m.nextID++
	return id
}

// registers a connection unless the server-wide cap is reached
func (m *Manager) attach(conn net.Conn) *Client {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.opts.MaxConnections > 0 && len(m.all) >= m.opts.MaxConnections {
		return nil
	}
	c := newClient(m.nextIDLocked(), conn, m)
	m.all[c] = true
	return c
}

func (m *Manager) handle(c *Client, msg protocol.ClientMessage) {
	if c.lobby == nil {
		if msg.Type == "pong" {
			return
		}
		if msg.Type != "hello" {
			sendError(c, "NOT_JOINED", "send hello first", false)
			return
		}
		m.join(c, msg)
		return
	}

	c.lobby.handle(c, msg)
}

func (m *Manager) join(c *Client, msg protocol.ClientMessage) {
	if msg.Proto != protocol.Version {
		sendError(c, "VERSION_MISMATCH", "This client is not compatible with this server.", true)
		return
	}

	name := sanitizeName(msg.Name)
	if name == "" {
		sendError(c, "INVALID_NAME", "Please choose a name between 1 and 24 characters.", true)
		return
	}
	code := sanitizeLobbyCode(msg.Lobby)
	steamID := sanitizeSteamID(msg.SteamID)

	m.mu.Lock()
	defer m.mu.Unlock()

	l := m.lobbies[code]
	if l == nil {
		l = newLobby(code, m.opts)
		m.lobbies[code] = l
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.clients) >= l.maxPlayers {
		sendError(c, "LOBBY_FULL", "That lobby is full.", true)
		return
	}
	if l.state == statePlaying {
		sendError(c, "GAME_IN_PROGRESS", "A game is already in progress in that lobby. Try again when it ends.", true)
		return
	}
	if l.nameTakenLocked(name) {
		sendError(c, "NAME_TAKEN", "That name is already in use in this lobby.", true)
		return
	}

	c.lobby = l
	l.addPlayerLocked(c, name, steamID)
}

func (m *Manager) removeClient(c *Client) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.all, c)

	l := c.lobby
	c.lobby = nil
	if l == nil {
		return
	}

	l.mu.Lock()
	empty := l.removePlayerLocked(c)
	if empty {
		l.shutdownLocked()
	}
	l.mu.Unlock()

	if empty && l.code != "" {
		delete(m.lobbies, l.code)
	}
}

// notifies connected clients and closes their connections
func (m *Manager) Shutdown() {
	m.mu.Lock()
	clients := make([]*Client, 0, len(m.all))
	for c := range m.all {
		clients = append(clients, c)
	}
	m.mu.Unlock()

	for _, c := range clients {
		c.enqueue(protocol.ServerMessage{
			Type:    "error",
			Code:    "SERVER_SHUTDOWN",
			Message: "The server is shutting down.",
		})
	}
	time.Sleep(100 * time.Millisecond)
	for _, c := range clients {
		c.close()
	}
}

func sendError(c *Client, code, message string, fatal bool) {
	c.enqueue(protocol.ServerMessage{
		Type:    "error",
		Code:    code,
		Message: message,
	})
	if fatal {
		go func() {
			time.Sleep(200 * time.Millisecond)
			c.close()
		}()
	}
}
