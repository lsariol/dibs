// Package presets sets up tables for particular games. A preset only arranges
// the generic table (decks, piles, hands, grids, dealer) and offers optional
// guided steps built from the same table actions; it never has its own card
// engine and doesn't enforce a game's rules.
package presets

import (
	"fmt"
	"slices"

	"github.com/lsariol/dibs/internal/cards"
	"github.com/lsariol/dibs/internal/table"
)

// Info describes a preset to the lobby.
type Info struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	MinPlayers  int    `json:"minPlayers"`
	MaxPlayers  int    `json:"maxPlayers"`
	Teams       bool   `json:"teams"`     // partnerships across the table
	ScoreMode   string `json:"scoreMode"` // suggested score mode
	TargetScore int    `json:"targetScore"`
}

// Options are the lobby's choices a preset builds from.
type Options struct {
	Seats     []string          // seated player IDs, clockwise
	Teams     map[string]string // player ID -> team
	DeckType  string
	DeckCount int
	Custom    cards.Composition
	GridRows  int
	GridCols  int
}

// Preset sets up a table for one game.
type Preset interface {
	Info() Info
	Setup(t *table.Table, o Options) error
}

var registry = map[string]Preset{}
var order []string

func register(p Preset) {
	registry[p.Info().ID] = p
	order = append(order, p.Info().ID)
}

// Get returns a preset by ID, or nil.
func Get(id string) Preset { return registry[id] }

// All returns every preset in display order.
func All() []Preset {
	out := make([]Preset, 0, len(order))
	for _, id := range order {
		out = append(out, registry[id])
	}
	return out
}

// HandID is the ID of a player's hand container.
func HandID(player string) string { return "hand:" + player }

// GridID is the ID of a player's personal grid.
func GridID(player string) string { return "grid:" + player }

// SpotID is the ID of a player's face-up spot (Blackjack).
func SpotID(player string) string { return "spot:" + player }

// addDeck adds a dealer-run, face-down deck filled with the given cards.
func addDeck(t *table.Table, id, label string, deck []cards.Card) error {
	if err := t.AddContainer(&table.Container{ID: id, Kind: table.Stack, Label: label, Take: table.Anyone, Place: table.Anyone, Manage: table.Dealer, Peek: table.Nobody}, 0); err != nil {
		return err
	}
	return t.AddCards(id, deck)
}

// addHands gives every seated player a private hand.
func addHands(t *table.Table, o Options) error {
	for _, p := range o.Seats {
		if err := t.AddContainer(&table.Container{ID: HandID(p), Kind: table.Hand, Label: "Hand", Owner: p, Peek: table.Nobody}, 0); err != nil {
			return err
		}
	}
	return nil
}

// addPile adds a shared stack anyone may use.
func addPile(t *table.Table, id, label string, faceUp, spread bool) error {
	return t.AddContainer(&table.Container{ID: id, Kind: table.Stack, Label: label, FaceUp: faceUp, Spread: spread, Peek: table.Nobody}, 0)
}

func checkSeats(p Preset, o Options) error {
	info := p.Info()
	if n := len(o.Seats); n < info.MinPlayers || n > info.MaxPlayers {
		if info.MinPlayers == info.MaxPlayers {
			return fmt.Errorf("%s needs exactly %d seated players; %d are seated", info.Name, info.MinPlayers, n)
		}
		return fmt.Errorf("%s needs %d to %d seated players; %d are seated", info.Name, info.MinPlayers, info.MaxPlayers, n)
	}
	if slices.Contains(o.Seats, "") {
		return fmt.Errorf("presets: empty seat ID")
	}
	return nil
}

func init() {
	register(blank{})
}

// blank is a free table: the lobby's deck, a discard pile, an open play
// area, hands and optional personal grids. Anything can be improvised on it.
type blank struct{}

func (blank) Info() Info {
	return Info{ID: "blank", Name: "Blank table", MinPlayers: 2, MaxPlayers: 8, ScoreMode: "owner"}
}

func (b blank) Setup(t *table.Table, o Options) error {
	if err := checkSeats(b, o); err != nil {
		return err
	}
	deckType := o.DeckType
	if deckType == "" {
		deckType = cards.Standard52
	}
	deck, err := cards.Build(deckType, max(o.DeckCount, 1), o.Custom)
	if err != nil {
		return err
	}
	if err := addDeck(t, "deck", "Deck", deck); err != nil {
		return err
	}
	if err := addPile(t, "discard", "Discard", true, false); err != nil {
		return err
	}
	if err := addPile(t, "play", "Table", true, true); err != nil {
		return err
	}
	if err := addHands(t, o); err != nil {
		return err
	}
	if o.GridRows > 0 && o.GridCols > 0 {
		for _, p := range o.Seats {
			c := &table.Container{ID: GridID(p), Kind: table.Grid, Label: "Grid", Owner: p, Cols: o.GridCols, Peek: table.Nobody}
			if err := t.AddContainer(c, o.GridRows*o.GridCols); err != nil {
				return err
			}
		}
	}
	t.Dealer = 0
	return nil
}
