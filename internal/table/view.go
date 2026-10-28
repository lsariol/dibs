package table

import "github.com/lsariol/dibs/internal/cards"

// View is one player's picture of the table. It's built per viewer and holds
// only what that viewer may know: hidden cards have no ID and no face, so
// they can't be tracked or identified, and hidden order is never sent.
type View struct {
	Seats      []string          `json:"seats"`
	Dealer     string            `json:"dealer"`
	Turn       string            `json:"turn"`
	Policy     Policy            `json:"policy"`
	BlindDraw  bool              `json:"blindDraw"`
	Containers []ContainerView   `json:"containers"`
	CutOffer   *CutOffer         `json:"cutOffer"`
	Dealing    *DealView         `json:"dealing"`
	Markers    map[string]string `json:"markers"`
	Counters   map[string]int    `json:"counters"`
	Teams      map[string]string `json:"teams"`
}

// ContainerView is a container as the viewer sees it.
type ContainerView struct {
	ID     string     `json:"id"`
	Kind   Kind       `json:"kind"`
	Label  string     `json:"label"`
	Owner  string     `json:"owner,omitempty"`
	Team   string     `json:"team,omitempty"`
	Cols   int        `json:"cols,omitempty"`
	FaceUp bool       `json:"faceUp"`
	Spread bool       `json:"spread"`
	Count  int        `json:"count"`
	Cards  []CardView `json:"cards"` // every slot for hands, grids and spread piles; only the top for closed piles
	Can    Can        `json:"can"`
}

// Can is what the viewer may do with a container.
type Can struct {
	Take   bool `json:"take"`
	Place  bool `json:"place"`
	Manage bool `json:"manage"`
	Peek   bool `json:"peek"`
}

// CardView is one card or slot. ID and Def are present only when the viewer
// may know the card.
type CardView struct {
	Index int        `json:"i"`
	ID    string     `json:"id,omitempty"`
	Def   *cards.Def `json:"def,omitempty"`
	Up    bool       `json:"up,omitempty"`    // face up for everyone
	Mine  bool       `json:"mine,omitempty"`  // known only to the viewer (own hand, or peeked)
	Empty bool       `json:"empty,omitempty"` // an empty grid slot
}

// DealView is a step-by-step deal in progress.
type DealView struct {
	By     string `json:"by"`
	From   string `json:"from"`
	Dealt  int    `json:"dealt"`
	Total  int    `json:"total"`
	NextTo string `json:"nextTo"`
}

// ViewFor builds viewer's view of the table.
func (t *Table) ViewFor(viewer string) View {
	v := View{
		Seats:     t.Seats,
		Dealer:    t.DealerID(),
		Policy:    t.Policy,
		BlindDraw: t.AllowBlindDraw,
		CutOffer:  t.CutOffer,
		Markers:   t.Markers,
		Counters:  t.Counters,
		Teams:     t.Teams,
	}
	if t.Turn >= 0 && t.Turn < len(t.Seats) {
		v.Turn = t.Seats[t.Turn]
	}
	if d := t.Dealing; d != nil {
		v.Dealing = &DealView{By: d.By, From: d.Plan.From, Dealt: d.Dealt, Total: d.Total}
		if !d.done() {
			v.Dealing.NextTo = d.Plan.To[d.Next]
		}
	}
	for _, c := range t.Containers {
		cv := ContainerView{
			ID: c.ID, Kind: c.Kind, Label: c.Label, Owner: c.Owner, Team: c.Team, Cols: c.Cols,
			FaceUp: c.FaceUp, Spread: c.Spread, Count: c.Count(),
			Can: Can{
				Take:   t.allowed(viewer, c, c.Take),
				Place:  t.canPlace(viewer, c),
				Manage: t.allowed(viewer, c, c.Manage),
				Peek:   t.allowed(viewer, c, c.Peek),
			},
		}
		cv.Cards = []CardView{}
		if c.Kind == Stack && !c.Spread {
			if i := c.top(); i >= 0 {
				cv.Cards = []CardView{t.cardView(c, i, viewer)}
			}
		} else {
			cv.Cards = make([]CardView, len(c.Cards))
			for i := range c.Cards {
				cv.Cards[i] = t.cardView(c, i, viewer)
			}
		}
		v.Containers = append(v.Containers, cv)
	}
	return v
}

func (t *Table) cardView(c *Container, i int, viewer string) CardView {
	id := c.Cards[i]
	if id == "" {
		return CardView{Index: i, Empty: true}
	}
	cs := t.Cards[id]
	cv := CardView{Index: i, Up: cs.FaceUp}
	if t.Visible(cs, viewer) {
		def := cs.Def
		cv.ID = cs.ID
		cv.Def = &def
		cv.Mine = !cs.FaceUp
	}
	return cv
}
