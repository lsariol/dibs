package table

import (
	"fmt"
	"slices"
	"strings"
)

// DealPlan describes a deal: which pile, to whom in what order, and the
// packet sizes for each pass around the table.
type DealPlan struct {
	From   string   `json:"from"`
	To     []string `json:"to"`     // recipient containers, in dealing order
	Passes [][]int  `json:"passes"` // passes[p][r]: cards for recipient r in pass p; one number means everyone gets that many
	Face   Face     `json:"face"`   // "up", "down", or "" for each recipient's default
	Step   bool     `json:"step"`   // deal packet by packet, one action each
}

// PendingDeal is a step-by-step deal in progress.
type PendingDeal struct {
	Plan  DealPlan `json:"plan"`
	By    string   `json:"by"`
	Pass  int      `json:"pass"` // next packet's pass
	Next  int      `json:"next"` // next packet's recipient index
	Dealt int      `json:"dealt"`
	Total int      `json:"total"`
}

func (d *PendingDeal) clone() *PendingDeal {
	n := *d
	n.Plan.To = slices.Clone(d.Plan.To)
	n.Plan.Passes = make([][]int, len(d.Plan.Passes))
	for i, p := range d.Plan.Passes {
		n.Plan.Passes[i] = slices.Clone(p)
	}
	return &n
}

// size is the packet for recipient r in pass p.
func (p DealPlan) size(pass, r int) int {
	if len(p.Passes[pass]) == 1 {
		return p.Passes[pass][0]
	}
	return p.Passes[pass][r]
}

func (p DealPlan) total() int {
	n := 0
	for pass := range p.Passes {
		for r := range p.To {
			n += p.size(pass, r)
		}
	}
	return n
}

// validate checks a plan against the table and the actor's rights.
func (t *Table) validateDeal(actor string, p DealPlan) error {
	src := t.byID[p.From]
	if src == nil || src.Kind != Stack {
		return fail(errNotFound, "Choose a pile to deal from.")
	}
	if err := t.need(t.allowed(actor, src, src.Manage), "Only %s can deal from %s.", t.managerText(src), t.LabelOf(src)); err != nil {
		return err
	}
	if err := checkFace(p.Face); err != nil {
		return err
	}
	if len(p.To) < 1 || len(p.To) > 16 {
		return fail(errBad, "Deal to 1 to 16 places.")
	}
	if len(p.Passes) < 1 || len(p.Passes) > 20 {
		return fail(errBad, "A deal has 1 to 20 passes.")
	}
	for i, to := range p.To {
		c := t.byID[to]
		if c == nil || c == src {
			return fail(errNotFound, "One of the places to deal to isn't on the table.")
		}
		if slices.Contains(p.To[:i], to) {
			return fail(errBad, "Each place appears once; use more passes to deal it more.")
		}
		if !t.canPlace(actor, c) {
			return fail(errForbidden, "You can't deal into %s.", t.LabelOf(c))
		}
	}
	for _, pass := range p.Passes {
		if len(pass) != 1 && len(pass) != len(p.To) {
			return fail(errBad, "Each pass needs one packet size, or one per place.")
		}
		for _, n := range pass {
			if n < 0 || n > 52 {
				return fail(errBad, "Packets are 0 to 52 cards.")
			}
		}
	}
	total := p.total()
	if total == 0 {
		return fail(errBad, "That deal has no cards.")
	}
	if total > src.Count() {
		return fail(errInvalid, "That deal needs %d cards but %s has %d.", total, t.LabelOf(src), src.Count())
	}
	for r, to := range p.To {
		c := t.byID[to]
		if c.Kind != Grid {
			continue
		}
		need := 0
		for pass := range p.Passes {
			need += p.size(pass, r)
		}
		if free := len(c.Cards) - c.Count(); need > free {
			return fail(errInvalid, "%s has room for %d more cards, not %d.", t.LabelOf(c), free, need)
		}
	}
	return nil
}

// Deal runs a plan: all at once, or (Step) sets it up for DealStep.
func (t *Table) Deal(actor string, p DealPlan) error {
	if t.Dealing != nil {
		return fail(errInvalid, "%s is already dealing. Finish or cancel that deal first.", t.Name(t.Dealing.By))
	}
	if err := t.validateDeal(actor, p); err != nil {
		return err
	}
	if err := t.blocked(t.byID[p.From]); err != nil {
		return err
	}
	d := &PendingDeal{Plan: p, By: actor, Total: p.total()}
	if p.Step {
		t.Dealing = d
		t.logf(actor, "%s started dealing %s from %s to %s.", t.Name(actor), plural(d.Total, "card", "cards"), t.LabelOf(t.byID[p.From]), t.recipients(p))
		return nil
	}
	for !d.done() {
		if err := t.dealPacket(d); err != nil {
			return err
		}
	}
	t.logf(actor, "%s dealt %s from %s to %s, %s.", t.Name(actor), plural(d.Total, "card", "cards"), t.LabelOf(t.byID[p.From]), t.recipients(p), passesText(p))
	return nil
}

// DealStep deals the next packet of a step-by-step deal; all = the rest at once.
func (t *Table) DealStep(actor string, all bool) error {
	d := t.Dealing
	if d == nil {
		return fail(errInvalid, "No deal is in progress.")
	}
	if d.By != actor {
		return fail(errForbidden, "%s is dealing.", t.Name(d.By))
	}
	src := t.byID[d.Plan.From]
	if t.CutOffer != nil && t.CutOffer.Container == src.ID {
		return fail(errInvalid, "Waiting for %s to cut or decline.", t.Name(t.CutOffer.To))
	}
	for {
		r, n := d.Next, d.Plan.size(d.Pass, d.Next)
		if src.Count() < n {
			return fail(errInvalid, "%s ran out of cards. Cancel the deal.", t.LabelOf(src))
		}
		if err := t.dealPacket(d); err != nil {
			return err
		}
		if !all && n > 0 {
			t.logf(actor, "%s dealt %s to %s.", t.Name(actor), plural(n, "card", "cards"), t.LabelOf(t.byID[d.Plan.To[r]]))
		}
		if d.done() {
			t.Dealing = nil
			t.logf(actor, "%s finished dealing.", t.Name(actor))
			return nil
		}
		if !all && n > 0 {
			return nil
		}
	}
}

// CancelDeal stops a step-by-step deal; cards already dealt stay where they are.
func (t *Table) CancelDeal(actor string, isOwner bool) error {
	d := t.Dealing
	if d == nil {
		return fail(errInvalid, "No deal is in progress.")
	}
	if d.By != actor && !isOwner {
		return fail(errForbidden, "Only %s can stop this deal.", t.Name(d.By))
	}
	t.Dealing = nil
	t.logf(actor, "%s stopped the deal after %d of %d cards.", t.Name(actor), d.Dealt, d.Total)
	return nil
}

func (d *PendingDeal) done() bool { return d.Pass >= len(d.Plan.Passes) }

// dealPacket moves the next packet from the top of the pile and advances the deal.
func (t *Table) dealPacket(d *PendingDeal) error {
	src := t.byID[d.Plan.From]
	dst := t.byID[d.Plan.To[d.Next]]
	n := d.Plan.size(d.Pass, d.Next)
	for range n {
		i := src.top()
		if i < 0 {
			return fail(errInvalid, "%s ran out of cards.", t.LabelOf(src))
		}
		id := src.removeAt(i)
		if err := t.put(dst, id, -1, d.Plan.Face); err != nil {
			return err
		}
	}
	src.ModRev = t.Rev
	d.Dealt += n
	d.advance()
	for !d.done() && d.Plan.size(d.Pass, d.Next) == 0 {
		d.advance()
	}
	return nil
}

func (d *PendingDeal) advance() {
	d.Next++
	if d.Next >= len(d.Plan.To) {
		d.Next = 0
		d.Pass++
	}
}

func (t *Table) recipients(p DealPlan) string {
	var names []string
	for _, to := range p.To {
		names = append(names, t.LabelOf(t.byID[to]))
	}
	return strings.Join(names, ", ")
}

func passesText(p DealPlan) string {
	var parts []string
	for pass := range p.Passes {
		var sizes []string
		for r := range p.To {
			sizes = append(sizes, fmt.Sprint(p.size(pass, r)))
		}
		parts = append(parts, strings.Join(sizes, "-"))
	}
	return plural(len(p.Passes), "pass", "passes") + " (" + strings.Join(parts, ", ") + ")"
}

// Transfer moves the top n cards of one container to another without
// checking the actor's access: for presets running a procedure they have
// already authorized (a Blackjack hit). It's logged like any deal.
func (t *Table) Transfer(by, from, to string, n int, face Face) error {
	src, dst := t.byID[from], t.byID[to]
	if src == nil || dst == nil || src == dst {
		return fail(errNotFound, "That pile isn't on the table.")
	}
	for _, c := range []*Container{src, dst} {
		if err := t.blocked(c); err != nil {
			return err
		}
	}
	if src.Count() < n {
		return fail(errInvalid, "%s has only %s.", t.LabelOf(src), plural(src.Count(), "card", "cards"))
	}
	var public []string
	for range n {
		id := src.removeAt(src.top())
		if err := t.put(dst, id, -1, face); err != nil {
			return err
		}
		if t.Cards[id].FaceUp {
			public = append(public, t.Cards[id].Def.Label())
		}
	}
	src.ModRev = t.Rev
	note := ""
	if len(public) > 0 {
		note = ": " + strings.Join(public, " ")
	}
	t.logf(by, "%s dealt %s from %s to %s%s.", t.Name(by), plural(n, "card", "cards"), t.LabelOf(src), t.LabelOf(dst), note)
	return nil
}

// DealOrder returns the seated players in dealing order: starting from the
// dealer's left going clockwise (dir 1), or the dealer's right going
// counterclockwise (dir -1). With no dealer it starts at the first seat.
func (t *Table) DealOrder(dir int) []string {
	n := len(t.Seats)
	start := 0
	if t.Dealer >= 0 {
		start = t.Dealer + dir
	}
	out := make([]string, 0, n)
	for i := range n {
		out = append(out, t.Seats[((start+dir*i)%n+n)%n])
	}
	return out
}
