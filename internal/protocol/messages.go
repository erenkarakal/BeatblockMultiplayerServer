package protocol

// protocol version
const Version = 4

// identifies the level that a round is played on
type MapInfo struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Variant string `json:"variant,omitempty"`
}

// the public representation of a player in the lobby
type PlayerView struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	IsHost  bool   `json:"is_host"`
	Status  string `json:"status"`
	SteamID string `json:"steam_id,omitempty"`
}

// sent to clients whenever the lobby changes. code is empty for the default lobby.
type LobbyView struct {
	Code    string       `json:"code,omitempty"`
	State   string       `json:"state"`
	HostID  string       `json:"host_id"`
	Map     MapInfo      `json:"map"`
	Players []PlayerView `json:"players"`
}

// a single chat line. system messages have System=true and Name="SERVER"
type ChatMessage struct {
	ID       string `json:"id,omitempty"`
	PlayerID string `json:"player_id,omitempty"`
	Name     string `json:"name,omitempty"`
	Text     string `json:"text"`
	Time     int64  `json:"ts"`
	System   bool   `json:"system,omitempty"`
}

// score is a finished run submitted by a client
type Score struct {
	PctGrade  float64 `json:"pct_grade"`
	Grade     string  `json:"grade"`
	PlusMinus string  `json:"plus_minus,omitempty"`
	Perfects  int     `json:"perfects"`
	Misses    int     `json:"misses"`
	Barelies  int     `json:"barelies"`
	MaxCombo  int     `json:"max_combo"`
	MaxHits   int     `json:"max_hits"`
	PB        bool    `json:"pb,omitempty"`
}

// one row of the leaderboard / results table
type Entry struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Status    string  `json:"status"`
	PctGrade  float64 `json:"pct_grade"`
	Grade     string  `json:"grade,omitempty"`
	PlusMinus string  `json:"plus_minus,omitempty"`
	Perfects  int     `json:"perfects,omitempty"`
	Misses    int     `json:"misses,omitempty"`
	Barelies  int     `json:"barelies,omitempty"`
	MaxCombo  int     `json:"max_combo,omitempty"`
	MaxHits   int     `json:"max_hits,omitempty"`
	PB        bool    `json:"pb,omitempty"`
}

// every message a client can send
type ClientMessage struct {
	Type     string   `json:"type"`
	Proto    int      `json:"proto,omitempty"`
	Name     string   `json:"name,omitempty"`
	Lobby    string   `json:"lobby,omitempty"`
	SteamID  string   `json:"steam_id,omitempty"`
	Target   string   `json:"target,omitempty"`
	Text     string   `json:"text,omitempty"`
	Score    *Score   `json:"score,omitempty"`
	PctGrade float64  `json:"pct_grade,omitempty"`
	Map      *MapInfo `json:"map,omitempty"`
	Reason   string   `json:"reason,omitempty"`
	T        int64    `json:"t,omitempty"`
}

// every message the server can send
type ServerMessage struct {
	Type     string        `json:"type"`
	PlayerID string        `json:"player_id,omitempty"`
	Lobby    *LobbyView    `json:"lobby,omitempty"`
	Chat     *ChatMessage  `json:"chat,omitempty"`
	History  []ChatMessage `json:"history,omitempty"`
	RoundID  int           `json:"round_id,omitempty"`
	Map      *MapInfo      `json:"map,omitempty"`
	Players  []PlayerView  `json:"players,omitempty"`
	Entries  []Entry       `json:"entries,omitempty"`
	Text     string        `json:"text,omitempty"`
	InMS     int           `json:"in_ms,omitempty"`
	Reason   string        `json:"reason,omitempty"`
	Code     string        `json:"code,omitempty"`
	Message  string        `json:"message,omitempty"`
	T        int64         `json:"t,omitempty"`
}
