package lobby

import (
	"bufio"
	"encoding/json"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"bb-mp-server/internal/protocol"
)

const (
	writeQueueSize = 64
	pingInterval   = 20 * time.Second
	idleTimeout    = 90 * time.Second
	writeTimeout   = 10 * time.Second
)

// Client represents one TCP connection
type Client struct {
	id       string
	conn     net.Conn
	manager  *Manager
	lobby    *Lobby
	remoteIP string

	player *Player

	send      chan []byte
	done      chan struct{}
	closeOnce sync.Once
	lastSeen  atomic.Int64

	chatTokens float64
	chatLast   time.Time
}

func newClient(id string, conn net.Conn, m *Manager) *Client {
	c := &Client{
		id:         id,
		conn:       conn,
		manager:    m,
		remoteIP:   remoteHost(conn),
		send:       make(chan []byte, writeQueueSize),
		done:       make(chan struct{}),
		chatTokens: 5,
		chatLast:   time.Now(),
	}
	c.lastSeen.Store(time.Now().UnixNano())
	return c
}

func remoteHost(conn net.Conn) string {
	if conn == nil {
		return "?"
	}
	addr := conn.RemoteAddr()
	if addr == nil {
		return "?"
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}

func (c *Client) run() {
	go c.writeLoop()
	go c.heartbeatLoop()
	c.readLoop()
	c.close()
	c.manager.removeClient(c)
}

func (c *Client) readLoop() {
	r := bufio.NewReaderSize(c.conn, 16<<10)
	for {
		_ = c.conn.SetReadDeadline(time.Now().Add(idleTimeout))

		payload, err := protocol.ReadFrame(r)
		if err != nil {
			return
		}
		c.lastSeen.Store(time.Now().UnixNano())

		var msg protocol.ClientMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			continue
		}
		c.manager.handle(c, msg)
	}
}

func (c *Client) writeLoop() {
	w := bufio.NewWriterSize(c.conn, 16<<10)
	for {
		select {
		case <-c.done:
			return
		case payload := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := protocol.WriteFrame(w, payload); err != nil {
				c.close()
				return
			}
			if err := w.Flush(); err != nil {
				c.close()
				return
			}
		}
	}
}

func (c *Client) heartbeatLoop() {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			if time.Since(time.Unix(0, c.lastSeen.Load())) > idleTimeout {
				c.close()
				return
			}
			c.enqueue(protocol.ServerMessage{
				Type: "ping",
				T:    time.Now().UnixMilli(),
			})
		}
	}
}

func (c *Client) enqueue(msg protocol.ServerMessage) {
	payload, err := json.Marshal(msg)
	if err != nil {
		return
	}
	c.enqueuePayload(payload)
}

func (c *Client) enqueuePayload(payload []byte) {
	select {
	case <-c.done:
	case c.send <- payload:
	default:
		// the peer can't keep up. dropping it is better than blocking the hub
		c.close()
	}
}

func (c *Client) close() {
	c.closeOnce.Do(func() {
		close(c.done)
		_ = c.conn.Close()
	})
}
