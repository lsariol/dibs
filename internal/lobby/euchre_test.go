package lobby

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/lsariol/dibs/internal/presets"
	"github.com/lsariol/dibs/internal/scoring"
	"github.com/lsariol/dibs/internal/table"
)

func step(name string, args any) Request {
	p := map[string]any{"step": name}
	if args != nil {
		p["args"] = args
	}
	return req("preset.step", p)
}

// guideHas reports whether a player's guide offers a step with this label prefix.
func guideHas(t *testing.T, l *Lobby, p, label string) bool {
	t.Helper()
	g := l.guideFor(p)
	for _, s := range g.Steps {
		if len(s.Label) >= len(label) && s.Label[:len(label)] == label {
			return true
		}
	}
	return false
}

func TestEuchreSetup(t *testing.T) {
	_, l, ids := newLobby(t, Settings{Preset: "euchre", ScoreMode: ScoreTeam}, "Ann", "Bob", "Cy", "Dee")
	if l.Settings.MaxPlayers != 4 {
		t.Fatal("euchre lobbies hold 4")
	}
	mustOK(t, l.Do(ids[0], req("table.start", nil)))
	tb := l.game.table
	if tb.Container("deck").Count() != 24 {
		t.Fatal("euchre deck is 24 cards")
	}
	for _, cs := range tb.Cards {
		if !slices.Contains([]string{"9", "10", "J", "Q", "K", "A"}, cs.Def.Rank) {
			t.Fatalf("card %s in a euchre deck", cs.Def.Label())
		}
	}
	// Opposite seats are partners.
	if tb.Teams[ids[0]] != tb.Teams[ids[2]] || tb.Teams[ids[1]] != tb.Teams[ids[3]] || tb.Teams[ids[0]] == tb.Teams[ids[1]] {
		t.Fatalf("teams %v", tb.Teams)
	}
	if l.score.Target != 10 || len(l.score.Entries) != 2 {
		t.Fatal("euchre scoreboard: two teams to 10")
	}
	// Asking for more players is capped at 4, and the score mode defaults to teams.
	m := NewManager("x")
	l2, _, _, err := m.Create("A", Settings{Preset: "euchre", MaxPlayers: 5})
	if err != nil || l2.Settings.MaxPlayers != 4 || l2.Settings.ScoreMode != ScoreTeam {
		t.Fatalf("euchre defaults: %+v %v", l2.Settings, err)
	}
}

func TestEuchreDealIsAuthentic(t *testing.T) {
	_, l, ids := newLobby(t, Settings{Preset: "euchre", ScoreMode: ScoreTeam}, "Ann", "Bob", "Cy", "Dee")
	mustOK(t, l.Do(ids[0], req("table.start", nil)))
	tb := l.game.table
	dealer := tb.DealerID()
	right := tb.Right(dealer)
	if right != ids[3] {
		t.Fatal("the cut goes to the dealer's right")
	}
	mustOK(t, l.Do(dealer, req("pile.shuffle", map[string]any{"container": "deck"})))
	mustFail(t, l.Do(ids[1], req("cut.offer", map[string]any{"container": "deck", "to": right})), CodeForbidden)
	mustOK(t, l.Do(dealer, req("cut.offer", map[string]any{"container": "deck", "to": right})))
	mustFail(t, l.Do(dealer, step("deal", map[string]any{"pattern": "3-2"})), CodeInvalid) // waits for the cut
	before := slices.Clone(l.game.table.Container("deck").Cards)
	mustOK(t, l.Do(right, req("cut.answer", map[string]any{"count": 7})))
	after := l.game.table.Container("deck").Cards
	if want := append(slices.Clone(before[17:]), before[:17]...); !slices.Equal(after, want) {
		t.Fatal("the cut didn't rotate the deck")
	}
	deck := slices.Clone(after)
	mustOK(t, l.Do(dealer, step("deal", map[string]any{"pattern": "3-2"})))
	tb = l.game.table
	order := tb.DealOrder(1)
	if order[0] != tb.Left(dealer) {
		t.Fatal("dealing starts at the dealer's left")
	}
	// First pass 3-2-3-2 then 2-3-2-3, clockwise from the left, from the top.
	pos := len(deck)
	got := map[string][]string{}
	for pass, sizes := range [][]int{{3, 2, 3, 2}, {2, 3, 2, 3}} {
		for r, p := range order {
			for range sizes[r] {
				pos--
				got[p] = append(got[p], deck[pos])
			}
		}
		_ = pass
	}
	for _, p := range order {
		h := hand(l, p).Cards
		if len(h) != 5 || !slices.Equal(h, got[p]) {
			t.Fatalf("%s's hand isn't the authentic two-pass deal", l.name(p))
		}
	}
	if tb.Container("deck").Count() != 4 {
		t.Fatal("kitty should have 4 cards")
	}
	noLeaks(t, l, ids)
}

// playHand plays one full hand and returns the scoring team and points given.
func playHand(t *testing.T, l *Lobby, ids []string, n int) {
	t.Helper()
	tb := l.game.table
	dealer := tb.DealerID()
	right := tb.Right(dealer)
	mustOK(t, l.Do(dealer, req("pile.shuffle", map[string]any{"container": "deck"})))
	mustOK(t, l.Do(dealer, req("cut.offer", map[string]any{"container": "deck", "to": right})))
	if n%3 == 0 {
		mustOK(t, l.Do(right, req("cut.answer", map[string]any{"count": 0}))) // declines (knocks)
	} else {
		mustOK(t, l.Do(right, req("cut.answer", map[string]any{"count": 1 + n%22})))
	}
	pattern := []string{"3-2", "2-3", "2s", "3s"}[n%4]
	mustOK(t, l.Do(dealer, step("deal", map[string]any{"pattern": pattern})))
	if !guideHas(t, l, dealer, "Turn up the top card") {
		t.Fatal("dealer isn't offered the upcard")
	}
	mustOK(t, l.Do(dealer, req("card.flip", map[string]any{"ref": table.Ref{Container: "deck", Index: -1}})))
	upcard := l.game.table.Cards[l.game.table.Container("deck").Cards[3]]
	if !upcard.FaceUp {
		t.Fatal("upcard not face up")
	}
	noLeaks(t, l, ids)

	order := l.game.table.DealOrder(1)
	var maker string
	if n%2 == 0 {
		// Round 1: the first player passes, the second orders it up; the dealer picks up and discards.
		mustOK(t, l.Do(order[0], step("pass", nil)))
		maker = order[1]
		mustOK(t, l.Do(maker, step("order", map[string]any{"alone": n%4 == 2})))
		mustFail(t, l.Do(maker, step("pickup", nil)), CodeForbidden)
		mustOK(t, l.Do(dealer, step("pickup", nil)))
		if hand(l, dealer).Count() != 6 {
			t.Fatal("dealer should hold 6 after picking up")
		}
		discard := hand(l, dealer).Cards[0]
		mustOK(t, l.Do(dealer, req("card.move", map[string]any{"ref": table.Ref{Container: presets.HandID(dealer), Card: discard}, "to": "deck", "face": "down"})))
		if hand(l, dealer).Count() != 5 || l.game.table.Container("deck").Count() != 4 {
			t.Fatal("pickup and discard must keep 5 in hand and 4 in the kitty")
		}
	} else {
		// Everyone passes round 1; round 2 the third player calls another suit.
		for _, p := range order {
			mustOK(t, l.Do(p, step("pass", nil)))
		}
		mustOK(t, l.Do(dealer, step("turn_down", nil)))
		maker = order[2]
		turned := l.game.table.Markers["turned"]
		mustFail(t, l.Do(maker, step("call", map[string]any{"suit": turned})), CodeInvalid)
		other := map[string]string{"S": "H", "H": "S", "D": "C", "C": "D"}[turned]
		mustOK(t, l.Do(maker, step("call", map[string]any{"suit": other, "alone": false})))
	}
	makerTeam := l.game.table.Teams[maker]
	alone := l.game.table.Markers["alone"]

	// Five tricks: each active player plays a card face up; winners alternate by hand number.
	for trick := range 5 {
		for _, p := range order {
			if alone != "" && p == l.game.table.Across(alone) {
				continue // the lone maker's partner sits out
			}
			c := hand(l, p).Cards[0]
			mustOK(t, l.Do(p, req("card.move", map[string]any{"ref": table.Ref{Container: presets.HandID(p), Card: c}, "to": "trick", "face": "up"})))
		}
		winner := maker
		if (n+trick)%(2+n%3) == 0 {
			winner = l.game.table.Left(maker) // a defender
		}
		if !guideHas(t, l, order[trick%4], "Trick won by") {
			t.Fatalf("hand %d trick %d: no button to award the trick", n, trick+1)
		}
		mustOK(t, l.Do(order[trick%4], step("take_trick", map[string]any{"player": winner})))
	}
	tb = l.game.table
	if err := tb.Check(); err != nil {
		t.Fatal(err)
	}
	if tb.Counters["tricks:A"]+tb.Counters["tricks:B"] != 5 {
		t.Fatal("five tricks per hand")
	}

	// Score: only the right team's keeper is offered and allowed.
	before := map[string]int{"A": score(l, scoring.TeamEntry("A")), "B": score(l, scoring.TeamEntry("B"))}
	m := tb.Counters["tricks:"+makerTeam]
	wantTeam, wantPts := makerTeam, 1
	switch {
	case m == 5 && alone != "":
		wantPts = 4
	case m == 5:
		wantPts = 2
	case m < 3:
		wantTeam, wantPts = map[string]string{"A": "B", "B": "A"}[makerTeam], 2
	}
	keeper := l.score.Keepers[wantTeam]
	other := l.score.Keepers[map[string]string{"A": "B", "B": "A"}[wantTeam]]
	mustFail(t, l.Do(other, step("score", nil)), CodeForbidden)
	mustOK(t, l.Do(keeper, step("score", nil)))
	mustFail(t, l.Do(keeper, step("score", nil)), CodeInvalid) // only once
	if got := score(l, scoring.TeamEntry(wantTeam)) - before[wantTeam]; got != wantPts {
		t.Fatalf("hand %d: team %s got %d, want %d (makers %s took %d, alone %v)", n, wantTeam, got, wantPts, makerTeam, m, alone != "")
	}
	mustOK(t, l.Do(dealer, step("next", nil)))
	if l.game.table.DealerID() != l.game.table.Left(dealer) || l.game.table.Container("deck").Count() != 24 {
		t.Fatal("next hand: the deal passes left and the deck is whole")
	}
}

func TestEuchreFullGameToTen(t *testing.T) {
	_, l, ids := newLobby(t, Settings{Preset: "euchre", ScoreMode: ScoreTeam}, "Ann", "Bob", "Cy", "Dee")
	mustOK(t, l.Do(ids[0], req("table.start", nil)))
	hands := 0
	for len(l.score.Leaders()) == 0 {
		if hands == 40 {
			t.Fatal("no team reached 10 in 40 hands")
		}
		playHand(t, l, ids, hands)
		hands++
		if hands == 3 {
			// Someone's phone drops and comes back: same seat, same score, fresh view.
			sub := &fakeSub{}
			_ = l.Attach(ids[2], sub)
			l.Detach(ids[2], sub)
			again := &fakeSub{}
			_ = l.Attach(ids[2], again)
			v := again.last(t)
			if v["you"] != ids[2] || v["score"] == nil {
				t.Fatal("reconnect lost the player or the score")
			}
		}
	}
	if len(l.game.table.Cards) != 24 {
		t.Fatal("cards were created or lost")
	}
	b, _ := json.Marshal(l.score.Entries)
	t.Logf("game over after %d hands: %s", hands, b)
	if hands < 3 {
		t.Fatal(fmt.Sprintf("suspiciously short game: %d hands", hands))
	}
}
