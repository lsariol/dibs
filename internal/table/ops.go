package table

import (
	"cmp"
	"slices"
	"strings"

	"github.com/lsariol/dibs/internal/cards"
	"github.com/lsariol/dibs/internal/random"
)

// Ref points at one card: by ID when the client can see it, otherwise by
// position (Index -1 is the top card).
type Ref struct {
	Container string `json:"container"`
	Index     int    `json:"index"`
	Card      string `json:"card,omitempty"`
}

// Face asks for a card's face after an action: "up", "down", or "" for the
// destination's default.
type Face string

// resolve finds the card a reference points at. clientRev is the revision
// the client saw; positions in ordered containers that changed since are stale.
func (t *Table) resolve(actor string, r Ref, clientRev uint64) (*Container, int, error) {
	c := t.byID[r.Container]
	if c == nil {
		return nil, 0, fail(errNotFound, "That pile isn't on the table.")
	}
	if r.Card != "" {
		cs := t.Cards[r.Card]
		if cs == nil || cs.Loc != c.ID || !t.Visible(cs, actor) {
			return nil, 0, fail(errStale, "That card has moved. Try again.")
		}
		return c, slices.Index(c.Cards, r.Card), nil
	}
	if r.Index == -1 {
		i := c.top()
		if i < 0 {
			return nil, 0, fail(errInvalid, "%s is empty.", t.LabelOf(c))
		}
		return c, i, nil
	}
	if r.Index < 0 || r.Index >= len(c.Cards) || c.Cards[r.Index] == "" {
		return nil, 0, fail(errStale, "There's no card there any more. Try again.")
	}
	if c.Kind != Grid && clientRev < c.ModRev {
		return nil, 0, fail(errStale, "%s changed while you were choosing. Try again.", t.LabelOf(c))
	}
	return c, r.Index, nil
}

// blocked refuses actions on a container with a pending cut offer or deal.
func (t *Table) blocked(c *Container) error {
	if t.CutOffer != nil && t.CutOffer.Container == c.ID {
		return fail(errInvalid, "Waiting for %s to cut or decline.", t.Name(t.CutOffer.To))
	}
	if t.Dealing != nil && t.Dealing.Plan.From == c.ID {
		return fail(errInvalid, "%s is being dealt from. Finish or cancel the deal first.", t.LabelOf(c))
	}
	return nil
}

// cardText names a card for the history when everyone can see it, else "a card".
func (t *Table) cardText(cs *CardState, public bool) string {
	if public {
		return cs.Def.Label()
	}
	return "a card"
}

// removeAt takes the card at index i out of c.
func (c *Container) removeAt(i int) string {
	id := c.Cards[i]
	if c.Kind == Grid {
		c.Cards[i] = ""
	} else {
		c.Cards = slices.Delete(c.Cards, i, i+1)
	}
	return id
}

// put places card id into c at index (-1 or past the end: the top, or the
// first empty slot of a grid). A grid slot must be empty.
func (t *Table) put(c *Container, id string, index int, face Face) error {
	cs := t.Cards[id]
	if c.Kind == Grid {
		if index < 0 {
			index = slices.Index(c.Cards, "")
			if index < 0 {
				return fail(errInvalid, "%s has no empty slot.", t.LabelOf(c))
			}
		}
		if index >= len(c.Cards) {
			return fail(errInvalid, "%s has no slot %d.", t.LabelOf(c), index+1)
		}
		if c.Cards[index] != "" {
			return fail(errInvalid, "That slot in %s is taken.", t.LabelOf(c))
		}
		c.Cards[index] = id
	} else if index < 0 || index >= len(c.Cards) {
		c.Cards = append(c.Cards, id)
	} else {
		c.Cards = slices.Insert(c.Cards, index, id)
	}
	if cs.Loc != c.ID {
		cs.Known = nil
	}
	cs.Loc = c.ID
	switch face {
	case "up":
		cs.FaceUp = true
	case "down":
		cs.FaceUp = false
	default:
		cs.FaceUp = c.FaceUp
	}
	c.ModRev = t.Rev
	return nil
}

func checkFace(f Face) error {
	if f != "" && f != "up" && f != "down" {
		return fail(errBad, "Face must be up, down or empty.")
	}
	return nil
}

// Move moves one card. index is the destination position (-1 for the top or
// the first empty slot). displaceTo, if set, receives the card already in a
// grid slot (a Golf swap).
func (t *Table) Move(actor string, r Ref, to string, index int, face Face, displaceTo string, clientRev uint64) error {
	if err := checkFace(face); err != nil {
		return err
	}
	src, i, err := t.resolve(actor, r, clientRev)
	if err != nil {
		return err
	}
	dst := t.byID[to]
	if dst == nil {
		return fail(errNotFound, "That destination isn't on the table.")
	}
	if err := t.need(t.allowed(actor, src, src.Take), "You can't take cards from %s.", t.LabelOf(src)); err != nil {
		return err
	}
	if err := t.need(t.canPlace(actor, dst), "You can't put cards in %s.", t.LabelOf(dst)); err != nil {
		return err
	}
	for _, c := range []*Container{src, dst} {
		if err := t.blocked(c); err != nil {
			return err
		}
	}
	if dst.Kind == Grid && index >= len(dst.Cards) {
		return fail(errInvalid, "%s has no slot %d.", t.LabelOf(dst), index+1)
	}
	id := src.Cards[i]
	cs := t.Cards[id]
	wasPublic := cs.FaceUp

	if src == dst && face == "" {
		face = map[bool]Face{true: "up", false: "down"}[cs.FaceUp]
	}
	var displaced string
	if dst.Kind == Grid && index >= 0 && index < len(dst.Cards) && dst.Cards[index] != "" && !(src == dst && index == i) {
		if displaceTo == "" {
			return fail(errInvalid, "That slot in %s is taken.", t.LabelOf(dst))
		}
		if err := t.need(t.allowed(actor, dst, dst.Take), "You can't take cards from %s.", t.LabelOf(dst)); err != nil {
			return err
		}
		dd := t.byID[displaceTo]
		if dd == nil || dd == dst {
			return fail(errNotFound, "Choose where the replaced card goes.")
		}
		if err := t.need(t.canPlace(actor, dd), "You can't put cards in %s.", t.LabelOf(dd)); err != nil {
			return err
		}
		displaced = dst.removeAt(index)
		if err := t.put(dd, displaced, -1, ""); err != nil {
			return err
		}
	}

	src.removeAt(i)
	src.ModRev = t.Rev
	if src == dst && src.Kind != Grid && index > i {
		index--
	}
	if err := t.put(dst, id, index, face); err != nil {
		return err
	}

	public := wasPublic || cs.FaceUp
	switch {
	case src == dst:
		t.logf(actor, "%s rearranged %s.", t.Name(actor), t.LabelOf(src))
	default:
		t.logf(actor, "%s moved %s from %s to %s%s.", t.Name(actor), t.cardText(cs, public), t.LabelOf(src), t.LabelOf(dst), faceNote(cs, public))
	}
	if displaced != "" {
		dcs := t.Cards[displaced]
		t.logf(actor, "%s put the replaced card, %s, in %s.", t.Name(actor), t.cardText(dcs, dcs.FaceUp), t.LabelOf(t.byID[displaceTo]))
	}
	return nil
}

func faceNote(cs *CardState, public bool) string {
	if !public {
		return ""
	}
	if cs.FaceUp {
		return " face up"
	}
	return " face down"
}

// Flip turns one card over in place.
func (t *Table) Flip(actor string, r Ref, clientRev uint64) error {
	c, i, err := t.resolve(actor, r, clientRev)
	if err != nil {
		return err
	}
	if err := t.need(t.allowed(actor, c, c.Take), "You can't flip cards in %s.", t.LabelOf(c)); err != nil {
		return err
	}
	if err := t.blocked(c); err != nil {
		return err
	}
	if c.Kind == Stack && !c.Spread && i != c.top() {
		return fail(errInvalid, "Only the top card of %s can be flipped.", t.LabelOf(c))
	}
	cs := t.Cards[c.Cards[i]]
	cs.FaceUp = !cs.FaceUp
	c.ModRev = t.Rev
	if cs.FaceUp {
		t.logf(actor, "%s turned up %s in %s.", t.Name(actor), cs.Def.Label(), t.LabelOf(c))
	} else {
		t.logf(actor, "%s turned %s face down in %s.", t.Name(actor), cs.Def.Label(), t.LabelOf(c))
	}
	return nil
}

// Peek lets the actor alone see a face-down card. Everyone is told it happened.
func (t *Table) Peek(actor string, r Ref, clientRev uint64) error {
	c, i, err := t.resolve(actor, r, clientRev)
	if err != nil {
		return err
	}
	if err := t.need(t.allowed(actor, c, c.Peek), "You can't peek at cards in %s.", t.LabelOf(c)); err != nil {
		return err
	}
	cs := t.Cards[c.Cards[i]]
	if t.Visible(cs, actor) {
		return fail(errInvalid, "You can already see that card.")
	}
	if cs.Known == nil {
		cs.Known = map[string]bool{}
	}
	cs.Known[actor] = true
	t.logf(actor, "%s peeked at a card in %s.", t.Name(actor), t.LabelOf(c))
	return nil
}

// RevealHand turns every card in the actor's hand face up (a showdown), or face down again.
func (t *Table) RevealHand(actor string, up bool) error {
	c := t.byID[t.HandOf(actor)]
	if c == nil {
		return fail(errNotFound, "You don't have a hand at this table.")
	}
	if len(c.Cards) == 0 {
		return fail(errInvalid, "Your hand is empty.")
	}
	var names []string
	for _, id := range c.Cards {
		t.Cards[id].FaceUp = up
		names = append(names, t.Cards[id].Def.Label())
	}
	c.ModRev = t.Rev
	if up {
		t.logf(actor, "%s showed their hand: %s.", t.Name(actor), strings.Join(names, " "))
	} else {
		t.logf(actor, "%s picked their hand back up.", t.Name(actor))
	}
	return nil
}

// SortHand orders the actor's own hand by suit then rank, or by rank then suit.
func (t *Table) SortHand(actor string, by string) error {
	c := t.byID[t.HandOf(actor)]
	if c == nil {
		return fail(errNotFound, "You don't have a hand at this table.")
	}
	rank := func(d cards.Def) int {
		if d.Kind == "color" {
			return slices.Index([]string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "skip", "reverse", "draw2", "wild", "wild4"}, d.Rank)
		}
		return slices.Index([]string{"2", "3", "4", "5", "6", "7", "8", "9", "10", "J", "Q", "K", "A"}, d.Rank)
	}
	suit := func(d cards.Def) string { return d.Kind + d.Suit + d.Color }
	slices.SortStableFunc(c.Cards, func(a, b string) int {
		da, db := t.Cards[a].Def, t.Cards[b].Def
		if by == "rank" {
			return cmp.Or(cmp.Compare(rank(da), rank(db)), cmp.Compare(suit(da), suit(db)))
		}
		return cmp.Or(cmp.Compare(suit(da), suit(db)), cmp.Compare(rank(da), rank(db)))
	})
	c.ModRev = t.Rev
	t.logf(actor, "%s sorted their hand.", t.Name(actor))
	return nil
}

// Shuffle shuffles a pile face down with crypto/rand. Peeks are forgotten.
func (t *Table) Shuffle(actor, container string) error {
	c := t.byID[container]
	if c == nil {
		return fail(errNotFound, "That pile isn't on the table.")
	}
	if c.Kind == Grid {
		return fail(errInvalid, "A grid can't be shuffled; collect it into a pile first.")
	}
	if err := t.need(t.allowed(actor, c, c.Manage), "Only %s can shuffle %s.", t.managerText(c), t.LabelOf(c)); err != nil {
		return err
	}
	if err := t.blocked(c); err != nil {
		return err
	}
	if len(c.Cards) < 2 {
		return fail(errInvalid, "There's nothing to shuffle in %s.", t.LabelOf(c))
	}
	cards.Shuffle(c.Cards)
	for _, id := range c.Cards {
		t.Cards[id].FaceUp = false
		t.Cards[id].Known = nil
	}
	c.ModRev = t.Rev
	t.logf(actor, "%s shuffled %s (%s).", t.Name(actor), t.LabelOf(c), plural(len(c.Cards), "card", "cards"))
	return nil
}

// managerText says who manages a container, for refusals.
func (t *Table) managerText(c *Container) string {
	if c.Owner != "" {
		return t.Name(c.Owner)
	}
	if c.Manage == Dealer && t.DealerID() != "" {
		return "the dealer (" + t.Name(t.DealerID()) + ")"
	}
	return "the dealer"
}

// Cut cuts a pile: the top k cards go underneath.
func (t *Table) Cut(actor, container string, k int) error {
	c := t.byID[container]
	if c == nil {
		return fail(errNotFound, "That pile isn't on the table.")
	}
	if err := t.need(t.allowed(actor, c, c.Manage), "Only %s can cut %s.", t.managerText(c), t.LabelOf(c)); err != nil {
		return err
	}
	if err := t.blocked(c); err != nil {
		return err
	}
	return t.cut(actor, c, k)
}

func (t *Table) cut(actor string, c *Container, k int) error {
	if c.Kind != Stack {
		return fail(errInvalid, "Only a pile can be cut.")
	}
	if err := cards.Cut(c.Cards, k); err != nil {
		return fail(errInvalid, "Cut between 1 and %d cards from the top.", max(len(c.Cards)-1, 1))
	}
	for _, id := range c.Cards {
		t.Cards[id].Known = nil
	}
	c.ModRev = t.Rev
	t.logf(actor, "%s cut %s, lifting %s off the top.", t.Name(actor), t.LabelOf(c), plural(k, "card", "cards"))
	return nil
}

// OfferCut asks another player to cut a pile; nothing is dealt from it until they answer.
func (t *Table) OfferCut(actor, container, to string) error {
	c := t.byID[container]
	if c == nil {
		return fail(errNotFound, "That pile isn't on the table.")
	}
	if err := t.need(t.allowed(actor, c, c.Manage), "Only %s can offer a cut of %s.", t.managerText(c), t.LabelOf(c)); err != nil {
		return err
	}
	if t.CutOffer != nil {
		return fail(errInvalid, "A cut is already offered to %s.", t.Name(t.CutOffer.To))
	}
	if err := t.blocked(c); err != nil {
		return err
	}
	if !t.IsSeated(to) || to == actor {
		return fail(errInvalid, "Offer the cut to another seated player.")
	}
	if c.Kind != Stack || len(c.Cards) < 2 {
		return fail(errInvalid, "%s needs at least 2 cards to cut.", t.LabelOf(c))
	}
	t.CutOffer = &CutOffer{Container: c.ID, By: actor, To: to}
	t.logf(actor, "%s offered %s the cut.", t.Name(actor), t.Name(to))
	return nil
}

// AnswerCut is the offered player's answer: cut k cards from the top, or decline (k = 0).
func (t *Table) AnswerCut(actor string, k int) error {
	o := t.CutOffer
	if o == nil {
		return fail(errInvalid, "No cut is being offered.")
	}
	if o.To != actor {
		return fail(errForbidden, "The cut was offered to %s.", t.Name(o.To))
	}
	c := t.byID[o.Container]
	if k == 0 {
		t.CutOffer = nil
		t.logf(actor, "%s declined to cut.", t.Name(actor))
		return nil
	}
	if err := t.cut(actor, c, k); err != nil {
		return err
	}
	t.CutOffer = nil
	return nil
}

// CancelCut withdraws an offer; the dealer who made it, or the lobby owner, may.
func (t *Table) CancelCut(actor string, isOwner bool) error {
	o := t.CutOffer
	if o == nil {
		return fail(errInvalid, "No cut is being offered.")
	}
	if actor != o.By && !isOwner {
		return fail(errForbidden, "Only %s can withdraw the offer.", t.Name(o.By))
	}
	t.CutOffer = nil
	t.logf(actor, "%s withdrew the offer to cut.", t.Name(actor))
	return nil
}

// Draw moves the top count cards of a pile to another container (by default the actor's hand).
func (t *Table) Draw(actor, from, to string, count int, face Face) error {
	if err := checkFace(face); err != nil {
		return err
	}
	src := t.byID[from]
	if src == nil {
		return fail(errNotFound, "That pile isn't on the table.")
	}
	if !t.IsSeated(actor) {
		return fail(errForbidden, "Only seated players can draw.")
	}
	if to == "" {
		to = t.HandOf(actor)
	}
	dst := t.byID[to]
	if dst == nil || dst == src {
		return fail(errNotFound, "Choose where the cards go.")
	}
	if count < 1 || count > 52 {
		return fail(errBad, "Draw 1 to 52 cards.")
	}
	if err := t.need(t.allowed(actor, src, src.Take), "You can't take cards from %s.", t.LabelOf(src)); err != nil {
		return err
	}
	if err := t.need(t.canPlace(actor, dst), "You can't put cards in %s.", t.LabelOf(dst)); err != nil {
		return err
	}
	for _, c := range []*Container{src, dst} {
		if err := t.blocked(c); err != nil {
			return err
		}
	}
	if src.Count() < count {
		return fail(errInvalid, "%s has only %s.", t.LabelOf(src), plural(src.Count(), "card", "cards"))
	}
	var public []string
	for range count {
		id := src.removeAt(src.top())
		wasUp := t.Cards[id].FaceUp
		if err := t.put(dst, id, -1, face); err != nil {
			return err
		}
		if wasUp || t.Cards[id].FaceUp {
			public = append(public, t.Cards[id].Def.Label())
		}
	}
	src.ModRev = t.Rev
	note := ""
	if len(public) > 0 {
		note = ": " + strings.Join(public, " ")
	}
	t.logf(actor, "%s moved %s from %s to %s%s.", t.Name(actor), plural(count, "card", "cards"), t.LabelOf(src), t.LabelOf(dst), note)
	return nil
}

// Collect moves every card of one container onto another, keeping their order.
func (t *Table) Collect(actor, from, to string, face Face) error {
	if err := checkFace(face); err != nil {
		return err
	}
	src, dst := t.byID[from], t.byID[to]
	if src == nil || dst == nil || src == dst {
		return fail(errNotFound, "Choose two different piles.")
	}
	if err := t.need(t.allowed(actor, src, src.Take), "You can't take cards from %s.", t.LabelOf(src)); err != nil {
		return err
	}
	if err := t.need(t.canPlace(actor, dst), "You can't put cards in %s.", t.LabelOf(dst)); err != nil {
		return err
	}
	for _, c := range []*Container{src, dst} {
		if err := t.blocked(c); err != nil {
			return err
		}
	}
	n := src.Count()
	if n == 0 {
		return fail(errInvalid, "%s is empty.", t.LabelOf(src))
	}
	for _, id := range slices.Clone(src.Cards) {
		if id == "" {
			continue
		}
		src.removeAt(slices.Index(src.Cards, id))
		if err := t.put(dst, id, -1, face); err != nil {
			return err
		}
	}
	src.ModRev = t.Rev
	t.logf(actor, "%s moved %s (%s) to %s.", t.Name(actor), t.LabelOf(src), plural(n, "card", "cards"), t.LabelOf(dst))
	return nil
}

// CollectAll gathers every card on the table into one pile, face down, ready
// to shuffle: the end of a hand. The dealer or the lobby owner may.
func (t *Table) CollectAll(actor, to string, isOwner bool) error {
	dst := t.byID[to]
	if dst == nil || dst.Kind != Stack {
		return fail(errNotFound, "Choose the pile to gather the cards into.")
	}
	if !isOwner && !t.allowed(actor, dst, Dealer) {
		return fail(errForbidden, "Only the dealer or the lobby owner can gather all the cards.")
	}
	for _, c := range t.Containers {
		if c == dst {
			continue
		}
		for _, id := range c.Cards {
			if id != "" {
				dst.Cards = append(dst.Cards, id)
				t.Cards[id].Loc = dst.ID
			}
		}
		if c.Kind == Grid {
			clear(c.Cards)
		} else {
			c.Cards = c.Cards[:0]
		}
		c.ModRev = t.Rev
	}
	for _, id := range dst.Cards {
		t.Cards[id].FaceUp = false
		t.Cards[id].Known = nil
	}
	dst.ModRev = t.Rev
	t.CutOffer = nil
	t.Dealing = nil
	t.logf(actor, "%s gathered all %s into %s.", t.Name(actor), plural(len(dst.Cards), "card", "cards"), t.LabelOf(dst))
	return nil
}

// BlindDraw takes an unseen card from another player's hand into the actor's
// hand (Old Maid). index picks a position among their card backs; -1 picks at
// random. Nothing about the other cards is revealed.
func (t *Table) BlindDraw(actor, from string, index int) error {
	if !t.AllowBlindDraw {
		return fail(errForbidden, "Blind draws aren't allowed at this table. The lobby owner can turn them on.")
	}
	src := t.byID[from]
	if src == nil || src.Kind != Hand || src.Owner == "" {
		return fail(errNotFound, "Choose another player's hand.")
	}
	if src.Owner == actor {
		return fail(errInvalid, "That's your own hand.")
	}
	dst := t.byID[t.HandOf(actor)]
	if dst == nil || !t.IsSeated(actor) {
		return fail(errForbidden, "Only seated players with a hand can draw.")
	}
	n := len(src.Cards)
	if n == 0 {
		return fail(errInvalid, "%s is empty.", t.LabelOf(src))
	}
	if index == -1 {
		index = random.Intn(n)
	}
	if index < 0 || index >= n {
		return fail(errStale, "That hand has fewer cards now. Try again.")
	}
	id := src.removeAt(index)
	src.ModRev = t.Rev
	if err := t.put(dst, id, -1, "down"); err != nil {
		return err
	}
	t.logf(actor, "%s drew a card blindly from %s.", t.Name(actor), t.LabelOf(src))
	return nil
}
