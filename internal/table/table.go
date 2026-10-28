// Package table is the generic card table: containers (piles, hands, grids),
// the cards in them, and every physical action on them, with who may do what
// and who may see what. It knows nothing about any particular game.
package table

import (
	"fmt"
	"maps"
	"slices"

	"github.com/lsariol/dibs/internal/cards"
)

// Kind is how a container holds cards.
type Kind string

// Container kinds.
const (
	Stack Kind = "stack" // an ordered pile; the last card is the top
	Hand  Kind = "hand"  // a player's private hand: the owner sees every card in it
	Grid  Kind = "grid"  // fixed slots, each empty or holding one card
)

// Access says who may do something to a shared container. Containers with an
// Owner ignore it: only the owner may take from, manage or peek into them.
type Access string

// Access levels.
const (
	Anyone Access = "anyone" // any seated player
	Dealer Access = "dealer" // the dealer; anyone when the lobby's policy is shared
	Nobody Access = "nobody" // no one (e.g. peeking at the burn pile)
)

// Policy is the lobby's interaction policy.
type Policy string

// Policies.
const (
	Strict Policy = "strict" // shared containers follow their own Access
	Shared Policy = "shared" // Dealer access opens to anyone; private containers stay private
)

// Container is one place cards can be.
type Container struct {
	ID     string   `json:"id"`
	Kind   Kind     `json:"kind"`
	Label  string   `json:"label"`
	Owner  string   `json:"owner,omitempty"` // player ID for personal containers
	Team   string   `json:"team,omitempty"`  // a team's container (e.g. tricks won)
	Cards  []string `json:"-"`               // card IDs; grids have one entry per slot, "" when empty
	Cols   int      `json:"cols,omitempty"`  // grid columns, for layout
	FaceUp bool     `json:"faceUp"`          // cards placed here land face up unless asked otherwise
	Spread bool     `json:"spread"`          // every card is laid out; otherwise only the top shows
	Take   Access   `json:"take"`            // move cards out, flip them
	Place  Access   `json:"place"`           // put cards in
	Manage Access   `json:"manage"`          // shuffle, cut, deal from, collect
	Peek   Access   `json:"peek"`            // look at a face-down card privately
	ModRev uint64   `json:"-"`               // revision of the last change; older position references are stale
}

// CardState is a card instance and its state on the table.
type CardState struct {
	cards.Card
	FaceUp bool            // visible to everyone
	Known  map[string]bool // players who peeked at it; cleared when it moves away or is shuffled
	Loc    string          // ID of the container it's in
}

// Event is one line for the public history. It never names a hidden card.
type Event struct {
	Actor string
	Text  string
}

// Table is the whole table. Methods that change it are not safe for
// concurrent use: the lobby runs each action on a clone under its lock and
// keeps the clone only if the action succeeded.
type Table struct {
	Cards          map[string]*CardState
	Containers     []*Container        // display order
	Seats          []string            // seated player IDs, clockwise
	Dealer         int                 // index into Seats, -1 for none
	Turn           int                 // index into Seats, -1 for none
	Policy         Policy              // lobby interaction policy
	AllowBlindDraw bool                // players may draw unseen from another player's hand
	CutOffer       *CutOffer           // a pending offer to cut, if any
	Dealing        *PendingDeal        // a step-by-step deal in progress, if any
	Markers        map[string]string   // game markers (trump, maker, …), shown to everyone
	Counters       map[string]int      // game counters (tricks taken, …), shown to everyone
	Rev            uint64              // the revision the current action will produce
	Name           func(string) string `json:"-"` // a player's display name, for history lines
	events         []Event
	byID           map[string]*Container
}

// CutOffer is the dealer's offer to one player to cut a pile.
type CutOffer struct {
	Container string `json:"container"`
	By        string `json:"by"`
	To        string `json:"to"`
}

// New makes an empty table for the seated players, in clockwise order.
func New(seats []string, policy Policy) *Table {
	return &Table{
		Cards:    map[string]*CardState{},
		Seats:    slices.Clone(seats),
		Dealer:   -1,
		Turn:     -1,
		Policy:   policy,
		Markers:  map[string]string{},
		Counters: map[string]int{},
		Name:     func(id string) string { return id },
		byID:     map[string]*Container{},
	}
}

// AddContainer adds a container; grids get slots empty slots.
func (t *Table) AddContainer(c *Container, slots int) error {
	if c.ID == "" || t.byID[c.ID] != nil {
		return fmt.Errorf("table: container ID %q is empty or taken", c.ID)
	}
	if c.Kind == Grid {
		if slots < 1 || slots > 48 {
			return fmt.Errorf("table: a grid needs 1 to 48 slots")
		}
		c.Cards = make([]string, slots)
	}
	for _, a := range []*Access{&c.Take, &c.Place, &c.Manage, &c.Peek} {
		if *a == "" {
			*a = Anyone
		}
	}
	t.Containers = append(t.Containers, c)
	t.byID[c.ID] = c
	return nil
}

// AddCards puts new card instances on top of a container, face down.
func (t *Table) AddCards(container string, cs []cards.Card) error {
	c := t.byID[container]
	if c == nil {
		return fmt.Errorf("table: no container %q", container)
	}
	for _, card := range cs {
		if t.Cards[card.ID] != nil {
			return fmt.Errorf("table: card %s added twice", card.ID)
		}
		t.Cards[card.ID] = &CardState{Card: card, Loc: c.ID}
		if c.Kind == Grid {
			i := slices.Index(c.Cards, "")
			if i < 0 {
				return fmt.Errorf("table: grid %q is full", c.Label)
			}
			c.Cards[i] = card.ID
		} else {
			c.Cards = append(c.Cards, card.ID)
		}
	}
	return nil
}

// Container returns a container by ID, or nil.
func (t *Table) Container(id string) *Container { return t.byID[id] }

// Clone returns a deep copy, so an action can be tried and thrown away.
func (t *Table) Clone() *Table {
	n := *t
	n.Cards = make(map[string]*CardState, len(t.Cards))
	for id, c := range t.Cards {
		cc := *c
		cc.Known = maps.Clone(c.Known)
		n.Cards[id] = &cc
	}
	n.Containers = make([]*Container, len(t.Containers))
	n.byID = make(map[string]*Container, len(t.Containers))
	for i, c := range t.Containers {
		cc := *c
		cc.Cards = slices.Clone(c.Cards)
		n.Containers[i] = &cc
		n.byID[cc.ID] = &cc
	}
	n.Seats = slices.Clone(t.Seats)
	n.Markers = maps.Clone(t.Markers)
	n.Counters = maps.Clone(t.Counters)
	if t.CutOffer != nil {
		o := *t.CutOffer
		n.CutOffer = &o
	}
	if t.Dealing != nil {
		n.Dealing = t.Dealing.clone()
	}
	n.events = nil
	return &n
}

// TakeEvents returns and clears the history lines the last actions produced.
func (t *Table) TakeEvents() []Event {
	e := t.events
	t.events = nil
	return e
}

func (t *Table) logf(actor, format string, args ...any) {
	t.events = append(t.events, Event{Actor: actor, Text: fmt.Sprintf(format, args...)})
}

// Check verifies the table's integrity: every card is in exactly one slot of
// exactly the container it records, and nothing else is in any container.
func (t *Table) Check() error {
	seen := make(map[string]string, len(t.Cards))
	for _, c := range t.Containers {
		if t.byID[c.ID] != c {
			return fmt.Errorf("table: container index out of sync for %q", c.ID)
		}
		for _, id := range c.Cards {
			if id == "" {
				if c.Kind != Grid {
					return fmt.Errorf("table: empty slot in %s %q", c.Kind, c.ID)
				}
				continue
			}
			cs := t.Cards[id]
			if cs == nil {
				return fmt.Errorf("table: unknown card %s in %q", id, c.ID)
			}
			if prev, dup := seen[id]; dup {
				return fmt.Errorf("table: card %s is in both %q and %q", id, prev, c.ID)
			}
			seen[id] = c.ID
			if cs.Loc != c.ID {
				return fmt.Errorf("table: card %s is in %q but records %q", id, c.ID, cs.Loc)
			}
		}
	}
	if len(seen) != len(t.Cards) {
		return fmt.Errorf("table: %d cards exist but %d are in containers", len(t.Cards), len(seen))
	}
	return nil
}

// Count returns how many cards a container holds (filled slots for a grid).
func (c *Container) Count() int {
	n := 0
	for _, id := range c.Cards {
		if id != "" {
			n++
		}
	}
	return n
}

// top returns the index of the top card, or -1 when empty.
func (c *Container) top() int {
	for i := len(c.Cards) - 1; i >= 0; i-- {
		if c.Cards[i] != "" {
			return i
		}
	}
	return -1
}

// IsSeated reports whether a player has a seat at this table.
func (t *Table) IsSeated(id string) bool { return slices.Contains(t.Seats, id) }

// DealerID returns the dealer's player ID, or "".
func (t *Table) DealerID() string {
	if t.Dealer < 0 || t.Dealer >= len(t.Seats) {
		return ""
	}
	return t.Seats[t.Dealer]
}

// SeatOf returns a player's index in clockwise seat order, or -1.
func (t *Table) SeatOf(id string) int { return slices.Index(t.Seats, id) }

// Left returns the player clockwise from p (to their left); Right, counterclockwise.
func (t *Table) Left(p string) string  { return t.neighbor(p, 1) }
func (t *Table) Right(p string) string { return t.neighbor(p, -1) }

// Across returns the player opposite p at a table with an even number of seats.
func (t *Table) Across(p string) string {
	if len(t.Seats)%2 != 0 {
		return ""
	}
	return t.neighbor(p, len(t.Seats)/2)
}

func (t *Table) neighbor(p string, step int) string {
	i := t.SeatOf(p)
	n := len(t.Seats)
	if i < 0 || n == 0 {
		return ""
	}
	return t.Seats[((i+step)%n+n)%n]
}

// HandOf returns the ID of a player's hand container, or "".
func (t *Table) HandOf(player string) string {
	for _, c := range t.Containers {
		if c.Kind == Hand && c.Owner == player {
			return c.ID
		}
	}
	return ""
}
