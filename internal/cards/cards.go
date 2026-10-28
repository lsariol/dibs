// Package cards defines card faces and builds decks of unique card
// instances. It knows nothing about tables, players or games.
package cards

import (
	"fmt"
	"slices"
	"strings"

	"github.com/lsariol/dibs/internal/random"
)

// Def is what a card is: its face. Many instances can share one Def.
type Def struct {
	Kind  string `json:"kind"`            // "standard", "joker" or "color"
	Rank  string `json:"rank,omitempty"`  // standard: A 2-10 J Q K; color: 0-9, skip, reverse, draw2, wild, wild4
	Suit  string `json:"suit,omitempty"`  // standard: S H D C
	Color string `json:"color,omitempty"` // standard and joker: red or black; color deck: red yellow green blue, or wild
}

// Card is one physical card: a unique ID and its face.
type Card struct {
	ID   string `json:"id"`
	Def  Def    `json:"def"`
	Deck int    `json:"deck"` // which of the combined decks it came from, from 0
}

// Ranks and suits of the standard deck, in order.
var (
	Ranks = []string{"A", "2", "3", "4", "5", "6", "7", "8", "9", "10", "J", "Q", "K"}
	Suits = []string{"S", "H", "D", "C"}
)

var (
	suitNames   = map[string]string{"S": "Spades", "H": "Hearts", "D": "Diamonds", "C": "Clubs"}
	suitSymbols = map[string]string{"S": "♠", "H": "♥", "D": "♦", "C": "♣"}
	rankNames   = map[string]string{"A": "Ace", "J": "Jack", "Q": "Queen", "K": "King"}
	colorRanks  = map[string]string{"skip": "Skip", "reverse": "Reverse", "draw2": "Draw Two", "wild": "Wild", "wild4": "Wild Draw Four"}
)

// SuitColor is "red" for hearts and diamonds, else "black".
func SuitColor(suit string) string {
	if suit == "H" || suit == "D" {
		return "red"
	}
	return "black"
}

// Label is the short face, e.g. "10♥", "Joker", "Blue 7".
func (d Def) Label() string {
	switch d.Kind {
	case "standard":
		return d.Rank + suitSymbols[d.Suit]
	case "joker":
		return "Joker"
	case "color":
		if d.Color == "wild" {
			return colorRanks[d.Rank]
		}
		r := d.Rank
		if n, ok := colorRanks[r]; ok {
			r = n
		}
		return title(d.Color) + " " + r
	}
	return "?"
}

// Name is the spoken name, e.g. "Ten of Hearts".
func (d Def) Name() string {
	if d.Kind != "standard" {
		if d.Kind == "joker" {
			return title(d.Color) + " Joker"
		}
		return d.Label()
	}
	r := d.Rank
	if n, ok := rankNames[r]; ok {
		r = n
	}
	return r + " of " + suitNames[d.Suit]
}

func title(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// Deck types.
const (
	Standard52 = "standard52"
	Standard54 = "standard54"
	Euchre24   = "euchre24"
	Color108   = "color108"
	Custom     = "custom"
)

// DeckTypes lists the deck types Build accepts.
var DeckTypes = []string{Standard52, Standard54, Euchre24, Color108, Custom}

// Composition is a custom deck: the chosen standard ranks in the chosen suits, plus jokers.
type Composition struct {
	Ranks  []string
	Suits  []string
	Jokers int
}

// Defs returns the faces of one deck of the given type, in a fixed order.
func Defs(deckType string, comp Composition) ([]Def, error) {
	switch deckType {
	case Standard52:
		return standard(Ranks, Suits, 0), nil
	case Standard54:
		return standard(Ranks, Suits, 2), nil
	case Euchre24:
		return standard([]string{"9", "10", "J", "Q", "K", "A"}, Suits, 0), nil
	case Color108:
		return colorDeck(), nil
	case Custom:
		for _, r := range comp.Ranks {
			if !slices.Contains(Ranks, r) {
				return nil, fmt.Errorf("cards: unknown rank %q", r)
			}
		}
		for _, s := range comp.Suits {
			if !slices.Contains(Suits, s) {
				return nil, fmt.Errorf("cards: unknown suit %q", s)
			}
		}
		if comp.Jokers < 0 || comp.Jokers > 4 {
			return nil, fmt.Errorf("cards: use 0 to 4 jokers")
		}
		defs := standard(dedupe(comp.Ranks), dedupe(comp.Suits), comp.Jokers)
		if len(defs) == 0 {
			return nil, fmt.Errorf("cards: a custom deck needs at least one card")
		}
		return defs, nil
	}
	return nil, fmt.Errorf("cards: unknown deck type %q", deckType)
}

// Build returns count decks of the given type as unique card instances.
func Build(deckType string, count int, comp Composition) ([]Card, error) {
	if count < 1 || count > 8 {
		return nil, fmt.Errorf("cards: use 1 to 8 decks")
	}
	defs, err := Defs(deckType, comp)
	if err != nil {
		return nil, err
	}
	out := make([]Card, 0, len(defs)*count)
	for d := range count {
		for _, def := range defs {
			out = append(out, Card{ID: random.ID("c_"), Def: def, Deck: d})
		}
	}
	return out, nil
}

func standard(ranks, suits []string, jokers int) []Def {
	var out []Def
	for _, s := range suits {
		for _, r := range ranks {
			out = append(out, Def{Kind: "standard", Rank: r, Suit: s, Color: SuitColor(s)})
		}
	}
	for j := range jokers {
		c := "black"
		if j%2 == 1 {
			c = "red"
		}
		out = append(out, Def{Kind: "joker", Color: c})
	}
	return out
}

// colorDeck is a generic 108-card color/number/action deck: per color one 0,
// two of 1-9, skip, reverse and draw two; plus four wild and four wild draw four.
func colorDeck() []Def {
	var out []Def
	for _, c := range []string{"red", "yellow", "green", "blue"} {
		out = append(out, Def{Kind: "color", Rank: "0", Color: c})
		for range 2 {
			for n := 1; n <= 9; n++ {
				out = append(out, Def{Kind: "color", Rank: fmt.Sprint(n), Color: c})
			}
			for _, a := range []string{"skip", "reverse", "draw2"} {
				out = append(out, Def{Kind: "color", Rank: a, Color: c})
			}
		}
	}
	for range 4 {
		out = append(out, Def{Kind: "color", Rank: "wild", Color: "wild"}, Def{Kind: "color", Rank: "wild4", Color: "wild"})
	}
	return out
}

func dedupe(s []string) []string {
	var out []string
	for _, v := range s {
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// Shuffle puts ids in a uniformly random order: Fisher–Yates with crypto/rand.
func Shuffle(ids []string) {
	for i := len(ids) - 1; i > 0; i-- {
		j := random.Intn(i + 1)
		ids[i], ids[j] = ids[j], ids[i]
	}
}

// Cut lifts the top k cards (the end of ids) and puts them underneath (the
// start), as a real cut does. k must leave cards in both packets.
func Cut(ids []string, k int) error {
	n := len(ids)
	if n < 2 || k < 1 || k > n-1 {
		return fmt.Errorf("cards: a cut must leave at least one card in each packet (1 to %d)", max(n-1, 1))
	}
	top := slices.Clone(ids[n-k:])
	copy(ids[k:], ids[:n-k])
	copy(ids, top)
	return nil
}
