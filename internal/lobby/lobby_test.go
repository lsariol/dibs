package lobby

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"
)

// fakeSub records what a connection would have been sent.
type fakeSub struct {
	mu     sync.Mutex
	states [][]byte
	ended  string
}

func (f *fakeSub) SendState(msg []byte) { f.mu.Lock(); f.states = append(f.states, msg); f.mu.Unlock() }
func (f *fakeSub) End(reason string)    { f.mu.Lock(); f.ended = reason; f.mu.Unlock() }

func (f *fakeSub) last(t *testing.T) map[string]any {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.states) == 0 {
		t.Fatal("no state received")
	}
	var v map[string]any
	if err := json.Unmarshal(f.states[len(f.states)-1], &v); err != nil {
		t.Fatal(err)
	}
	return v
}

var reqN int

func req(action string, params any) Request {
	reqN++
	b, _ := json.Marshal(params)
	return Request{ID: fmt.Sprintf("r%d", reqN), Action: action, Params: b}
}

func mustOK(t *testing.T, r Response) {
	t.Helper()
	if !r.OK {
		t.Fatalf("action failed: %+v", r.Error)
	}
}

func mustFail(t *testing.T, r Response, code string) {
	t.Helper()
	if r.OK || r.Error == nil || r.Error.Code != code {
		t.Fatalf("want error %q, got %+v", code, r)
	}
}

// newLobby makes a lobby with an owner and n-1 more players.
func newLobby(t *testing.T, s Settings, names ...string) (*Manager, *Lobby, []string) {
	t.Helper()
	m := NewManager("http://localhost:2600")
	l, owner, _, err := m.Create(names[0], s)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{owner}
	for _, n := range names[1:] {
		id, _, err := l.Join(n)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return m, l, ids
}

func TestCreateAndJoin(t *testing.T) {
	m, l, ids := newLobby(t, Settings{}, "Ann", "Bob")
	if m.Get(l.Code) != l || m.Get(" "+l.Code[:3]+"-"+l.Code[3:]+" ") != l {
		t.Fatal("lookup by code failed")
	}
	if l.OwnerID != ids[0] || len(l.active()) != 2 {
		t.Fatalf("owner %s players %d", l.OwnerID, len(l.active()))
	}
	if l.Players[0].Seat != 0 || l.Players[1].Seat != 1 {
		t.Fatal("players should take the first free seats while waiting")
	}
	if _, _, err := l.Join("ann"); err == nil {
		t.Fatal("duplicate name (any case) accepted")
	}
	if _, _, err := l.Join("   "); err == nil {
		t.Fatal("blank name accepted")
	}
	if _, _, err := l.Join("abcdefghijklmnopqrstu"); err == nil {
		t.Fatal("21-character name accepted")
	}
}

func TestLobbyFull(t *testing.T) {
	_, l, _ := newLobby(t, Settings{MaxPlayers: 2}, "Ann", "Bob")
	if _, _, err := l.Join("Cy"); err == nil || asError(err).Code != CodeFull {
		t.Fatalf("third player in a 2-player lobby: %v", err)
	}
}

func TestPresetLimits(t *testing.T) {
	m := NewManager("x")
	if _, _, _, err := m.Create("Ann", Settings{Preset: "chess"}); err == nil {
		t.Fatal("unknown preset accepted")
	}
	if _, _, _, err := m.Create("Ann", Settings{Preset: "blank", MaxPlayers: 1}); err == nil {
		t.Fatal("1-player lobby accepted")
	}
}

func TestSessions(t *testing.T) {
	m := NewManager("x")
	l, owner, token, err := m.Create("Ann", Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if id, ok := l.Authenticate(token); !ok || id != owner {
		t.Fatal("owner token rejected")
	}
	for _, bad := range []string{"", "x", token + "x"} {
		if _, ok := l.Authenticate(bad); ok {
			t.Fatalf("token %q accepted", bad)
		}
	}
	for h := range l.sessions {
		if string(h[:]) == token {
			t.Fatal("token stored in plain text")
		}
	}
}

func TestSeatRules(t *testing.T) {
	_, l, ids := newLobby(t, Settings{MaxPlayers: 4}, "Ann", "Bob", "Cy")
	owner, bob, cy := ids[0], ids[1], ids[2]
	mustFail(t, l.Do(bob, req("lobby.seat", map[string]any{"seat": 0})), CodeInvalid)                 // taken
	mustFail(t, l.Do(bob, req("lobby.seat", map[string]any{"player": cy, "seat": 3})), CodeForbidden) // not owner
	mustFail(t, l.Do(bob, req("lobby.seat", map[string]any{"seat": 4})), CodeInvalid)                 // no such seat
	mustOK(t, l.Do(bob, req("lobby.seat", map[string]any{"seat": 3})))
	mustOK(t, l.Do(owner, req("lobby.seat", map[string]any{"player": cy, "seat": 3}))) // swap
	if l.player(cy).Seat != 3 || l.player(bob).Seat != 2 {
		t.Fatalf("swap: cy %d bob %d", l.player(cy).Seat, l.player(bob).Seat)
	}
	mustFail(t, l.Do(bob, req("lobby.team", map[string]any{"player": bob, "team": "A"})), CodeForbidden)
	mustOK(t, l.Do(owner, req("lobby.team", map[string]any{"player": bob, "team": "A"})))
	mustFail(t, l.Do(owner, req("lobby.team", map[string]any{"player": bob, "team": "Z"})), CodeInvalid)
	mustFail(t, l.Do(owner, req("lobby.seat", map[string]any{"seat": 1, "bogus": true})), CodeBadRequest)
}

func TestKickRevokesSession(t *testing.T) {
	m := NewManager("x")
	l, owner, _, _ := m.Create("Ann", Settings{})
	bob, token, _ := l.Join("Bob")
	sub := &fakeSub{}
	if err := l.Attach(bob, sub); err != nil {
		t.Fatal(err)
	}
	mustFail(t, l.Do(bob, req("lobby.kick", map[string]any{"player": owner})), CodeForbidden)
	mustOK(t, l.Do(owner, req("lobby.kick", map[string]any{"player": bob})))
	if _, ok := l.Authenticate(token); ok {
		t.Fatal("kicked player's session still works")
	}
	if sub.ended == "" {
		t.Fatal("kicked player's connection wasn't ended")
	}
	mustFail(t, l.Do(bob, req("lobby.ready", map[string]any{"ready": true})), CodeForbidden)
	if err := l.Attach(bob, &fakeSub{}); err == nil {
		t.Fatal("kicked player reattached")
	}
}

func TestOwnerLeavingPassesOwnership(t *testing.T) {
	_, l, ids := newLobby(t, Settings{}, "Ann", "Bob")
	mustOK(t, l.Do(ids[0], req("lobby.leave", nil)))
	if l.OwnerID != ids[1] {
		t.Fatal("ownership not passed on")
	}
	mustOK(t, l.Do(ids[1], req("lobby.leave", nil)))
	if l.State != Ended {
		t.Fatal("empty lobby not ended")
	}
}

func TestDeduplication(t *testing.T) {
	_, l, ids := newLobby(t, Settings{}, "Ann", "Bob")
	r := req("lobby.rename", map[string]any{"name": "Annie"})
	first := l.Do(ids[0], r)
	mustOK(t, first)
	rev := l.Rev()
	again := l.Do(ids[0], r)
	if !again.OK || !again.Duplicate || again.Rev != first.Rev || l.Rev() != rev {
		t.Fatalf("retry wasn't deduplicated: %+v, rev %d→%d", again, rev, l.Rev())
	}
	// A failed request is remembered too, so a retry doesn't suddenly succeed twice.
	bad := req("lobby.rename", map[string]any{"name": ""})
	mustFail(t, l.Do(ids[0], bad), CodeInvalid)
	if r := l.Do(ids[0], bad); !r.Duplicate {
		t.Fatal("failed request not remembered")
	}
}

func TestMalformedRequests(t *testing.T) {
	_, l, ids := newLobby(t, Settings{}, "Ann")
	rev := l.Rev()
	mustFail(t, l.Do(ids[0], Request{Action: "lobby.ready"}), CodeBadRequest)
	mustFail(t, l.Do(ids[0], Request{ID: "x", Action: "nope"}), CodeBadRequest)
	mustFail(t, l.Do(ids[0], Request{ID: "y", Action: "lobby.ready", Params: json.RawMessage(`{"ready":"yes"}`)}), CodeBadRequest)
	mustFail(t, l.Do(ids[0], Request{ID: "z", Action: "lobby.seat", Params: json.RawMessage(`[1,2`)}), CodeBadRequest)
	mustFail(t, l.Do("p_nobody", req("lobby.ready", nil)), CodeForbidden)
	if l.Rev() != rev {
		t.Fatal("failed requests changed the revision")
	}
}

func TestDisconnectIsNotLeave(t *testing.T) {
	_, l, ids := newLobby(t, Settings{}, "Ann", "Bob")
	watcher := &fakeSub{}
	_ = l.Attach(ids[0], watcher)
	sub := &fakeSub{}
	_ = l.Attach(ids[1], sub)
	l.Detach(ids[1], sub)
	if l.player(ids[1]) == nil || l.player(ids[1]).Seat != 1 {
		t.Fatal("a closed connection removed the player or their seat")
	}
	v := watcher.last(t)
	players := v["lobby"].(map[string]any)["players"].([]any)
	if players[1].(map[string]any)["connected"] != false {
		t.Fatal("others don't see the player as disconnected")
	}
	again := &fakeSub{}
	if err := l.Attach(ids[1], again); err != nil {
		t.Fatal(err)
	}
	if again.last(t)["you"] != ids[1] {
		t.Fatal("reconnect didn't restore the same player")
	}
}

func TestSweep(t *testing.T) {
	m := NewManager("x")
	now := time.Now()
	m.now = func() time.Time { return now }
	l, owner, _, _ := m.Create("Ann", Settings{})
	l.now = m.now
	sub := &fakeSub{}
	_ = l.Attach(owner, sub)

	now = now.Add(EmptyTimeout + time.Minute)
	if m.Sweep() != 0 {
		t.Fatal("a lobby with a connected player expired early")
	}
	l.Detach(owner, sub)
	now = now.Add(EmptyTimeout - time.Minute)
	if m.Sweep() != 0 {
		t.Fatal("expired before EmptyTimeout with nobody connected")
	}
	now = now.Add(2 * time.Minute)
	if m.Sweep() != 1 || m.Get(l.Code) != nil {
		t.Fatal("abandoned lobby not removed")
	}
}

func TestSweepIdle(t *testing.T) {
	m := NewManager("x")
	now := time.Now()
	m.now = func() time.Time { return now }
	l, owner, _, _ := m.Create("Ann", Settings{})
	l.now = m.now
	_ = l.Attach(owner, &fakeSub{})
	now = now.Add(IdleTimeout + time.Minute)
	if m.Sweep() != 1 {
		t.Fatal("idle lobby with an open tab not removed")
	}
}

func TestSettingsOwnerOnly(t *testing.T) {
	_, l, ids := newLobby(t, Settings{}, "Ann", "Bob", "Cy")
	mustFail(t, l.Do(ids[1], req("lobby.settings", Settings{Preset: "blank"})), CodeForbidden)
	mustFail(t, l.Do(ids[0], req("lobby.settings", Settings{Preset: "blank", MaxPlayers: 2})), CodeInvalid)
	mustOK(t, l.Do(ids[0], req("lobby.settings", Settings{Preset: "blank", MaxPlayers: 5, ScoreMode: ScoreSolo})))
	if l.Settings.MaxPlayers != 5 || l.Settings.ScoreMode != ScoreSolo {
		t.Fatalf("settings %+v", l.Settings)
	}
}

func TestLogHasNoFailedActions(t *testing.T) {
	_, l, ids := newLobby(t, Settings{}, "Ann", "Bob")
	n := len(l.log)
	mustFail(t, l.Do(ids[1], req("lobby.kick", map[string]any{"player": ids[0]})), CodeForbidden)
	if len(l.log) != n {
		t.Fatal("a failed action left a history line")
	}
}
