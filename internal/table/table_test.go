package table

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/lsariol/dibs/internal/cards"
)

var players = []string{"ann", "bob", "cy", "dee"}

// newTestTable is a 4-seat table: a 52-card deck (dealer-managed), a face-up
// discard, a spread trick, a burn pile, a hand and a 2x3 grid per player.
func newTestTable(t *testing.T) *Table {
	t.Helper()
	tb := New(players, Strict)
	must(t, tb.AddContainer(&Container{ID: "deck", Kind: Stack, Label: "Deck", Manage: Dealer, Take: Anyone, Peek: Nobody}, 0))
	must(t, tb.AddContainer(&Container{ID: "discard", Kind: Stack, Label: "Discard", FaceUp: true, Peek: Nobody}, 0))
	must(t, tb.AddContainer(&Container{ID: "trick", Kind: Stack, Label: "Trick", FaceUp: true, Spread: true, Peek: Nobody}, 0))
	must(t, tb.AddContainer(&Container{ID: "burn", Kind: Stack, Label: "Burn", Take: Dealer, Place: Dealer, Manage: Dealer, Peek: Nobody}, 0))
	for _, p := range players {
		must(t, tb.AddContainer(&Container{ID: "hand-" + p, Kind: Hand, Label: "Hand", Owner: p}, 0))
		must(t, tb.AddContainer(&Container{ID: "grid-" + p, Kind: Grid, Label: "Grid", Owner: p, Cols: 3}, 6))
	}
	cs, err := cards.Build(cards.Standard52, 1, cards.Composition{})
	must(t, err)
	must(t, tb.AddCards("deck", cs))
	tb.Dealer = 0
	must(t, tb.Check())
	return tb
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func wantCode(t *testing.T, err error, c string) {
	t.Helper()
	if code(err) != c {
		t.Fatalf("want %q, got %v", c, err)
	}
}

func count(tb *Table, id string) int { return tb.Container(id).Count() }

func TestDrawAndMovePreserveCards(t *testing.T) {
	tb := newTestTable(t)
	must(t, tb.Draw("bob", "deck", "", 5, ""))
	if count(tb, "hand-bob") != 5 || count(tb, "deck") != 47 {
		t.Fatal("draw counts")
	}
	top := tb.Container("hand-bob").Cards[4]
	must(t, tb.Move("bob", Ref{Container: "hand-bob", Card: top}, "trick", -1, "up", "", 0))
	if !tb.Cards[top].FaceUp || tb.Container("trick").Cards[0] != top {
		t.Fatal("card not played face up to the trick")
	}
	must(t, tb.Check())
}

func TestOrderIsPreserved(t *testing.T) {
	tb := newTestTable(t)
	deck := slices.Clone(tb.Container("deck").Cards)
	must(t, tb.Draw("ann", "deck", "discard", 3, ""))
	got := tb.Container("discard").Cards
	want := []string{deck[51], deck[50], deck[49]} // the top card first
	if !slices.Equal(got, want) {
		t.Fatalf("discard order %v, want %v", got, want)
	}
	must(t, tb.Collect("ann", "discard", "trick", ""))
	if !slices.Equal(tb.Container("trick").Cards, want) {
		t.Fatal("collect changed the order")
	}
	// Rearranging a hand: move the first card to the end.
	must(t, tb.Draw("ann", "deck", "", 3, ""))
	h := slices.Clone(tb.Container("hand-ann").Cards)
	must(t, tb.Move("ann", Ref{Container: "hand-ann", Card: h[0]}, "hand-ann", -1, "", "", 0))
	if want := []string{h[1], h[2], h[0]}; !slices.Equal(tb.Container("hand-ann").Cards, want) {
		t.Fatalf("rearranged hand %v, want %v", tb.Container("hand-ann").Cards, want)
	}
	must(t, tb.Move("ann", Ref{Container: "hand-ann", Card: h[0]}, "hand-ann", 0, "", "", 0))
	if !slices.Equal(tb.Container("hand-ann").Cards, h) {
		t.Fatal("moving back to the front")
	}
}

func TestShuffleAndCut(t *testing.T) {
	tb := newTestTable(t)
	before := slices.Clone(tb.Container("deck").Cards)
	wantCode(t, tb.Shuffle("bob", "deck"), errForbidden) // bob isn't the dealer
	must(t, tb.Shuffle("ann", "deck"))
	after := tb.Container("deck").Cards
	if slices.Equal(before, after) {
		t.Fatal("shuffle left the order unchanged (1 in 52!)")
	}
	a, b := slices.Clone(before), slices.Clone(after)
	slices.Sort(a)
	slices.Sort(b)
	if !slices.Equal(a, b) {
		t.Fatal("shuffle changed the cards")
	}
	pre := slices.Clone(after)
	must(t, tb.Cut("ann", "deck", 10))
	if want := append(slices.Clone(pre[42:]), pre[:42]...); !slices.Equal(tb.Container("deck").Cards, want) {
		t.Fatal("cut didn't rotate at the chosen position")
	}
	wantCode(t, tb.Cut("ann", "deck", 52), errInvalid)
	wantCode(t, tb.Cut("ann", "deck", 0), errInvalid)
	must(t, tb.Check())
}

func TestCutOffer(t *testing.T) {
	tb := newTestTable(t)
	wantCode(t, tb.OfferCut("bob", "deck", "cy"), errForbidden)
	must(t, tb.OfferCut("ann", "deck", "dee"))
	wantCode(t, tb.Deal("ann", DealPlan{From: "deck", To: []string{"hand-bob"}, Passes: [][]int{{1}}}), errInvalid)
	wantCode(t, tb.Shuffle("ann", "deck"), errInvalid)
	wantCode(t, tb.AnswerCut("bob", 5), errForbidden)
	pre := slices.Clone(tb.Container("deck").Cards)
	must(t, tb.AnswerCut("dee", 20))
	if tb.CutOffer != nil {
		t.Fatal("offer not cleared")
	}
	if want := append(slices.Clone(pre[32:]), pre[:32]...); !slices.Equal(tb.Container("deck").Cards, want) {
		t.Fatal("offered cut didn't rotate the deck")
	}
	must(t, tb.OfferCut("ann", "deck", "dee"))
	pre = slices.Clone(tb.Container("deck").Cards)
	must(t, tb.AnswerCut("dee", 0))
	if !slices.Equal(pre, tb.Container("deck").Cards) || tb.CutOffer != nil {
		t.Fatal("declining changed the deck or kept the offer")
	}
	wantCode(t, tb.AnswerCut("dee", 3), errInvalid)
}

func TestInstantDealWithPackets(t *testing.T) {
	tb := newTestTable(t)
	order := tb.DealOrder(1)
	if !slices.Equal(order, []string{"bob", "cy", "dee", "ann"}) {
		t.Fatalf("deal order from dealer's left: %v", order)
	}
	var to []string
	for _, p := range order {
		to = append(to, "hand-"+p)
	}
	deck := slices.Clone(tb.Container("deck").Cards)
	must(t, tb.Deal("ann", DealPlan{From: "deck", To: to, Passes: [][]int{{2, 3, 2, 3}, {3, 2, 3, 2}}}))
	for _, p := range players {
		if count(tb, "hand-"+p) != 5 {
			t.Fatalf("%s has %d cards", p, count(tb, "hand-"+p))
		}
	}
	// Bob is first: the top two cards, then cards 11-13 from the top in the second pass.
	n := len(deck)
	want := []string{deck[n-1], deck[n-2], deck[n-11], deck[n-12], deck[n-13]}
	if !slices.Equal(tb.Container("hand-bob").Cards, want) {
		t.Fatal("packets weren't dealt in sequence")
	}
	if count(tb, "deck") != 32 {
		t.Fatal("deck count")
	}
	must(t, tb.Check())
}

func TestUnequalAndCounterclockwiseDeals(t *testing.T) {
	tb := newTestTable(t)
	order := tb.DealOrder(-1)
	if !slices.Equal(order, []string{"dee", "cy", "bob", "ann"}) {
		t.Fatalf("counterclockwise order: %v", order)
	}
	must(t, tb.Deal("ann", DealPlan{From: "deck", To: []string{"hand-dee", "hand-cy", "trick"}, Passes: [][]int{{1, 4, 0}, {2}}, Face: "up"}))
	if count(tb, "hand-dee") != 3 || count(tb, "hand-cy") != 6 || count(tb, "trick") != 2 {
		t.Fatal("unequal packet counts")
	}
	for _, id := range tb.Container("hand-dee").Cards {
		if !tb.Cards[id].FaceUp {
			t.Fatal("face-up deal left a card face down")
		}
	}
}

func TestDealValidation(t *testing.T) {
	tb := newTestTable(t)
	wantCode(t, tb.Deal("bob", DealPlan{From: "deck", To: []string{"hand-cy"}, Passes: [][]int{{1}}}), errForbidden)
	wantCode(t, tb.Deal("ann", DealPlan{From: "deck", To: []string{"hand-cy"}, Passes: [][]int{{53}}}), errBad)
	wantCode(t, tb.Deal("ann", DealPlan{From: "deck", To: []string{"hand-cy", "hand-bob"}, Passes: [][]int{{30}}}), errInvalid)
	wantCode(t, tb.Deal("ann", DealPlan{From: "deck", To: []string{"hand-cy", "hand-cy"}, Passes: [][]int{{1}}}), errBad)
	wantCode(t, tb.Deal("ann", DealPlan{From: "deck", To: []string{"hand-cy", "hand-bob"}, Passes: [][]int{{1, 2, 3}}}), errBad)
	wantCode(t, tb.Deal("ann", DealPlan{From: "deck", To: []string{"grid-cy"}, Passes: [][]int{{7}}}), errInvalid)
	wantCode(t, tb.Deal("ann", DealPlan{From: "deck", To: []string{"burn"}, Passes: [][]int{{0}}}), errBad)
	if count(tb, "deck") != 52 {
		t.Fatal("a refused deal moved cards")
	}
}

func TestStepDeal(t *testing.T) {
	tb := newTestTable(t)
	must(t, tb.Deal("ann", DealPlan{From: "deck", To: []string{"hand-bob", "hand-cy"}, Passes: [][]int{{2}, {1, 0}}, Step: true}))
	if count(tb, "deck") != 52 || tb.Dealing == nil {
		t.Fatal("a step deal moved cards at once")
	}
	wantCode(t, tb.DealStep("bob", false), errForbidden)
	wantCode(t, tb.Shuffle("ann", "deck"), errInvalid)
	wantCode(t, tb.Deal("ann", DealPlan{From: "deck", To: []string{"hand-dee"}, Passes: [][]int{{1}}}), errInvalid)
	must(t, tb.DealStep("ann", false))
	if count(tb, "hand-bob") != 2 || count(tb, "hand-cy") != 0 {
		t.Fatal("first packet")
	}
	must(t, tb.DealStep("ann", false))
	must(t, tb.DealStep("ann", false)) // bob's 1; cy's 0 is skipped and the deal ends
	if tb.Dealing != nil || count(tb, "hand-bob") != 3 || count(tb, "hand-cy") != 2 {
		t.Fatalf("deal end: pending %v bob %d cy %d", tb.Dealing != nil, count(tb, "hand-bob"), count(tb, "hand-cy"))
	}
	must(t, tb.Deal("ann", DealPlan{From: "deck", To: []string{"hand-dee"}, Passes: [][]int{{2}, {2}}, Step: true}))
	must(t, tb.DealStep("ann", true))
	if count(tb, "hand-dee") != 4 || tb.Dealing != nil {
		t.Fatal("finish dealing")
	}
	must(t, tb.Deal("ann", DealPlan{From: "deck", To: []string{"hand-dee"}, Passes: [][]int{{2}, {2}}, Step: true}))
	must(t, tb.DealStep("ann", false))
	must(t, tb.CancelDeal("ann", false))
	if count(tb, "hand-dee") != 6 || tb.Dealing != nil {
		t.Fatal("cancel keeps what was dealt")
	}
	must(t, tb.Check())
}

func TestGridPlacementAndSwap(t *testing.T) {
	tb := newTestTable(t)
	must(t, tb.Deal("ann", DealPlan{From: "deck", To: []string{"grid-bob"}, Passes: [][]int{{6}}}))
	g := tb.Container("grid-bob")
	if g.Count() != 6 {
		t.Fatal("grid not filled")
	}
	must(t, tb.Draw("bob", "deck", "", 1, ""))
	drawn := tb.Container("hand-bob").Cards[0]
	old := g.Cards[4]
	wantCode(t, tb.Move("bob", Ref{Container: "hand-bob", Card: drawn}, "grid-bob", 4, "", "", 0), errInvalid)
	must(t, tb.Move("bob", Ref{Container: "hand-bob", Card: drawn}, "grid-bob", 4, "up", "discard", 0))
	if g.Cards[4] != drawn || tb.Container("discard").Cards[0] != old || !tb.Cards[old].FaceUp {
		t.Fatal("swap into grid slot")
	}
	// Someone else can't swap into Bob's grid.
	must(t, tb.Draw("cy", "deck", "", 1, ""))
	cyCard := tb.Container("hand-cy").Cards[0]
	wantCode(t, tb.Move("cy", Ref{Container: "hand-cy", Card: cyCard}, "grid-bob", 0, "", "discard", 0), errForbidden)
	must(t, tb.Check())
}

func TestPrivateContainers(t *testing.T) {
	tb := newTestTable(t)
	must(t, tb.Draw("bob", "deck", "", 3, ""))
	bobCard := tb.Container("hand-bob").Cards[0]
	// Another player can't use Bob's card ID (they can't see it) or his positions.
	wantCode(t, tb.Move("cy", Ref{Container: "hand-bob", Card: bobCard}, "trick", -1, "up", "", 99), errStale)
	wantCode(t, tb.Move("cy", Ref{Container: "hand-bob", Index: 0}, "trick", -1, "up", "", 99), errForbidden)
	wantCode(t, tb.Flip("cy", Ref{Container: "hand-bob", Index: 0}, 99), errForbidden)
	wantCode(t, tb.Peek("cy", Ref{Container: "hand-bob", Index: 0}, 99), errForbidden)
	wantCode(t, tb.Collect("cy", "hand-bob", "trick", ""), errForbidden)
	wantCode(t, tb.Shuffle("cy", "hand-bob"), errForbidden)
	// The dealer and the lobby owner can't take from it either.
	wantCode(t, tb.Move("ann", Ref{Container: "hand-bob", Index: 0}, "deck", -1, "", "", 99), errForbidden)
	// Unseated people can do nothing.
	wantCode(t, tb.Draw("zed", "deck", "", 1, ""), errForbidden)
	if count(tb, "hand-bob") != 3 {
		t.Fatal("refused actions changed the hand")
	}
}

func TestPeekAndVisibility(t *testing.T) {
	tb := newTestTable(t)
	must(t, tb.Deal("ann", DealPlan{From: "deck", To: []string{"grid-bob"}, Passes: [][]int{{6}}}))
	id := tb.Container("grid-bob").Cards[2]
	cs := tb.Cards[id]
	if tb.Visible(cs, "bob") {
		t.Fatal("face-down grid card visible to its owner before peeking")
	}
	must(t, tb.Peek("bob", Ref{Container: "grid-bob", Index: 2}, 0))
	if !tb.Visible(cs, "bob") || tb.Visible(cs, "cy") {
		t.Fatal("peek visibility")
	}
	wantCode(t, tb.Peek("bob", Ref{Container: "grid-bob", Index: 2}, 0), errInvalid)
	wantCode(t, tb.Peek("ann", Ref{Container: "deck", Index: -1}, 0), errForbidden)
	// Moving the card away forgets the peek.
	must(t, tb.Move("bob", Ref{Container: "grid-bob", Index: 2}, "deck", -1, "down", "", 0))
	if tb.Visible(cs, "bob") {
		t.Fatal("peek survived a move into the deck")
	}
	must(t, tb.Draw("dee", "deck", "", 1, ""))
	if !tb.Visible(tb.Cards[tb.Container("hand-dee").Cards[0]], "dee") || tb.Visible(tb.Cards[tb.Container("hand-dee").Cards[0]], "ann") {
		t.Fatal("hand visibility")
	}
}

func TestFlipOnlyTopOfClosedPile(t *testing.T) {
	tb := newTestTable(t)
	wantCode(t, tb.Flip("ann", Ref{Container: "deck", Index: 3}, 0), errInvalid)
	must(t, tb.Flip("ann", Ref{Container: "deck", Index: -1}, 0))
	top := tb.Container("deck").Cards[51]
	if !tb.Cards[top].FaceUp {
		t.Fatal("top card not turned up")
	}
}

func TestStalePositions(t *testing.T) {
	tb := newTestTable(t)
	tb.Rev = 5
	must(t, tb.Draw("ann", "deck", "discard", 3, "")) // discard changed at rev 5
	wantCode(t, tb.Move("bob", Ref{Container: "discard", Index: 0}, "hand-bob", -1, "", "", 4), errStale)
	must(t, tb.Move("bob", Ref{Container: "discard", Index: 0}, "hand-bob", -1, "", "", 5))
	// The top card is always the top card, whatever the client saw.
	must(t, tb.Move("cy", Ref{Container: "discard", Index: -1}, "hand-cy", -1, "", "", 0))
}

func TestBlindDraw(t *testing.T) {
	tb := newTestTable(t)
	must(t, tb.Draw("bob", "deck", "", 5, ""))
	wantCode(t, tb.BlindDraw("cy", "hand-bob", -1), errForbidden)
	tb.AllowBlindDraw = true
	bobBefore := slices.Clone(tb.Container("hand-bob").Cards)
	must(t, tb.BlindDraw("cy", "hand-bob", 2))
	got := tb.Container("hand-cy").Cards
	if len(got) != 1 || got[0] != bobBefore[2] || count(tb, "hand-bob") != 4 {
		t.Fatal("blind draw didn't move exactly the chosen card")
	}
	if tb.Cards[got[0]].FaceUp || !tb.Visible(tb.Cards[got[0]], "cy") || tb.Visible(tb.Cards[got[0]], "bob") {
		t.Fatal("blind draw visibility: the card should now be Cy's private card")
	}
	for _, id := range tb.Container("hand-bob").Cards {
		if tb.Visible(tb.Cards[id], "cy") {
			t.Fatal("blind draw revealed Bob's other cards")
		}
	}
	must(t, tb.BlindDraw("cy", "hand-bob", -1))
	wantCode(t, tb.BlindDraw("cy", "hand-cy", 0), errInvalid)
	wantCode(t, tb.BlindDraw("cy", "deck", 0), errNotFound)
	wantCode(t, tb.BlindDraw("cy", "hand-bob", 9), errStale)
	must(t, tb.Check())
}

func TestCollectAllResets(t *testing.T) {
	tb := newTestTable(t)
	must(t, tb.Deal("ann", DealPlan{From: "deck", To: []string{"hand-bob", "grid-cy", "trick"}, Passes: [][]int{{5}}}))
	must(t, tb.OfferCut("ann", "deck", "bob"))
	wantCode(t, tb.CollectAll("bob", "deck", false), errForbidden)
	must(t, tb.CollectAll("bob", "deck", true)) // the lobby owner may
	if count(tb, "deck") != 52 || tb.CutOffer != nil {
		t.Fatal("collect all")
	}
	for _, cs := range tb.Cards {
		if cs.FaceUp || cs.Known != nil {
			t.Fatal("collected card still face up or known")
		}
	}
	must(t, tb.Check())
}

func TestDealerAndTurn(t *testing.T) {
	tb := newTestTable(t)
	if tb.Left("ann") != "bob" || tb.Right("ann") != "dee" || tb.Across("ann") != "cy" || tb.Left("dee") != "ann" {
		t.Fatal("neighbors")
	}
	wantCode(t, tb.PassDeal("bob", false), errForbidden)
	must(t, tb.PassDeal("ann", false))
	if tb.DealerID() != "bob" {
		t.Fatal("deal should pass to the left")
	}
	wantCode(t, tb.SetDealer("cy", "cy", false), errForbidden)
	must(t, tb.SetDealer("cy", "dee", true))
	must(t, tb.SetTurn("dee", "ann", false))
	must(t, tb.AdvanceTurn("ann", 1, false))
	if tb.Seats[tb.Turn] != "bob" {
		t.Fatal("turn")
	}
	wantCode(t, tb.AdvanceTurn("cy", 1, false), errForbidden)
}

func TestSharedPolicyOpensDealerPilesOnly(t *testing.T) {
	tb := newTestTable(t)
	tb.Policy = Shared
	must(t, tb.Shuffle("cy", "deck"))
	must(t, tb.Draw("bob", "deck", "", 2, ""))
	wantCode(t, tb.Move("cy", Ref{Container: "hand-bob", Index: 0}, "trick", -1, "", "", 99), errForbidden)
}

func TestAddAndRemovePile(t *testing.T) {
	tb := newTestTable(t)
	wantCode(t, tb.AddPile("bob", PileSpec{Label: "Kitty", Kind: Stack}, false, "x1"), errForbidden)
	must(t, tb.AddPile("ann", PileSpec{Label: "Kitty", Kind: Stack}, false, "x1"))
	must(t, tb.AddPile("bob", PileSpec{Label: "Board", Kind: Grid, Slots: 5}, true, "x2"))
	must(t, tb.Draw("ann", "deck", "x1", 1, ""))
	wantCode(t, tb.RemovePile("ann", "x1", false), errInvalid)
	must(t, tb.RemovePile("ann", "x2", false))
	wantCode(t, tb.RemovePile("ann", "hand-bob", true), errNotFound)
	must(t, tb.Check())
}

// TestRandomOperationsKeepIntegrity runs thousands of random actions, valid
// and invalid, and checks after each that no card was lost or duplicated.
func TestRandomOperationsKeepIntegrity(t *testing.T) {
	tb := newTestTable(t)
	tb.AllowBlindDraw = true
	r := rand.New(rand.NewPCG(1, 2))
	ids := []string{"deck", "discard", "trick", "burn"}
	for _, p := range players {
		ids = append(ids, "hand-"+p, "grid-"+p)
	}
	pick := func(s []string) string { return s[r.IntN(len(s))] }
	faces := []Face{"", "up", "down"}
	for i := range 5000 {
		tb.Rev = uint64(i)
		next := tb.Clone()
		p := pick(players)
		var err error
		k := r.IntN(12)
		op := fmt.Sprint(k)
		switch k {
		case 0:
			err = next.Draw(p, pick(ids), pick(ids), 1+r.IntN(4), faces[r.IntN(3)])
		case 1:
			err = next.Move(p, Ref{Container: pick(ids), Index: r.IntN(8) - 1}, pick(ids), r.IntN(8)-1, faces[r.IntN(3)], pick(append(ids, "")), uint64(i))
		case 2:
			err = next.Shuffle(p, pick(ids))
		case 3:
			err = next.Cut(p, pick(ids), r.IntN(30))
		case 4:
			err = next.Collect(p, pick(ids), pick(ids), faces[r.IntN(3)])
		case 5:
			err = next.Deal(p, DealPlan{From: pick(ids), To: []string{pick(ids), pick(ids)}, Passes: [][]int{{r.IntN(3)}, {r.IntN(3), r.IntN(3)}}, Step: r.IntN(2) == 0})
		case 6:
			err = next.DealStep(p, r.IntN(2) == 0)
		case 7:
			err = next.BlindDraw(p, pick(ids), r.IntN(6)-1)
		case 8:
			err = next.Flip(p, Ref{Container: pick(ids), Index: -1}, uint64(i))
		case 9:
			err = next.CollectAll(p, pick(ids), r.IntN(4) == 0)
		case 10:
			err = next.PassDeal(p, r.IntN(2) == 0)
		case 11:
			if next.CutOffer == nil {
				err = next.OfferCut(p, pick(ids), pick(players))
			} else {
				err = next.AnswerCut(next.CutOffer.To, r.IntN(10))
			}
		}
		if err == nil {
			if cerr := next.Check(); cerr != nil {
				t.Fatalf("step %d (action %s by %s): %v", i, op, p, cerr)
			}
			tb = next
		} else if code(err) == "" {
			t.Fatalf("step %d: error without a code: %v", i, err)
		}
	}
	if len(tb.Cards) != 52 {
		t.Fatal("card count changed")
	}
	if err := tb.Check(); err != nil {
		t.Fatal(err)
	}
}

func TestCloneIsDeep(t *testing.T) {
	tb := newTestTable(t)
	c := tb.Clone()
	must(t, c.Draw("bob", "deck", "", 5, ""))
	c.Cards[c.Container("hand-bob").Cards[0]].FaceUp = true
	c.Markers["x"] = "y"
	if count(tb, "deck") != 52 || count(tb, "hand-bob") != 0 || len(tb.Markers) != 0 {
		t.Fatal("changing a clone changed the original")
	}
	for _, cs := range tb.Cards {
		if cs.FaceUp {
			t.Fatal("card state shared with the clone")
		}
	}
}
