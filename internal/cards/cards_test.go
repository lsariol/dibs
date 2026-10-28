package cards

import (
	"fmt"
	"slices"
	"testing"
)

func countBy(cs []Card, key func(Def) string) map[string]int {
	m := map[string]int{}
	for _, c := range cs {
		m[key(c.Def)]++
	}
	return m
}

func TestDeckSizes(t *testing.T) {
	for _, tc := range []struct {
		typ   string
		count int
		want  int
	}{
		{Standard52, 1, 52}, {Standard54, 1, 54}, {Euchre24, 1, 24}, {Color108, 1, 108}, {Standard52, 2, 104}, {Euchre24, 2, 48},
	} {
		cs, err := Build(tc.typ, tc.count, Composition{})
		if err != nil {
			t.Fatal(err)
		}
		if len(cs) != tc.want {
			t.Errorf("%s x%d: %d cards, want %d", tc.typ, tc.count, len(cs), tc.want)
		}
	}
}

func TestStandardDeckIsComplete(t *testing.T) {
	cs, _ := Build(Standard52, 1, Composition{})
	faces := countBy(cs, Def.Label)
	if len(faces) != 52 {
		t.Fatalf("%d distinct faces", len(faces))
	}
	for _, s := range Suits {
		for _, r := range Ranks {
			if faces[Def{Kind: "standard", Rank: r, Suit: s, Color: SuitColor(s)}.Label()] != 1 {
				t.Errorf("missing %s%s", r, s)
			}
		}
	}
	if (Def{Kind: "standard", Rank: "10", Suit: "H"}).Name() != "10 of Hearts" || (Def{Kind: "standard", Rank: "Q", Suit: "S"}).Label() != "Q♠" {
		t.Fatal("names")
	}
}

func TestEuchreDeck(t *testing.T) {
	cs, _ := Build(Euchre24, 1, Composition{})
	ranks := countBy(cs, func(d Def) string { return d.Rank })
	for _, r := range []string{"9", "10", "J", "Q", "K", "A"} {
		if ranks[r] != 4 {
			t.Errorf("rank %s: %d", r, ranks[r])
		}
	}
	if len(ranks) != 6 {
		t.Fatalf("ranks %v", ranks)
	}
}

func TestJokersAndColorDeck(t *testing.T) {
	cs, _ := Build(Standard54, 1, Composition{})
	if countBy(cs, func(d Def) string { return d.Kind })["joker"] != 2 {
		t.Fatal("54-card deck needs 2 jokers")
	}
	cs, _ = Build(Color108, 1, Composition{})
	kinds := countBy(cs, func(d Def) string { return d.Rank })
	if kinds["0"] != 4 || kinds["7"] != 8 || kinds["skip"] != 8 || kinds["wild"] != 4 || kinds["wild4"] != 4 {
		t.Fatalf("color deck ranks %v", kinds)
	}
	if (Def{Kind: "color", Rank: "draw2", Color: "blue"}).Label() != "Blue Draw Two" {
		t.Fatal("color label")
	}
}

func TestMultiDeckInstancesAreDistinct(t *testing.T) {
	cs, _ := Build(Standard52, 2, Composition{})
	ids := map[string]bool{}
	for _, c := range cs {
		if ids[c.ID] {
			t.Fatal("duplicate instance ID")
		}
		ids[c.ID] = true
	}
	var aces []Card
	for _, c := range cs {
		if c.Def.Rank == "A" && c.Def.Suit == "S" {
			aces = append(aces, c)
		}
	}
	if len(aces) != 2 || aces[0].ID == aces[1].ID || aces[0].Deck == aces[1].Deck {
		t.Fatalf("two decks' Aces of Spades aren't distinct: %+v", aces)
	}
}

func TestCustomComposition(t *testing.T) {
	cs, err := Build(Custom, 1, Composition{Ranks: []string{"A", "K", "K"}, Suits: []string{"H", "S"}, Jokers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 5 {
		t.Fatalf("custom deck: %d cards, want 2 ranks x 2 suits + 1 joker", len(cs))
	}
	for _, bad := range []Composition{{Ranks: []string{"1"}, Suits: []string{"H"}}, {Ranks: []string{"A"}, Suits: []string{"X"}}, {}, {Ranks: []string{"A"}, Suits: []string{"H"}, Jokers: 9}} {
		if _, err := Build(Custom, 1, bad); err == nil {
			t.Errorf("composition %+v accepted", bad)
		}
	}
	if _, err := Build("tarot", 1, Composition{}); err == nil {
		t.Fatal("unknown deck type accepted")
	}
	if _, err := Build(Standard52, 9, Composition{}); err == nil {
		t.Fatal("9 decks accepted")
	}
}

func ids(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprint(i)
	}
	return out
}

func TestShufflePreservesCards(t *testing.T) {
	for range 100 {
		a := ids(52)
		Shuffle(a)
		b := slices.Clone(a)
		slices.Sort(b)
		want := ids(52)
		slices.Sort(want)
		if !slices.Equal(b, want) {
			t.Fatal("shuffle lost or duplicated a card")
		}
	}
}

// Every card should land in every position about equally often.
func TestShuffleIsUniformEnough(t *testing.T) {
	const n, rounds = 5, 20000
	counts := [n][n]int{}
	for range rounds {
		a := ids(n)
		Shuffle(a)
		for pos, v := range a {
			counts[v[0]-'0'][pos]++
		}
	}
	for c := range n {
		for p := range n {
			if got := counts[c][p]; got < rounds/n*85/100 || got > rounds/n*115/100 {
				t.Fatalf("card %d at position %d: %d times of %d", c, p, got, rounds)
			}
		}
	}
}

func TestCutRotatesAtThePosition(t *testing.T) {
	a := []string{"b1", "b2", "b3", "t1", "t2"} // the end is the top
	if err := Cut(a, 2); err != nil {
		t.Fatal(err)
	}
	if want := []string{"t1", "t2", "b1", "b2", "b3"}; !slices.Equal(a, want) {
		t.Fatalf("cut 2 from the top: %v, want %v", a, want)
	}
	for _, k := range []int{0, 5, -1} {
		if Cut([]string{"a", "b", "c", "d", "e"}, k) == nil {
			t.Errorf("cut %d accepted", k)
		}
	}
	if Cut([]string{"a"}, 1) == nil {
		t.Fatal("cut of one card accepted")
	}
}
