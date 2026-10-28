package lobby

import (
	"fmt"
	"log/slog"

	"github.com/lsariol/dibs/internal/cards"
	"github.com/lsariol/dibs/internal/presets"
	"github.com/lsariol/dibs/internal/table"
)

// game is a running table.
type game struct {
	table  *table.Table
	preset presets.Preset
	undo   *undoPoint
	piles  int // counter for IDs of piles added during play
}

// undoPoint lets the last action be taken back by whoever did it, as long
// as nothing has happened since and it revealed nothing to anyone.
type undoPoint struct {
	rev   uint64
	actor string
	prev  *table.Table
	text  string
}

// startGame builds a fresh table from the preset with the seated players.
func (l *Lobby) startGame() error {
	p := presets.Get(l.Settings.Preset)
	if p == nil {
		return errorf(CodeInvalid, "Unknown game %q.", l.Settings.Preset)
	}
	var seats []string
	teams := map[string]string{}
	for _, pl := range l.seated() {
		seats = append(seats, pl.ID)
		if pl.Team != "" {
			teams[pl.ID] = pl.Team
		}
	}
	policy := table.Strict
	if l.Settings.Policy == PolicyShared {
		policy = table.Shared
	}
	t := table.New(seats, policy)
	t.AllowBlindDraw = l.Settings.BlindDraw
	t.Name = l.name
	err := p.Setup(t, presets.Options{
		Seats:     seats,
		Teams:     teams,
		DeckType:  l.Settings.DeckType,
		DeckCount: l.Settings.DeckCount,
		Custom:    cards.Composition{Ranks: l.Settings.Custom.Ranks, Suits: l.Settings.Custom.Suits, Jokers: l.Settings.Custom.Jokers},
		GridRows:  l.Settings.GridRows,
		GridCols:  l.Settings.GridCols,
	})
	if err != nil {
		return errorf(CodeInvalid, "%s.", err.Error())
	}
	if err := t.Check(); err != nil {
		return fmt.Errorf("lobby: preset %s built a broken table: %w", p.Info().ID, err)
	}
	l.game = &game{table: t, preset: p}
	return nil
}

// tableFn is one table action.
type tableFn func(c *actx, t *table.Table) error

// onTable wraps a table action: it runs on a clone, which replaces the table
// only if the action succeeds and the table still checks out. undoable
// actions can be taken back if they revealed nothing.
func onTable(undoable bool, fn tableFn) handler {
	return func(c *actx) error {
		g := c.l.game
		if c.l.State != Playing || g == nil {
			return errorf(CodeInvalid, "The table hasn't started yet.")
		}
		next := g.table.Clone()
		next.Rev = c.l.rev + 1
		next.Name = c.l.name
		var before map[string]map[string]bool
		if undoable {
			before = g.table.Viewers()
		}
		if err := fn(c, next); err != nil {
			return err
		}
		if err := next.Check(); err != nil {
			slog.Error("lobby: an action broke the table; refused", "action", c.req.Action, "lobby", c.l.Code, "err", err)
			return errorf(CodeInvalid, "That would have broken the table, so nothing changed.")
		}
		events := next.TakeEvents()
		for _, e := range events {
			c.l.addLog(e.Actor, "%s", e.Text)
		}
		g.undo = nil
		if undoable && len(events) > 0 && !revealsAnything(before, next.Viewers()) {
			g.undo = &undoPoint{rev: c.l.rev + 1, actor: c.p.ID, prev: g.table, text: events[len(events)-1].Text}
		}
		g.table = next
		return nil
	}
}

// revealsAnything reports whether any card became visible to anyone new.
func revealsAnything(before, after map[string]map[string]bool) bool {
	for id, viewers := range after {
		for p := range viewers {
			if !before[id][p] {
				return true
			}
		}
	}
	return false
}

func (c *actx) isOwner() bool { return c.l.OwnerID == c.p.ID }

func init() {
	register("table.start", actStart)
	register("table.stop", actStop)
	register("table.reset", actReset)
	register("action.undo", actUndo)

	register("card.move", onTable(true, func(c *actx, t *table.Table) error {
		var a struct {
			Ref        table.Ref  `json:"ref"`
			To         string     `json:"to"`
			Index      *int       `json:"index"`
			Face       table.Face `json:"face"`
			DisplaceTo string     `json:"displaceTo"`
		}
		if err := decode(c, &a); err != nil {
			return err
		}
		idx := -1
		if a.Index != nil {
			idx = *a.Index
		}
		return t.Move(c.p.ID, a.Ref, a.To, idx, a.Face, a.DisplaceTo, c.req.Rev)
	}))
	register("card.flip", onTable(true, func(c *actx, t *table.Table) error {
		var a struct {
			Ref table.Ref `json:"ref"`
		}
		if err := decode(c, &a); err != nil {
			return err
		}
		return t.Flip(c.p.ID, a.Ref, c.req.Rev)
	}))
	register("card.peek", onTable(false, func(c *actx, t *table.Table) error {
		var a struct {
			Ref table.Ref `json:"ref"`
		}
		if err := decode(c, &a); err != nil {
			return err
		}
		return t.Peek(c.p.ID, a.Ref, c.req.Rev)
	}))
	register("card.draw", onTable(true, func(c *actx, t *table.Table) error {
		var a struct {
			From  string     `json:"from"`
			To    string     `json:"to"`
			Count int        `json:"count"`
			Face  table.Face `json:"face"`
		}
		if err := decode(c, &a); err != nil {
			return err
		}
		if a.Count == 0 {
			a.Count = 1
		}
		return t.Draw(c.p.ID, a.From, a.To, a.Count, a.Face)
	}))
	register("card.blind_draw", onTable(false, func(c *actx, t *table.Table) error {
		var a struct {
			From  string `json:"from"`
			Index int    `json:"index"`
		}
		if err := decode(c, &a); err != nil {
			return err
		}
		return t.BlindDraw(c.p.ID, a.From, a.Index)
	}))
	register("hand.reveal", onTable(true, func(c *actx, t *table.Table) error {
		var a struct {
			Up bool `json:"up"`
		}
		if err := decode(c, &a); err != nil {
			return err
		}
		return t.RevealHand(c.p.ID, a.Up)
	}))
	register("hand.sort", onTable(true, func(c *actx, t *table.Table) error {
		var a struct {
			By string `json:"by"`
		}
		if err := decode(c, &a); err != nil {
			return err
		}
		return t.SortHand(c.p.ID, a.By)
	}))
	register("pile.shuffle", onTable(false, func(c *actx, t *table.Table) error {
		var a struct {
			Container string `json:"container"`
		}
		if err := decode(c, &a); err != nil {
			return err
		}
		return t.Shuffle(c.p.ID, a.Container)
	}))
	register("pile.cut", onTable(false, func(c *actx, t *table.Table) error {
		var a struct {
			Container string `json:"container"`
			Count     int    `json:"count"`
		}
		if err := decode(c, &a); err != nil {
			return err
		}
		return t.Cut(c.p.ID, a.Container, a.Count)
	}))
	register("pile.collect", onTable(true, func(c *actx, t *table.Table) error {
		var a struct {
			From string     `json:"from"`
			To   string     `json:"to"`
			Face table.Face `json:"face"`
		}
		if err := decode(c, &a); err != nil {
			return err
		}
		return t.Collect(c.p.ID, a.From, a.To, a.Face)
	}))
	register("pile.collect_all", onTable(false, func(c *actx, t *table.Table) error {
		var a struct {
			To string `json:"to"`
		}
		if err := decode(c, &a); err != nil {
			return err
		}
		return t.CollectAll(c.p.ID, a.To, c.isOwner())
	}))
	register("pile.create", onTable(true, func(c *actx, t *table.Table) error {
		var a table.PileSpec
		if err := decode(c, &a); err != nil {
			return err
		}
		c.l.game.piles++
		return t.AddPile(c.p.ID, a, c.isOwner(), fmt.Sprintf("pile:%d", c.l.game.piles))
	}))
	register("pile.remove", onTable(true, func(c *actx, t *table.Table) error {
		var a struct {
			Container string `json:"container"`
		}
		if err := decode(c, &a); err != nil {
			return err
		}
		return t.RemovePile(c.p.ID, a.Container, c.isOwner())
	}))
	register("cut.offer", onTable(false, func(c *actx, t *table.Table) error {
		var a struct {
			Container string `json:"container"`
			To        string `json:"to"`
		}
		if err := decode(c, &a); err != nil {
			return err
		}
		return t.OfferCut(c.p.ID, a.Container, a.To)
	}))
	register("cut.answer", onTable(false, func(c *actx, t *table.Table) error {
		var a struct {
			Count int `json:"count"`
		}
		if err := decode(c, &a); err != nil {
			return err
		}
		return t.AnswerCut(c.p.ID, a.Count)
	}))
	register("cut.cancel", onTable(false, func(c *actx, t *table.Table) error {
		return t.CancelCut(c.p.ID, c.isOwner())
	}))
	register("deal.start", onTable(false, func(c *actx, t *table.Table) error {
		var a table.DealPlan
		if err := decode(c, &a); err != nil {
			return err
		}
		return t.Deal(c.p.ID, a)
	}))
	register("deal.step", onTable(false, func(c *actx, t *table.Table) error {
		var a struct {
			All bool `json:"all"`
		}
		if err := decode(c, &a); err != nil {
			return err
		}
		return t.DealStep(c.p.ID, a.All)
	}))
	register("deal.cancel", onTable(false, func(c *actx, t *table.Table) error {
		return t.CancelDeal(c.p.ID, c.isOwner())
	}))
	register("dealer.set", onTable(true, func(c *actx, t *table.Table) error {
		var a struct {
			Player string `json:"player"`
		}
		if err := decode(c, &a); err != nil {
			return err
		}
		return t.SetDealer(c.p.ID, a.Player, c.isOwner())
	}))
	register("round.next", onTable(false, func(c *actx, t *table.Table) error {
		var a struct {
			To string `json:"to"`
		}
		if err := decode(c, &a); err != nil {
			return err
		}
		if !c.isOwner() && c.p.ID != t.DealerID() {
			return errorf(CodeForbidden, "Only the dealer or the lobby owner can start the next hand.")
		}
		if err := t.CollectAll(c.p.ID, a.To, true); err != nil {
			return err
		}
		if t.DealerID() != "" {
			return t.PassDeal(c.p.ID, true)
		}
		return nil
	}))
	register("dealer.pass", onTable(true, func(c *actx, t *table.Table) error {
		return t.PassDeal(c.p.ID, c.isOwner())
	}))
	register("turn.set", onTable(true, func(c *actx, t *table.Table) error {
		var a struct {
			Player string `json:"player"`
		}
		if err := decode(c, &a); err != nil {
			return err
		}
		return t.SetTurn(c.p.ID, a.Player, c.isOwner())
	}))
	register("turn.advance", onTable(true, func(c *actx, t *table.Table) error {
		var a struct {
			Dir int `json:"dir"`
		}
		if err := decode(c, &a); err != nil {
			return err
		}
		if a.Dir == 0 {
			a.Dir = 1
		}
		return t.AdvanceTurn(c.p.ID, a.Dir, c.isOwner())
	}))
}

func actStart(c *actx) error {
	if err := c.requireOwner(); err != nil {
		return err
	}
	if c.l.State == Playing {
		return errorf(CodeInvalid, "The table is already running.")
	}
	if err := c.l.startGame(); err != nil {
		return err
	}
	c.l.State = Playing
	c.l.addLog(c.p.ID, "%s started the table: %s.", c.p.Name, c.l.game.preset.Info().Name)
	return nil
}

func actStop(c *actx) error {
	if err := c.requireOwner(); err != nil {
		return err
	}
	if c.l.State != Playing {
		return errorf(CodeInvalid, "The table isn't running.")
	}
	c.l.State = Waiting
	c.l.game = nil
	c.l.addLog(c.p.ID, "%s cleared the table and returned everyone to the lobby.", c.p.Name)
	return nil
}

// actReset deals a fresh table with the same seats and settings.
func actReset(c *actx) error {
	if err := c.requireOwner(); err != nil {
		return err
	}
	if c.l.State != Playing {
		return errorf(CodeInvalid, "The table isn't running.")
	}
	if err := c.l.startGame(); err != nil {
		return err
	}
	c.l.addLog(c.p.ID, "%s reset the table with fresh cards.", c.p.Name)
	return nil
}

// actUndo takes back the actor's own last action, if nothing has happened
// since and it showed no card to anyone. Anything else needs a corrective
// action instead: a revealed card can't be made unknown again.
func actUndo(c *actx) error {
	g := c.l.game
	if g == nil || g.undo == nil || g.undo.rev != c.l.rev {
		return errorf(CodeInvalid, "There's nothing to undo. Only the latest action can be undone, and only if it showed no cards.")
	}
	if g.undo.actor != c.p.ID {
		return errorf(CodeForbidden, "Only %s can undo their last action.", c.l.name(g.undo.actor))
	}
	prev := g.undo.prev
	for _, ct := range prev.Containers {
		ct.ModRev = c.l.rev + 1
	}
	g.table = prev
	c.l.addLog(c.p.ID, "%s undid: %s", c.p.Name, g.undo.text)
	g.undo = nil
	return nil
}
