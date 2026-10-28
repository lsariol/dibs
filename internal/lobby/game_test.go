package lobby

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/lsariol/dibs/internal/presets"
	"github.com/lsariol/dibs/internal/table"
)

// startedLobby is a blank-table lobby with the given players seated and started.
func startedLobby(t *testing.T, s Settings, names ...string) (*Lobby, []string) {
	t.Helper()
	if s.Preset == "" {
		s.Preset = "blank"
	}
	_, l, ids := newLobby(t, s, names...)
	mustOK(t, l.Do(ids[0], req("table.start", nil)))
	return l, ids
}

// leaks returns the card IDs and faces in viewer's snapshot that viewer must not know.
func leaks(t *testing.T, l *Lobby, viewer string) []string {
	t.Helper()
	snap, err := l.Snapshot(viewer)
	if err != nil {
		t.Fatal(err)
	}
	s := string(snap)
	tb := l.game.table
	var out []string
	for id, cs := range tb.Cards {
		if tb.Visible(cs, viewer) {
			continue
		}
		if strings.Contains(s, id) {
			out = append(out, "id "+id)
		}
	}
	// Faces: a hidden card's face may appear only if another card with the
	// same face is visible to the viewer (two decks), so check unique faces.
	faces := map[string]int{}
	visibleFaces := map[string]bool{}
	for _, cs := range tb.Cards {
		b, _ := json.Marshal(cs.Def)
		faces[string(b)]++
		if tb.Visible(cs, viewer) {
			visibleFaces[string(b)] = true
		}
	}
	for f := range faces {
		if !visibleFaces[f] && strings.Contains(s, f[1:len(f)-1]) {
			out = append(out, "face "+f)
		}
	}
	return out
}

func noLeaks(t *testing.T, l *Lobby, ids []string) {
	t.Helper()
	for _, id := range ids {
		if lk := leaks(t, l, id); len(lk) > 0 {
			t.Fatalf("%s's snapshot leaks %v", l.name(id), lk)
		}
	}
}

func hand(l *Lobby, p string) *table.Container { return l.game.table.Container(presets.HandID(p)) }

func TestStartNeedsOwnerAndSeats(t *testing.T) {
	_, l, ids := newLobby(t, Settings{}, "Ann", "Bob")
	mustFail(t, l.Do(ids[1], req("table.start", nil)), CodeForbidden)
	mustOK(t, l.Do(ids[1], req("lobby.seat", map[string]any{"seat": -1})))
	mustFail(t, l.Do(ids[0], req("table.start", nil)), CodeInvalid)
	mustOK(t, l.Do(ids[1], req("lobby.seat", map[string]any{"seat": 5})))
	mustOK(t, l.Do(ids[0], req("table.start", nil)))
	if l.State != Playing || len(l.game.table.Seats) != 2 || l.game.table.Seats[1] != ids[1] {
		t.Fatal("table not started with the seated players in seat order")
	}
	mustFail(t, l.Do(ids[0], req("lobby.seat", map[string]any{"seat": 2})), CodeInvalid)
}

func TestPrivacyOfHandsDeckAndLog(t *testing.T) {
	l, ids := startedLobby(t, Settings{}, "Ann", "Bob", "Cy")
	ann, bob, cy := ids[0], ids[1], ids[2]
	noLeaks(t, l, ids) // a fresh deck: nobody knows any card or the order

	mustOK(t, l.Do(ann, req("pile.shuffle", map[string]any{"container": "deck"})))
	mustOK(t, l.Do(ann, req("deal.start", table.DealPlan{From: "deck", To: []string{presets.HandID(bob), presets.HandID(cy), presets.HandID(ann)}, Passes: [][]int{{5}}})))
	noLeaks(t, l, ids)

	// Bob sees his own cards; Ann, the lobby owner and dealer, doesn't.
	snap, _ := l.Snapshot(bob)
	for _, id := range hand(l, bob).Cards {
		if !strings.Contains(string(snap), id) {
			t.Fatal("Bob can't see his own card")
		}
	}
	// Bob plays one face up: everyone may now know it, and only it.
	played := hand(l, bob).Cards[0]
	mustOK(t, l.Do(bob, req("card.move", map[string]any{"ref": table.Ref{Container: presets.HandID(bob), Card: played}, "to": "play", "face": "up"})))
	annSnap, _ := l.Snapshot(ann)
	if !strings.Contains(string(annSnap), played) {
		t.Fatal("a face-up card isn't shown to others")
	}
	// Bob plays one face down: still hidden from others.
	mustOK(t, l.Do(bob, req("card.move", map[string]any{"ref": table.Ref{Container: presets.HandID(bob), Card: hand(l, bob).Cards[0]}, "to": "discard", "face": "down"})))
	noLeaks(t, l, ids)

	// The history never names hidden cards.
	for _, e := range l.log {
		for id, cs := range l.game.table.Cards {
			if strings.Contains(e.Text, id) || (!cs.FaceUp && strings.Contains(e.Text, cs.Def.Label()) && !anyFaceUp(l, cs.Def.Label())) {
				t.Fatalf("history line %q names a hidden card", e.Text)
			}
		}
	}
}

func anyFaceUp(l *Lobby, label string) bool {
	for _, cs := range l.game.table.Cards {
		if cs.FaceUp && cs.Def.Label() == label {
			return true
		}
	}
	return false
}

func TestOwnerCantTouchOthersHands(t *testing.T) {
	l, ids := startedLobby(t, Settings{}, "Ann", "Bob")
	ann, bob := ids[0], ids[1]
	mustOK(t, l.Do(bob, req("card.draw", map[string]any{"from": "deck", "count": 3})))
	h := presets.HandID(bob)
	for _, a := range []struct {
		action string
		params any
	}{
		{"card.move", map[string]any{"ref": table.Ref{Container: h, Index: 0}, "to": "play", "face": "up"}},
		{"card.flip", map[string]any{"ref": table.Ref{Container: h, Index: 0}}},
		{"card.peek", map[string]any{"ref": table.Ref{Container: h, Index: 0}}},
		{"pile.collect", map[string]any{"from": h, "to": "play"}},
		{"pile.shuffle", map[string]any{"container": h}},
		{"card.blind_draw", map[string]any{"from": h, "index": 0}},
	} {
		r := l.Do(ann, req(a.action, a.params))
		if r.OK {
			t.Fatalf("lobby owner could %s on another player's hand", a.action)
		}
	}
	if hand(l, bob).Count() != 3 {
		t.Fatal("Bob's hand changed")
	}
	noLeaks(t, l, ids)
}

func TestBlindDrawSetting(t *testing.T) {
	l, ids := startedLobby(t, Settings{BlindDraw: true}, "Ann", "Bob")
	mustOK(t, l.Do(ids[1], req("card.draw", map[string]any{"from": "deck", "count": 4})))
	mustOK(t, l.Do(ids[0], req("card.blind_draw", map[string]any{"from": presets.HandID(ids[1]), "index": -1})))
	if hand(l, ids[0]).Count() != 1 || hand(l, ids[1]).Count() != 3 {
		t.Fatal("blind draw counts")
	}
	noLeaks(t, l, ids)
}

func TestUndoIsSafe(t *testing.T) {
	l, ids := startedLobby(t, Settings{}, "Ann", "Bob")
	ann, bob := ids[0], ids[1]

	// A draw shows Bob the card: it can't be undone.
	mustOK(t, l.Do(bob, req("card.draw", map[string]any{"from": "deck", "count": 2})))
	mustFail(t, l.Do(bob, req("action.undo", nil)), CodeInvalid)

	// Rearranging his own hand can be undone, by him only.
	h := hand(l, bob)
	before := append([]string(nil), h.Cards...)
	mustOK(t, l.Do(bob, req("card.move", map[string]any{"ref": table.Ref{Container: h.ID, Card: before[0]}, "to": h.ID})))
	mustFail(t, l.Do(ann, req("action.undo", nil)), CodeForbidden)
	mustOK(t, l.Do(bob, req("action.undo", nil)))
	if got := hand(l, bob).Cards; got[0] != before[0] || got[1] != before[1] {
		t.Fatal("undo didn't restore the hand order")
	}
	mustFail(t, l.Do(bob, req("action.undo", nil)), CodeInvalid) // only once

	// Playing a card face up revealed it: refused, a corrective move is needed instead.
	mustOK(t, l.Do(bob, req("card.move", map[string]any{"ref": table.Ref{Container: h.ID, Card: before[0]}, "to": "play", "face": "up"})))
	mustFail(t, l.Do(bob, req("action.undo", nil)), CodeInvalid)

	// A face-down move to a public pile is undoable, until someone else acts.
	mustOK(t, l.Do(bob, req("card.move", map[string]any{"ref": table.Ref{Container: h.ID, Card: before[1]}, "to": "discard", "face": "down"})))
	snap, _ := l.Snapshot(bob)
	if !strings.Contains(string(snap), `"undo":`) {
		t.Fatal("undo not offered to Bob")
	}
	mustOK(t, l.Do(ann, req("lobby.ready", map[string]any{"ready": true})))
	mustFail(t, l.Do(bob, req("action.undo", nil)), CodeInvalid)
	if err := l.game.table.Check(); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentDrawsNeverShareACard(t *testing.T) {
	names := []string{"P1", "P2", "P3", "P4", "P5", "P6", "P7", "P8"}
	l, ids := startedLobby(t, Settings{Policy: PolicyShared}, names...)
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Go(func() {
			for i := range 10 {
				l.Do(id, Request{ID: fmt.Sprintf("d%d", i), Action: "card.draw", Params: json.RawMessage(`{"from":"deck","count":1}`)})
			}
		})
	}
	wg.Wait()
	l.mu.Lock()
	defer l.mu.Unlock()
	seen := map[string]bool{}
	total := 0
	for _, id := range ids {
		for _, c := range hand(l, id).Cards {
			if seen[c] {
				t.Fatal("two players got the same card")
			}
			seen[c] = true
			total++
		}
	}
	if total != 52 || l.game.table.Container("deck").Count() != 0 {
		t.Fatalf("drew %d of 52 (80 attempts on a 52-card deck)", total)
	}
	if err := l.game.table.Check(); err != nil {
		t.Fatal(err)
	}
}

func TestRetriedDealIsNotDealtTwice(t *testing.T) {
	l, ids := startedLobby(t, Settings{}, "Ann", "Bob")
	r := req("deal.start", table.DealPlan{From: "deck", To: []string{presets.HandID(ids[1])}, Passes: [][]int{{5}}})
	mustOK(t, l.Do(ids[0], r))
	if again := l.Do(ids[0], r); !again.Duplicate {
		t.Fatal("retry not recognised")
	}
	if hand(l, ids[1]).Count() != 5 {
		t.Fatal("retried deal dealt again")
	}
}

func TestReconnectRestoresHand(t *testing.T) {
	l, ids := startedLobby(t, Settings{}, "Ann", "Bob")
	mustOK(t, l.Do(ids[1], req("card.draw", map[string]any{"from": "deck", "count": 3})))
	sub := &fakeSub{}
	_ = l.Attach(ids[1], sub)
	l.Detach(ids[1], sub)
	again := &fakeSub{}
	_ = l.Attach(ids[1], again)
	v := again.last(t)
	tbl := v["table"].(map[string]any)
	for _, c := range tbl["containers"].([]any) {
		cm := c.(map[string]any)
		if cm["id"] == presets.HandID(ids[1]) {
			cs := cm["cards"].([]any)
			if len(cs) != 3 || cs[0].(map[string]any)["def"] == nil {
				t.Fatal("reconnected player didn't get their hand back")
			}
			return
		}
	}
	t.Fatal("hand missing after reconnect")
}

func TestStaleAndUnauthorizedActionsChangeNothing(t *testing.T) {
	l, ids := startedLobby(t, Settings{}, "Ann", "Bob")
	mustOK(t, l.Do(ids[0], req("card.draw", map[string]any{"from": "deck", "to": "play", "count": 3})))
	rev := l.Rev()
	r := req("card.move", map[string]any{"ref": table.Ref{Container: "play", Index: 0}, "to": "discard"})
	r.Rev = rev - 1
	mustFail(t, l.Do(ids[1], r), CodeStale)
	mustFail(t, l.Do(ids[1], req("card.move", map[string]any{"ref": table.Ref{Container: "nope", Index: 0}, "to": "discard"})), CodeNotFound)
	mustFail(t, l.Do(ids[1], req("card.move", map[string]any{"ref": "garbage"})), CodeBadRequest)
	if l.Rev() != rev {
		t.Fatal("refused actions changed the revision")
	}
}

func TestStepDealSurvivesDisconnect(t *testing.T) {
	l, ids := startedLobby(t, Settings{}, "Ann", "Bob")
	ann := ids[0]
	sub := &fakeSub{}
	_ = l.Attach(ann, sub)
	mustOK(t, l.Do(ann, req("deal.start", table.DealPlan{From: "deck", To: []string{presets.HandID(ids[1]), presets.HandID(ann)}, Passes: [][]int{{2}, {3}}, Step: true})))
	mustOK(t, l.Do(ann, req("deal.step", map[string]any{"all": false})))
	l.Detach(ann, sub) // the dealer's phone locks mid-deal
	if l.game.table.Dealing == nil || hand(l, ids[1]).Count() != 2 {
		t.Fatal("a disconnect changed the deal")
	}
	again := &fakeSub{}
	_ = l.Attach(ann, again)
	if again.last(t)["table"].(map[string]any)["dealing"] == nil {
		t.Fatal("the reconnected dealer doesn't see the deal in progress")
	}
	mustOK(t, l.Do(ann, req("deal.step", map[string]any{"all": true})))
	if hand(l, ids[1]).Count() != 5 || hand(l, ann).Count() != 5 || l.game.table.Dealing != nil {
		t.Fatal("finishing the deal after reconnecting")
	}
}

func TestNextHandGathersAndPassesTheDeal(t *testing.T) {
	l, ids := startedLobby(t, Settings{}, "Ann", "Bob", "Cy")
	mustOK(t, l.Do(ids[1], req("card.draw", map[string]any{"from": "deck", "count": 4})))
	mustOK(t, l.Do(ids[2], req("card.draw", map[string]any{"from": "deck", "to": "play", "count": 2, "face": "up"})))
	mustFail(t, l.Do(ids[1], req("round.next", map[string]any{"to": "deck"})), CodeForbidden)
	mustOK(t, l.Do(ids[0], req("round.next", map[string]any{"to": "deck"})))
	tb := l.game.table
	if tb.Container("deck").Count() != 52 || tb.DealerID() != ids[1] {
		t.Fatalf("deck %d, dealer %s", tb.Container("deck").Count(), tb.DealerID())
	}
	// Bob is the dealer now and runs the next one.
	mustOK(t, l.Do(ids[1], req("round.next", map[string]any{"to": "deck"})))
	if tb := l.game.table; tb.DealerID() != ids[2] {
		t.Fatal("deal didn't pass on again")
	}
	noLeaks(t, l, ids)
}
