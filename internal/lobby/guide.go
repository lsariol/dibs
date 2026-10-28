package lobby

import (
	"encoding/json"

	"github.com/lsariol/dibs/internal/presets"
	"github.com/lsariol/dibs/internal/table"
)

// presetEnv is what presets may ask of the lobby. c is set while a step runs.
type presetEnv struct {
	l *Lobby
	c *actx
}

func (e presetEnv) IsOwner(p string) bool { return e.l.OwnerID == p }

func (e presetEnv) CanScore(p, entry string) bool {
	return e.l.score != nil && e.l.score.CanEdit(p, entry, e.l.OwnerID == p)
}

// AddScore records a preset's scoring as the acting player: the same path,
// and the same permissions, as a manual score change.
func (e presetEnv) AddScore(entry string, delta int, reason string) error {
	if e.c == nil {
		return errorf(CodeInvalid, "Scores can only change during an action.")
	}
	b, err := e.c.board()
	if err != nil {
		return err
	}
	return e.c.addScore(b, entry, delta, reason)
}

// guideFor returns the preset's guide for a player, or nil for presets without one.
func (l *Lobby) guideFor(viewer string) *presets.Guide {
	g := l.game
	if g == nil {
		return nil
	}
	gp, ok := g.preset.(presets.Guided)
	if !ok {
		return nil
	}
	guide := gp.Guide(g.table, viewer, presetEnv{l: l})
	return &guide
}

func init() {
	register("preset.step", onTable(false, func(c *actx, t *table.Table) error {
		var a struct {
			Step string          `json:"step"`
			Args json.RawMessage `json:"args"`
		}
		if err := decode(c, &a); err != nil {
			return err
		}
		gp, ok := c.l.game.preset.(presets.Guided)
		if !ok {
			return errorf(CodeBadRequest, "This game has no guided steps.")
		}
		return gp.Do(t, c.p.ID, a.Step, a.Args, presetEnv{l: c.l, c: c})
	}))
}
