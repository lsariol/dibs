package table

import (
	"fmt"
	"strings"
)

// Error is a refused action, with a code the client understands.
type Error struct {
	Code string
	Msg  string
}

func (e *Error) Error() string     { return e.Msg }
func (e *Error) ErrorCode() string { return e.Code }

func fail(code, format string, args ...any) error {
	return &Error{Code: code, Msg: fmt.Sprintf(format, args...)}
}

// Error codes, matching the lobby's.
const (
	errForbidden = "forbidden"
	errNotFound  = "not_found"
	errInvalid   = "invalid"
	errStale     = "stale"
	errBad       = "bad_request"
)

// allowed reports whether player may act on c with the given access level.
// Personal containers allow only their owner, whatever the access says.
func (t *Table) allowed(player string, c *Container, a Access) bool {
	if !t.IsSeated(player) {
		return false
	}
	if c.Owner != "" {
		return player == c.Owner
	}
	switch a {
	case Anyone:
		return true
	case Dealer:
		return t.Policy == Shared || t.DealerID() == "" || player == t.DealerID()
	}
	return false
}

// canPlace reports whether player may put cards into c. Anyone seated may
// give a card to another player's hand or grid (that's how dealing works);
// shared containers follow their Place access.
func (t *Table) canPlace(player string, c *Container) bool {
	if !t.IsSeated(player) {
		return false
	}
	if c.Owner != "" {
		return true
	}
	return t.allowed(player, c, c.Place)
}

func (t *Table) need(ok bool, format string, args ...any) error {
	if !ok {
		return fail(errForbidden, format, args...)
	}
	return nil
}

// Visible reports whether viewer may know a card's identity: it's face up,
// it's in the viewer's own hand, or the viewer peeked at it.
func (t *Table) Visible(cs *CardState, viewer string) bool {
	if cs.FaceUp {
		return true
	}
	if c := t.byID[cs.Loc]; c != nil && c.Kind == Hand && c.Owner == viewer {
		return true
	}
	return cs.Known[viewer]
}

// Viewers returns, for every card, which seated players may know it; the
// lobby compares this before and after an action to refuse unsafe undo.
func (t *Table) Viewers() map[string]map[string]bool {
	out := make(map[string]map[string]bool, len(t.Cards))
	for id, cs := range t.Cards {
		m := map[string]bool{}
		for _, p := range t.Seats {
			if t.Visible(cs, p) {
				m[p] = true
			}
		}
		out[id] = m
	}
	return out
}

// LabelOf is a container's display name; personal ones get the owner's name.
func (t *Table) LabelOf(c *Container) string {
	if c.Owner != "" {
		return t.Name(c.Owner) + "'s " + strings.ToLower(c.Label)
	}
	return c.Label
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
