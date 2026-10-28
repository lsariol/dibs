package lobby

import (
	"testing"

	"github.com/lsariol/dibs/internal/presets"
	"github.com/lsariol/dibs/internal/table"
)

func TestEveryPresetBuildsAValidTable(t *testing.T) {
	for _, p := range presets.All() {
		info := p.Info()
		names := []string{"P1", "P2", "P3", "P4", "P5", "P6", "P7", "P8"}[:max(info.MinPlayers, 2)]
		_, l, ids := newLobby(t, Settings{Preset: info.ID}, names...)
		mustOK(t, l.Do(ids[0], req("table.start", nil)))
		if err := l.game.table.Check(); err != nil {
			t.Fatalf("%s: %v", info.ID, err)
		}
		noLeaks(t, l, ids)
	}
}

func TestHoldemFlow(t *testing.T) {
	_, l, ids := newLobby(t, Settings{Preset: "holdem"}, "Ann", "Bob", "Cy")
	mustOK(t, l.Do(ids[0], req("table.start", nil)))
	if l.Settings.ScoreMode != ScoreSolo {
		t.Fatal("hold'em defaults to solo scores (chip counts)")
	}
	ann := ids[0]
	mustOK(t, l.Do(ann, req("pile.shuffle", map[string]any{"container": "deck"})))
	mustOK(t, l.Do(ann, step("deal", nil)))
	for _, id := range ids {
		if hand(l, id).Count() != 2 {
			t.Fatal("two hole cards each")
		}
	}
	mustFail(t, l.Do(ids[1], step("street", map[string]any{"cards": 3})), CodeForbidden)
	mustFail(t, l.Do(ann, step("street", map[string]any{"cards": 1})), CodeInvalid) // the flop comes first
	mustOK(t, l.Do(ann, step("street", map[string]any{"cards": 3})))
	mustOK(t, l.Do(ann, step("street", map[string]any{"cards": 1})))
	mustOK(t, l.Do(ann, step("street", map[string]any{"cards": 1})))
	tb := l.game.table
	if tb.Container("board").Count() != 5 || tb.Container("burn").Count() != 3 || tb.Container("deck").Count() != 52-6-8 {
		t.Fatalf("board %d burn %d deck %d", tb.Container("board").Count(), tb.Container("burn").Count(), tb.Container("deck").Count())
	}
	for _, id := range tb.Container("burn").Cards {
		if tb.Cards[id].FaceUp {
			t.Fatal("a burned card is face up")
		}
	}
	// Nobody, not even the dealer, can look at or take from the burn pile.
	mustFail(t, l.Do(ann, req("card.peek", map[string]any{"ref": table.Ref{Container: "burn", Index: -1}})), CodeForbidden)
	mustFail(t, l.Do(ann, req("card.move", map[string]any{"ref": table.Ref{Container: "burn", Index: -1}, "to": presets.HandID(ann)})), CodeForbidden)
	noLeaks(t, l, ids)
	mustOK(t, l.Do(ids[1], step("fold", nil)))
	mustOK(t, l.Do(ids[2], req("hand.reveal", map[string]any{"up": true})))
	noLeaks(t, l, ids)
	mustOK(t, l.Do(ann, step("next", nil)))
	if l.game.table.Container("deck").Count() != 52 || l.game.table.DealerID() != ids[1] {
		t.Fatal("next hand")
	}
}

func TestGolfFlow(t *testing.T) {
	_, l, ids := newLobby(t, Settings{Preset: "golf"}, "Ann", "Bob")
	mustOK(t, l.Do(ids[0], req("table.start", nil)))
	mustOK(t, l.Do(ids[0], step("deal", nil)))
	tb := l.game.table
	for _, id := range ids {
		g := tb.Container(presets.GridID(id))
		if g.Count() != 6 {
			t.Fatal("six cards in each grid")
		}
		for _, c := range g.Cards {
			if tb.Visible(tb.Cards[c], id) {
				t.Fatal("golf cards start face down, unknown even to their owner")
			}
		}
	}
	mustOK(t, l.Do(ids[0], step("start", nil)))
	// Bob flips two of his own; he can't flip Ann's.
	mustOK(t, l.Do(ids[1], req("card.flip", map[string]any{"ref": table.Ref{Container: presets.GridID(ids[1]), Index: 0}})))
	mustOK(t, l.Do(ids[1], req("card.flip", map[string]any{"ref": table.Ref{Container: presets.GridID(ids[1]), Index: 4}})))
	mustFail(t, l.Do(ids[1], req("card.flip", map[string]any{"ref": table.Ref{Container: presets.GridID(ids[0]), Index: 0}})), CodeForbidden)
	// Bob takes the discard and swaps it into slot 2; the old card goes to the discard.
	mustOK(t, l.Do(ids[1], req("card.move", map[string]any{"ref": table.Ref{Container: "discard", Index: -1}, "to": presets.HandID(ids[1])})))
	drawn := hand(l, ids[1]).Cards[0]
	mustOK(t, l.Do(ids[1], req("card.move", map[string]any{"ref": table.Ref{Container: presets.HandID(ids[1]), Card: drawn}, "to": presets.GridID(ids[1]), "index": 2, "face": "up", "displaceTo": "discard"})))
	if l.game.table.Container(presets.GridID(ids[1])).Cards[2] != drawn || l.game.table.Container("discard").Count() != 1 {
		t.Fatal("swap")
	}
	mustFail(t, l.Do(ids[1], step("score", nil)), CodeInvalid) // cards still face down
	mustOK(t, l.Do(ids[1], step("reveal", nil)))
	mustOK(t, l.Do(ids[1], step("score", nil)))
	mustFail(t, l.Do(ids[1], step("score", nil)), CodeInvalid)
	noLeaks(t, l, ids)
	mustOK(t, l.Do(ids[0], step("next", nil)))
	if l.game.table.Container("deck").Count() != 52 {
		t.Fatal("next hole")
	}
}

func TestBlackjackFlow(t *testing.T) {
	_, l, ids := newLobby(t, Settings{Preset: "blackjack", DeckCount: 2}, "Deal", "Bob", "Cy")
	mustOK(t, l.Do(ids[0], req("table.start", nil)))
	dealer, bob, cy := ids[0], ids[1], ids[2]
	tb := l.game.table
	if tb.Container("shoe").Count() != 104 {
		t.Fatal("two-deck shoe")
	}
	mustFail(t, l.Do(bob, step("deal", nil)), CodeForbidden)
	mustOK(t, l.Do(dealer, step("deal", nil)))
	tb = l.game.table
	house := tb.Container("house")
	if house.Count() != 2 || !tb.Cards[house.Cards[0]].FaceUp || tb.Cards[house.Cards[1]].FaceUp {
		t.Fatal("dealer gets one up, one down")
	}
	for _, p := range []string{bob, cy} {
		if tb.Container(presets.SpotID(p)).Count() != 2 {
			t.Fatal("two cards per player")
		}
	}
	noLeaks(t, l, ids) // the hole card is hidden from players
	peek := req("card.peek", map[string]any{"ref": table.Ref{Container: "house", Index: 1}})
	peek.Rev = l.Rev() // a position reference is only valid at the revision the client saw
	mustOK(t, l.Do(dealer, peek))
	noLeaks(t, l, ids) // ...and from everyone but the dealer after a peek
	mustFail(t, l.Do(bob, step("hit", map[string]any{"player": cy})), CodeForbidden)
	mustOK(t, l.Do(bob, step("hit", map[string]any{"player": bob})))
	mustOK(t, l.Do(bob, step("stand", nil)))
	mustFail(t, l.Do(bob, step("hit", map[string]any{"player": bob})), CodeInvalid)
	mustFail(t, l.Do(cy, req("card.draw", map[string]any{"from": "shoe"})), CodeForbidden) // the shoe is the dealer's
	mustOK(t, l.Do(dealer, step("hit", map[string]any{"player": cy})))
	mustOK(t, l.Do(dealer, step("hole", nil)))
	mustOK(t, l.Do(dealer, step("hit", map[string]any{"player": ""})))
	mustOK(t, l.Do(dealer, step("collect", nil)))
	tb = l.game.table
	if tb.Container("house").Count() != 0 || tb.Container("discard").Count() != 104-tb.Container("shoe").Count() {
		t.Fatal("collect moves every card to the discard")
	}
	mustOK(t, l.Do(dealer, step("reshoe", nil)))
	if l.game.table.Container("shoe").Count() != 104 {
		t.Fatal("reshoe")
	}
	if err := l.game.table.Check(); err != nil {
		t.Fatal(err)
	}
}
