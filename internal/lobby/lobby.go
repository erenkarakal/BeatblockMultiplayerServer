package lobby

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"bb-mp-server/internal/protocol"
)

const (
	stateLobby   = "lobby"
	statePlaying = "playing"

	statusLobby        = "lobby"
	statusPlaying      = "playing"
	statusFinished     = "finished"
	statusDNF          = "dnf"
	statusDisconnected = "disconnected"

	maxNameLen          = 24
	maxLobbyCodeLen     = 16
	maxChatLen          = 300
	chatBurst           = 5.0
	chatRefillPerSecond = 1.0 / 1.5
	chatHistoryLimit    = 100

	beginDelayMS = 3000
	loadTimeout  = 60 * time.Second
)

// Options configures a Manager and the lobbies it creates
type Options struct {
	MaxPlayers     int
	MaxConnections int
	RoundTimeout   time.Duration
	Verbose        bool
	Logger         *slog.Logger
}

// Player is a connected lobby member
type Player struct {
	ID      string
	Name    string
	SteamID string
	Joined  time.Time
}

// Round is an in progress match
type Round struct {
	ID           int
	Participants map[string]*RoundEntry
	Order        []string
	BeginSent    bool
	Timer        *time.Timer
	LoadTimer    *time.Timer
}

// RoundEntry is the server's per-player state during a round
// kept separately from Player so a disconnect does not lose a submitted score
type RoundEntry struct {
	ID       string
	Name     string
	Status   string
	Score    *protocol.Score
	PctGrade float64
	Loaded   bool
	PB       bool
}

// Lobby is one group of players with its own chat, map and round.
// The empty code is the default lobby, which always exists.
type Lobby struct {
	mu sync.Mutex

	code    string
	clients map[*Client]*Player
	order   []string
	hostID  string
	state   string
	mapInfo protocol.MapInfo

	chat  []protocol.ChatMessage
	round *Round

	maxPlayers   int
	roundTimeout time.Duration
	verbose      bool
	logger       *slog.Logger

	nextRoundID int64
	nextChatID  int64
}

func newLobby(code string, opts Options) *Lobby {
	return &Lobby{
		code:         code,
		clients:      make(map[*Client]*Player),
		state:        stateLobby,
		mapInfo:      defaultMap(),
		maxPlayers:   opts.MaxPlayers,
		roundTimeout: opts.RoundTimeout,
		verbose:      opts.Verbose,
		logger:       opts.Logger,
	}
}

// defaultMap is used until the host picks a level.
func defaultMap() protocol.MapInfo {
	return protocol.MapInfo{
		Path: "levels/Finished levels/grittedstrings/",
		Name: "Gritted Strings",
	}
}

func (l *Lobby) label() string {
	if l.code == "" {
		return "default"
	}
	return l.code
}

func (l *Lobby) logf(format string, args ...any) {
	l.logger.Info("[" + l.label() + "] " + fmt.Sprintf(format, args...))
}

func (l *Lobby) handle(c *Client, msg protocol.ClientMessage) {
	l.mu.Lock()
	defer l.mu.Unlock()

	switch msg.Type {
	case "pong":
		// lastSeen is already updated by the read loop.
	case "chat":
		l.chatLocked(c, msg.Text)
	case "start":
		l.startLocked(c)
	case "loaded":
		l.loadedLocked(c)
	case "load_error":
		l.loadErrorLocked(c, msg.Reason)
	case "progress":
		l.progressLocked(c, msg.PctGrade)
	case "finished":
		l.finishedLocked(c, msg.Score)
	case "quit_level":
		l.quitLevelLocked(c)
	case "set_map":
		l.setMapLocked(c, msg.Map)
	case "set_host":
		l.setHostLocked(c, msg.Target)
	case "kick":
		l.kickLocked(c, msg.Target)
	}
}

func (l *Lobby) chatLocked(c *Client, raw string) {
	now := time.Now()
	elapsed := now.Sub(c.chatLast).Seconds()
	c.chatTokens = math.Min(chatBurst, c.chatTokens+elapsed*chatRefillPerSecond)
	c.chatLast = now
	if c.chatTokens < 1 {
		sendError(c, "RATE_LIMITED", "Slow down a little.", false)
		return
	}
	c.chatTokens--

	text := sanitizeChat(raw)
	if text == "" {
		return
	}

	msg := protocol.ChatMessage{
		ID:       fmt.Sprintf("c%d", l.nextChatID),
		PlayerID: c.player.ID,
		Name:     c.player.Name,
		Text:     text,
		Time:     time.Now().UnixMilli(),
	}
	l.nextChatID++
	l.appendChatLocked(msg)
	l.broadcastLocked(protocol.ServerMessage{Type: "chat", Chat: &msg})

	if l.verbose {
		l.logf("chat %s: %s", c.player.Name, text)
	}
}

func (l *Lobby) startLocked(c *Client) {
	if c.player.ID != l.hostID {
		sendError(c, "NOT_HOST", "Only the host can start the game.", false)
		return
	}
	if l.state == statePlaying {
		sendError(c, "ALREADY_PLAYING", "A game is already in progress.", false)
		return
	}

	l.nextRoundID++
	round := &Round{
		ID:           int(l.nextRoundID),
		Participants: make(map[string]*RoundEntry, len(l.clients)),
		Order:        make([]string, 0, len(l.clients)),
	}

	players := make([]protocol.PlayerView, 0, len(l.clients))
	for _, id := range l.order {
		p := l.playerByIDLocked(id)
		if p == nil {
			continue
		}
		round.Participants[id] = &RoundEntry{
			ID:     id,
			Name:   p.Name,
			Status: statusPlaying,
		}
		round.Order = append(round.Order, id)
		players = append(players, protocol.PlayerView{
			ID:     id,
			Name:   p.Name,
			IsHost: id == l.hostID,
			Status: statusPlaying,
		})
	}

	l.round = round
	l.state = statePlaying

	l.broadcastLocked(protocol.ServerMessage{
		Type:    "game_start",
		RoundID: round.ID,
		Map:     &l.mapInfo,
		Players: players,
	})

	round.Timer = time.AfterFunc(l.roundTimeout, func() {
		l.roundTimedOut(round.ID)
	})
	round.LoadTimer = time.AfterFunc(loadTimeout, func() {
		l.roundLoadTimedOut(round.ID)
	})

	l.logf("round %d started: %s (%d players)", round.ID, l.mapInfo.Name, len(players))
}

func (l *Lobby) loadedLocked(c *Client) {
	round := l.round
	if l.state != statePlaying || round == nil {
		return
	}
	entry := round.Participants[c.player.ID]
	if entry == nil {
		return
	}
	entry.Loaded = true
	l.maybeBeginLocked()
}

func (l *Lobby) maybeBeginLocked() {
	round := l.round
	if l.state != statePlaying || round == nil || round.BeginSent {
		return
	}
	for _, e := range round.Participants {
		if !e.Loaded {
			return
		}
	}

	round.BeginSent = true
	if round.LoadTimer != nil {
		round.LoadTimer.Stop()
	}
	l.broadcastLocked(protocol.ServerMessage{
		Type: "begin",
		InMS: beginDelayMS,
	})
	l.logf("round %d: all players loaded, beginning countdown", round.ID)
}

func (l *Lobby) loadErrorLocked(c *Client, reason string) {
	reason = sanitizeChat(reason)
	if reason == "" {
		reason = "unknown error"
	}
	l.abortRoundLocked(fmt.Sprintf("%s could not load the map: %s", c.player.Name, reason))
}

func (l *Lobby) progressLocked(c *Client, pct float64) {
	round := l.round
	if l.state != statePlaying || round == nil {
		return
	}
	entry := round.Participants[c.player.ID]
	if entry == nil || entry.Status != statusPlaying {
		return
	}

	entry.PctGrade = clampFloat(pct, 0, 100)

	l.broadcastLocked(protocol.ServerMessage{
		Type:    "leaderboard",
		Entries: rankEntries(round),
	})
}

func (l *Lobby) finishedLocked(c *Client, score *protocol.Score) {
	round := l.round
	if l.state != statePlaying || round == nil || score == nil {
		return
	}
	entry := round.Participants[c.player.ID]
	if entry == nil || entry.Status != statusPlaying {
		return
	}

	sanitizeScore(score)
	entry.Status = statusFinished
	entry.Score = score
	entry.PctGrade = score.PctGrade
	entry.PB = score.PB

	l.broadcastLocked(protocol.ServerMessage{
		Type:    "leaderboard",
		Entries: rankEntries(round),
	})

	l.logf("finished: %s (%s) %.2f%%", entry.Name, entry.ID, score.PctGrade)
	l.maybeFinalizeLocked()
}

func (l *Lobby) quitLevelLocked(c *Client) {
	round := l.round
	if l.state != statePlaying || round == nil {
		return
	}
	entry := round.Participants[c.player.ID]
	if entry == nil || entry.Status != statusPlaying {
		return
	}

	entry.Status = statusDNF
	l.broadcastLocked(protocol.ServerMessage{
		Type:    "leaderboard",
		Entries: rankEntries(round),
	})
	l.maybeFinalizeLocked()
}

func (l *Lobby) setMapLocked(c *Client, m *protocol.MapInfo) {
	if c.player.ID != l.hostID {
		sendError(c, "NOT_HOST", "Only the host can change the map.", false)
		return
	}
	if l.state == statePlaying {
		sendError(c, "ALREADY_PLAYING", "The map cannot be changed during a round.", false)
		return
	}
	if m == nil || !validMapPath(m.Path) {
		sendError(c, "INVALID_MAP", "That map path is not allowed.", false)
		return
	}

	name := strings.TrimSpace(m.Name)
	if name == "" {
		name = strings.TrimSuffix(strings.TrimSuffix(m.Path, "/"), "\\")
	}
	if utf8.RuneCountInString(name) > 80 {
		name = truncateRunes(name, 80)
	}

	l.mapInfo = protocol.MapInfo{
		Path:    m.Path,
		Name:    name,
		Variant: strings.TrimSpace(m.Variant),
	}
	l.broadcastLobbyLocked()
	l.postSystemChatLocked(fmt.Sprintf("%s changed the map to %s", c.player.Name, l.mapInfo.Name))
}

func (l *Lobby) setHostLocked(c *Client, targetID string) {
	if c.player.ID != l.hostID {
		sendError(c, "NOT_HOST", "Only the host can pass host.", false)
		return
	}

	target := l.playerByIDLocked(targetID)
	if target == nil || target.ID == c.player.ID {
		sendError(c, "INVALID_TARGET", "That player is not in the lobby.", false)
		return
	}

	l.hostID = target.ID
	l.broadcastLobbyLocked()
	l.postSystemChatLocked(fmt.Sprintf("%s is now the host", target.Name))
}

func (l *Lobby) kickLocked(c *Client, targetID string) {
	if c.player.ID != l.hostID {
		sendError(c, "NOT_HOST", "Only the host can kick players.", false)
		return
	}
	if targetID == c.player.ID {
		sendError(c, "INVALID_TARGET", "You cannot kick yourself.", false)
		return
	}

	var targetClient *Client
	var targetName string
	for client, p := range l.clients {
		if p.ID == targetID {
			targetClient, targetName = client, p.Name
			break
		}
	}
	if targetClient == nil {
		sendError(c, "INVALID_TARGET", "That player is not in the lobby.", false)
		return
	}

	l.postSystemChatLocked(targetName + " was kicked from the lobby")
	sendError(targetClient, "KICKED", "You were kicked from the lobby.", true)
}

func (l *Lobby) roundTimedOut(id int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.round == nil || l.round.ID != id {
		return
	}
	for _, e := range l.round.Participants {
		if e.Status == statusPlaying {
			e.Status = statusDNF
		}
	}
	l.postSystemChatLocked("The round timed out.")
	l.maybeFinalizeLocked()
}

func (l *Lobby) roundLoadTimedOut(id int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.round == nil || l.round.ID != id || l.round.BeginSent {
		return
	}
	l.abortRoundLocked("Timed out waiting for players to load the map.")
}

func (l *Lobby) abortRoundLocked(reason string) {
	if l.state != statePlaying || l.round == nil {
		return
	}
	round := l.round
	l.state = stateLobby
	l.round = nil
	if round.Timer != nil {
		round.Timer.Stop()
	}
	if round.LoadTimer != nil {
		round.LoadTimer.Stop()
	}

	l.broadcastLocked(protocol.ServerMessage{
		Type:   "game_aborted",
		Reason: reason,
	})
	l.postSystemChatLocked("Round aborted: " + reason)
	l.broadcastLobbyLocked()
	l.logf("round %d aborted: %s", round.ID, reason)
}

func (l *Lobby) maybeFinalizeLocked() {
	if l.state != statePlaying || l.round == nil {
		return
	}
	for _, e := range l.round.Participants {
		if e.Status == statusPlaying {
			return
		}
	}

	round := l.round
	entries := rankEntries(round)
	text := formatResults(l.mapInfo.Name, entries)

	l.state = stateLobby
	l.round = nil
	if round.Timer != nil {
		round.Timer.Stop()
	}
	if round.LoadTimer != nil {
		round.LoadTimer.Stop()
	}

	l.broadcastLocked(protocol.ServerMessage{
		Type:    "level_end",
		Entries: entries,
		Text:    text,
	})
	l.postSystemChatLocked(text)
	l.broadcastLobbyLocked()

	l.logf("round %d finished", round.ID)
}

func (l *Lobby) addPlayerLocked(c *Client, name, steamID string) {
	player := &Player{
		ID:      c.id,
		Name:    name,
		SteamID: steamID,
		Joined:  time.Now(),
	}
	c.player = player
	l.clients[c] = player
	l.order = append(l.order, player.ID)
	if l.hostID == "" {
		l.hostID = player.ID
	}

	history := make([]protocol.ChatMessage, len(l.chat))
	copy(history, l.chat)

	c.enqueue(protocol.ServerMessage{
		Type:     "welcome",
		PlayerID: player.ID,
		Lobby:    l.snapshotLocked(),
		History:  history,
	})
	l.broadcastLobbyLocked()

	l.logf("joined: %s (%s) from %s [%d players]",
		player.Name, player.ID, c.remoteIP, len(l.clients))
}

// detaches c and reports whether the lobby is now empty
func (l *Lobby) removePlayerLocked(c *Client) bool {
	player := c.player
	if player == nil {
		return len(l.clients) == 0
	}
	c.player = nil

	delete(l.clients, c)
	for i, id := range l.order {
		if id == player.ID {
			l.order = append(l.order[:i], l.order[i+1:]...)
			break
		}
	}

	if l.round != nil {
		if entry := l.round.Participants[player.ID]; entry != nil {
			entry.Loaded = true
			if entry.Status == statusPlaying {
				entry.Status = statusDisconnected
			}
		}
	}

	if l.hostID == player.ID {
		l.reassignHostLocked()
	}

	l.broadcastLobbyLocked()
	l.maybeFinalizeLocked()
	l.maybeBeginLocked()

	l.logf("left: %s (%s) from %s [%d players]",
		player.Name, player.ID, c.remoteIP, len(l.clients))

	return len(l.clients) == 0
}

// stops timers and leaves the lobby in a joinable state.
func (l *Lobby) shutdownLocked() {
	l.state = stateLobby
	if l.round != nil {
		if l.round.Timer != nil {
			l.round.Timer.Stop()
		}
		if l.round.LoadTimer != nil {
			l.round.LoadTimer.Stop()
		}
		l.round = nil
	}
}

func (l *Lobby) reassignHostLocked() {
	var oldest *Player
	for _, p := range l.clients {
		if oldest == nil || p.Joined.Before(oldest.Joined) {
			oldest = p
		}
	}
	if oldest == nil {
		l.hostID = ""
		return
	}
	l.hostID = oldest.ID
}

func (l *Lobby) snapshotLocked() *protocol.LobbyView {
	players := make([]protocol.PlayerView, 0, len(l.clients))
	for _, id := range l.order {
		p := l.playerByIDLocked(id)
		if p == nil {
			continue
		}
		players = append(players, protocol.PlayerView{
			ID:      p.ID,
			Name:    p.Name,
			IsHost:  p.ID == l.hostID,
			Status:  l.playerStatusLocked(p.ID),
			SteamID: p.SteamID,
		})
	}
	m := l.mapInfo
	return &protocol.LobbyView{
		Code:    l.code,
		State:   l.state,
		HostID:  l.hostID,
		Map:     m,
		Players: players,
	}
}

func (l *Lobby) playerStatusLocked(id string) string {
	if l.state == statePlaying && l.round != nil {
		if entry := l.round.Participants[id]; entry != nil {
			return entry.Status
		}
	}
	return statusLobby
}

func (l *Lobby) playerByIDLocked(id string) *Player {
	for _, p := range l.clients {
		if p.ID == id {
			return p
		}
	}
	return nil
}

func (l *Lobby) nameTakenLocked(name string) bool {
	lower := strings.ToLower(name)
	for _, p := range l.clients {
		if strings.ToLower(p.Name) == lower {
			return true
		}
	}
	return false
}

func (l *Lobby) broadcastLobbyLocked() {
	l.broadcastLocked(protocol.ServerMessage{
		Type:  "lobby",
		Lobby: l.snapshotLocked(),
	})
}

func (l *Lobby) broadcastLocked(msg protocol.ServerMessage) {
	payload, err := json.Marshal(msg)
	if err != nil {
		return
	}
	for c := range l.clients {
		c.enqueuePayload(payload)
	}
}

func (l *Lobby) postSystemChatLocked(text string) {
	msg := protocol.ChatMessage{
		ID:     fmt.Sprintf("s%d", l.nextChatID),
		Name:   "SERVER",
		Text:   text,
		Time:   time.Now().UnixMilli(),
		System: true,
	}
	l.nextChatID++
	l.appendChatLocked(msg)
	l.broadcastLocked(protocol.ServerMessage{Type: "chat", Chat: &msg})
}

func (l *Lobby) appendChatLocked(msg protocol.ChatMessage) {
	l.chat = append(l.chat, msg)
	if len(l.chat) > chatHistoryLimit {
		l.chat = l.chat[len(l.chat)-chatHistoryLimit:]
	}
}

func sanitizeName(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.Join(strings.Fields(s), " ")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return truncateRunes(s, maxNameLen)
}

// uppercases a code and keeps letters, digits, '-' and '_'
func sanitizeLobbyCode(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(raw)) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			continue
		}
		if b.Len() >= maxLobbyCodeLen {
			break
		}
	}
	return b.String()
}

func sanitizeSteamID(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	id := b.String()
	if len(id) < 10 || len(id) > 20 {
		return ""
	}
	return id
}

func sanitizeChat(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	return truncateRunes(s, maxChatLen)
}

func truncateRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max])
}

func sanitizeScore(s *protocol.Score) {
	s.PctGrade = clampFloat(s.PctGrade, 0, 100)
	s.Misses = clampInt(s.Misses, 0, 10_000_000)
	s.Barelies = clampInt(s.Barelies, 0, 10_000_000)
	s.Perfects = clampInt(s.Perfects, 0, 10_000_000)
	s.MaxCombo = clampInt(s.MaxCombo, 0, 10_000_000)
	s.MaxHits = clampInt(s.MaxHits, 1, 10_000_000)

	s.Grade = strings.ToLower(strings.TrimSpace(s.Grade))
	if utf8.RuneCountInString(s.Grade) > 16 {
		s.Grade = truncateRunes(s.Grade, 16)
	}
	s.PlusMinus = strings.ToLower(strings.TrimSpace(s.PlusMinus))
	if utf8.RuneCountInString(s.PlusMinus) > 8 {
		s.PlusMinus = truncateRunes(s.PlusMinus, 8)
	}
}

func validMapPath(path string) bool {
	p := strings.TrimSpace(path)
	if p == "" || strings.Contains(p, "..") || strings.ContainsRune(p, '\\') {
		return false
	}
	lower := strings.ToLower(p)
	return strings.HasPrefix(lower, "levels/") ||
		strings.HasPrefix(lower, "custom levels/") ||
		strings.HasPrefix(lower, "workshop/")
}

func clampFloat(v, lo, hi float64) float64 {
	if math.IsNaN(v) || v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
