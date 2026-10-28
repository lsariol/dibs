// Package lobby holds the lobbies: players, sessions, seats, teams and owner
// controls, and the per-lobby lock every action goes through. It keeps
// everything in memory.
package lobby

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/lsariol/dibs/internal/random"
)

// Limits on lobbies and players.
const (
	MinPlayers  = 2
	MaxPlayers  = 8
	MaxNameLen  = 20
	logKeep     = 200
	dedupKeep   = 128
	CodeLength  = 6
	maxRequestB = 16 << 10
)

// Teams are the team IDs players can be put in.
var Teams = []string{"A", "B", "C", "D"}

// State is where a lobby is in its life.
type State string

// Lobby states.
const (
	Waiting State = "waiting" // players gather, take seats, pick teams
	Playing State = "playing" // the table is running
	Ended   State = "ended"   // the owner ended it, or it expired
)

// Subscriber is one open connection of a player; it never blocks the lobby.
type Subscriber interface {
	SendState(msg []byte) // the newest snapshot; older unsent ones may be dropped
	End(reason string)    // the session is over for this connection
}

// Player is one person at the table.
type Player struct {
	ID       string
	Name     string
	Seat     int    // 0..MaxPlayers-1, or -1 when not seated
	Team     string // "" or one of Teams
	Ready    bool
	Gone     bool // left or removed; kept so history and cards can still name them
	JoinedAt time.Time
	subs     map[Subscriber]struct{}
	recent   []cachedResponse // newest last, for deduplicating retried requests
}

type cachedResponse struct {
	id   string
	resp Response
}

// Connected reports whether the player has at least one open connection.
func (p *Player) Connected() bool { return len(p.subs) > 0 }

// LogEntry is one line of the public action history. It never holds hidden card identities.
type LogEntry struct {
	Rev   uint64    `json:"rev"`
	Time  time.Time `json:"time"`
	Actor string    `json:"actor,omitempty"`
	Text  string    `json:"text"`
}

// Lobby is one game room. Every field is guarded by mu.
type Lobby struct {
	mu        sync.Mutex
	Code      string
	OwnerID   string
	Settings  Settings
	State     State
	Players   []*Player // join order, including gone players
	sessions  map[[32]byte]string
	rev       uint64
	log       []LogEntry
	created   time.Time
	activity  time.Time // last action, join or connection change
	noConnsAt time.Time // when the last connection closed; zero while anyone is connected
	shareURL  string
	now       func() time.Time
	game      *game // the running table, while Playing
}

func hashToken(token string) [32]byte { return sha256.Sum256([]byte(token)) }

// player returns an active player by ID, or nil.
func (l *Lobby) player(id string) *Player {
	for _, p := range l.Players {
		if p.ID == id && !p.Gone {
			return p
		}
	}
	return nil
}

// anyPlayer returns a player by ID, gone or not, or nil.
func (l *Lobby) anyPlayer(id string) *Player {
	for _, p := range l.Players {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// active returns the players still in the lobby, in join order.
func (l *Lobby) active() []*Player {
	var out []*Player
	for _, p := range l.Players {
		if !p.Gone {
			out = append(out, p)
		}
	}
	return out
}

// seated returns the seated players in clockwise seat order.
func (l *Lobby) seated() []*Player {
	var out []*Player
	for _, p := range l.Players {
		if !p.Gone && p.Seat >= 0 {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b *Player) int { return a.Seat - b.Seat })
	return out
}

// name returns a player's display name, or "someone" for an unknown ID.
func (l *Lobby) name(id string) string {
	if p := l.anyPlayer(id); p != nil {
		return p.Name
	}
	return "someone"
}

// addLog appends a public history line for the change being made, which
// gets revision rev+1.
func (l *Lobby) addLog(actor, format string, args ...any) {
	l.log = append(l.log, LogEntry{Rev: l.rev + 1, Time: l.now().UTC(), Actor: actor, Text: fmt.Sprintf(format, args...)})
	if len(l.log) > logKeep {
		l.log = slices.Clone(l.log[len(l.log)-logKeep:])
	}
}

// addPlayer creates a player and their session; the caller holds mu.
func (l *Lobby) addPlayer(name string) (*Player, string, error) {
	name, err := CleanName(name)
	if err != nil {
		return nil, "", err
	}
	if len(l.active()) >= l.Settings.MaxPlayers {
		return nil, "", errorf(CodeFull, "This lobby is full (%d players).", l.Settings.MaxPlayers)
	}
	for _, p := range l.active() {
		if strings.EqualFold(p.Name, name) {
			return nil, "", errorf(CodeInvalid, "Someone here is already called %q. Pick another name.", name)
		}
	}
	p := &Player{ID: random.ID("p_"), Name: name, Seat: -1, JoinedAt: l.now(), subs: map[Subscriber]struct{}{}}
	if l.State == Waiting {
		p.Seat = l.freeSeat()
	}
	l.Players = append(l.Players, p)
	token := random.Token()
	l.sessions[hashToken(token)] = p.ID
	l.touch()
	return p, token, nil
}

// freeSeat returns the lowest empty seat, or -1.
func (l *Lobby) freeSeat() int {
	for s := 0; s < l.Settings.MaxPlayers; s++ {
		if l.seatHolder(s) == nil {
			return s
		}
	}
	return -1
}

func (l *Lobby) seatHolder(seat int) *Player {
	for _, p := range l.active() {
		if p.Seat == seat {
			return p
		}
	}
	return nil
}

// removePlayer marks a player gone, revokes their sessions and closes their connections.
func (l *Lobby) removePlayer(p *Player, reason string) {
	p.Gone = true
	p.Seat = -1
	p.Ready = false
	for h, id := range l.sessions {
		if id == p.ID {
			delete(l.sessions, h)
		}
	}
	for s := range p.subs {
		s.End(reason)
	}
	p.subs = map[Subscriber]struct{}{}
	if l.OwnerID == p.ID {
		if next := l.active(); len(next) > 0 {
			l.OwnerID = next[0].ID
			l.addLog("", "%s is now the lobby owner.", next[0].Name)
		}
	}
	l.connectionsChanged()
}

func (l *Lobby) touch() { l.activity = l.now() }

// connectionsChanged records when the lobby last had nobody connected.
func (l *Lobby) connectionsChanged() {
	for _, p := range l.active() {
		if p.Connected() {
			l.noConnsAt = time.Time{}
			return
		}
	}
	if l.noConnsAt.IsZero() {
		l.noConnsAt = l.now()
	}
}

// Attach registers a connection for a player and sends it a fresh snapshot.
func (l *Lobby) Attach(playerID string, s Subscriber) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	p := l.player(playerID)
	if p == nil || l.State == Ended {
		return errorf(CodeForbidden, "You're no longer in this lobby.")
	}
	wasConnected := p.Connected()
	p.subs[s] = struct{}{}
	l.touch()
	l.connectionsChanged()
	if !wasConnected {
		l.broadcast()
	} else {
		s.SendState(l.snapshot(p))
	}
	return nil
}

// Detach removes a connection. A closed connection is never a leave: the
// player keeps their seat and cards and can reconnect. Connection changes are
// broadcast without a new revision: they aren't table state, and a new
// revision would cancel someone's chance to undo.
func (l *Lobby) Detach(playerID string, s Subscriber) {
	l.mu.Lock()
	defer l.mu.Unlock()
	p := l.anyPlayer(playerID)
	if p == nil {
		return
	}
	if _, ok := p.subs[s]; !ok {
		return
	}
	delete(p.subs, s)
	l.touch()
	l.connectionsChanged()
	if !p.Connected() && !p.Gone && l.State != Ended {
		l.broadcast()
	}
}

// broadcast sends every connection its own filtered snapshot.
func (l *Lobby) broadcast() {
	for _, p := range l.active() {
		if !p.Connected() {
			continue
		}
		msg := l.snapshot(p)
		for s := range p.subs {
			s.SendState(msg)
		}
	}
}

// end closes the lobby for everyone.
func (l *Lobby) end(reason string) {
	l.State = Ended
	for _, p := range l.Players {
		for s := range p.subs {
			s.End(reason)
		}
		p.subs = map[Subscriber]struct{}{}
	}
	l.sessions = map[[32]byte]string{}
}

// Rev returns the current revision.
func (l *Lobby) Rev() uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rev
}

// Snapshot returns the player's current filtered view as JSON.
func (l *Lobby) Snapshot(playerID string) ([]byte, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	p := l.player(playerID)
	if p == nil {
		return nil, errorf(CodeForbidden, "You're no longer in this lobby.")
	}
	return l.snapshot(p), nil
}

// CleanName trims a display name and checks it's 1 to MaxNameLen printable characters.
func CleanName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return "", errorf(CodeInvalid, "Enter a name.")
	}
	if utf8.RuneCountInString(name) > MaxNameLen {
		return "", errorf(CodeInvalid, "Names can be at most %d characters.", MaxNameLen)
	}
	for _, r := range name {
		if !unicode.IsPrint(r) {
			return "", errorf(CodeInvalid, "Names can only have printable characters.")
		}
	}
	return name, nil
}

// mustJSON marshals values that can't fail (plain structs and maps).
func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic("lobby: marshal: " + err.Error())
	}
	return b
}
