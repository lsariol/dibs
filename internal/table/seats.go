package table

import "slices"

// SetDealer names the dealer ("" for none). The lobby owner, the current
// dealer, or anyone while there's no dealer may.
func (t *Table) SetDealer(actor, player string, isOwner bool) error {
	if !isOwner && t.DealerID() != "" && actor != t.DealerID() {
		return fail(errForbidden, "Only the dealer or the lobby owner can choose the dealer.")
	}
	if !t.IsSeated(actor) && !isOwner {
		return fail(errForbidden, "Only seated players can choose the dealer.")
	}
	if player == "" {
		t.Dealer = -1
		t.logf(actor, "%s cleared the dealer.", t.Name(actor))
		return nil
	}
	i := t.SeatOf(player)
	if i < 0 {
		return fail(errInvalid, "The dealer must be a seated player.")
	}
	t.Dealer = i
	t.logf(actor, "%s is the dealer.", t.Name(player))
	return nil
}

// PassDeal moves the deal one seat clockwise (to the dealer's left).
func (t *Table) PassDeal(actor string, isOwner bool) error {
	if t.Dealer < 0 {
		return fail(errInvalid, "Nobody is the dealer yet.")
	}
	if !isOwner && actor != t.DealerID() {
		return fail(errForbidden, "Only the dealer or the lobby owner can pass the deal.")
	}
	t.Dealer = (t.Dealer + 1) % len(t.Seats)
	t.logf(actor, "The deal passes to %s.", t.Name(t.DealerID()))
	return nil
}

// SetTurn shows whose turn it is ("" for nobody). It's only an indicator:
// nothing is refused because it isn't someone's turn.
func (t *Table) SetTurn(actor, player string, isOwner bool) error {
	cur := ""
	if t.Turn >= 0 && t.Turn < len(t.Seats) {
		cur = t.Seats[t.Turn]
	}
	if !isOwner && cur != "" && actor != cur && actor != t.DealerID() {
		return fail(errForbidden, "The player whose turn it is, the dealer or the lobby owner can change the turn.")
	}
	if !t.IsSeated(actor) && !isOwner {
		return fail(errForbidden, "Only seated players can change the turn.")
	}
	if player == "" {
		t.Turn = -1
		return nil
	}
	i := t.SeatOf(player)
	if i < 0 {
		return fail(errInvalid, "That player isn't seated.")
	}
	t.Turn = i
	t.logf(actor, "It's %s's turn.", t.Name(player))
	return nil
}

// AdvanceTurn passes the turn one seat clockwise (dir 1) or counterclockwise (-1).
func (t *Table) AdvanceTurn(actor string, dir int, isOwner bool) error {
	if t.Turn < 0 {
		return fail(errInvalid, "Nobody has the turn yet.")
	}
	if dir != 1 && dir != -1 {
		return fail(errBad, "Pass the turn left (1) or right (-1).")
	}
	n := len(t.Seats)
	return t.SetTurn(actor, t.Seats[((t.Turn+dir)%n+n)%n], isOwner)
}

// PileSpec describes a pile players add during play.
type PileSpec struct {
	Label  string `json:"label"`
	Kind   Kind   `json:"kind"`  // stack or grid
	Slots  int    `json:"slots"` // grids
	Cols   int    `json:"cols"`
	FaceUp bool   `json:"faceUp"`
	Spread bool   `json:"spread"`
}

// AddPile adds a shared pile during play; the lobby owner or the dealer may.
func (t *Table) AddPile(actor string, s PileSpec, isOwner bool, id string) error {
	if !isOwner && (!t.IsSeated(actor) || (t.DealerID() != "" && actor != t.DealerID() && t.Policy != Shared)) {
		return fail(errForbidden, "Only the dealer or the lobby owner can add piles.")
	}
	if l := len([]rune(s.Label)); l < 1 || l > 24 {
		return fail(errBad, "Name the pile in 1 to 24 characters.")
	}
	if s.Kind != Stack && s.Kind != Grid {
		return fail(errBad, "A pile is a stack or a grid.")
	}
	if len(t.Containers) >= 80 {
		return fail(errInvalid, "This table has as many piles as it can hold.")
	}
	c := &Container{ID: id, Kind: s.Kind, Label: s.Label, FaceUp: s.FaceUp, Spread: s.Spread || s.Kind == Grid, Cols: s.Cols, Peek: Nobody}
	slots := 0
	if s.Kind == Grid {
		slots = s.Slots
		if c.Cols < 1 || c.Cols > slots {
			c.Cols = min(slots, 6)
		}
	}
	if err := t.AddContainer(c, slots); err != nil {
		return fail(errBad, "A grid has 1 to 48 slots.")
	}
	t.logf(actor, "%s added %s.", t.Name(actor), s.Label)
	return nil
}

// RemovePile removes an empty shared pile.
func (t *Table) RemovePile(actor, id string, isOwner bool) error {
	c := t.byID[id]
	if c == nil || c.Owner != "" {
		return fail(errNotFound, "That pile isn't on the table.")
	}
	if !isOwner && !t.allowed(actor, c, Dealer) {
		return fail(errForbidden, "Only the dealer or the lobby owner can remove piles.")
	}
	if c.Count() > 0 {
		return fail(errInvalid, "Empty %s first.", c.Label)
	}
	if err := t.blocked(c); err != nil {
		return err
	}
	t.Containers = slices.DeleteFunc(t.Containers, func(x *Container) bool { return x == c })
	delete(t.byID, id)
	t.logf(actor, "%s removed %s.", t.Name(actor), c.Label)
	return nil
}
