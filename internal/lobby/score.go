package lobby

import (
	"github.com/lsariol/dibs/internal/presets"
	"github.com/lsariol/dibs/internal/scoring"
)

// scoreView is the scoreboard as one player sees it (scores are public).
type scoreView struct {
	Mode    scoring.Mode      `json:"mode"`
	Entries []*scoring.Entry  `json:"entries"`
	Target  int               `json:"target"`
	Keepers map[string]string `json:"keepers"`
	History []*scoring.Change `json:"history"` // the latest 50
	Leaders []string          `json:"leaders"`
	CanEdit []string          `json:"canEdit"` // entry IDs this viewer may change
}

// lineup is the seated players and their teams.
func (l *Lobby) lineup() scoring.Lineup {
	lu := scoring.Lineup{Teams: map[string]string{}}
	for _, p := range l.seated() {
		lu.Players = append(lu.Players, p.ID)
		if p.Team != "" {
			lu.Teams[p.ID] = p.Team
		}
	}
	return lu
}

// nextScore returns the scoreboard fitted to the seated players and score
// mode, keeping existing scores; the caller commits it once the table starts.
func (l *Lobby) nextScore() (*scoring.Board, error) {
	b := &scoring.Board{}
	if l.score != nil {
		b = l.score.Clone()
	} else if p := presets.Get(l.Settings.Preset); p != nil {
		b.Target = p.Info().TargetScore
	}
	if err := b.Rebuild(scoring.Mode(l.Settings.ScoreMode), l.lineup()); err != nil {
		return nil, err
	}
	return b, nil
}

func (l *Lobby) scoreView(viewer string) *scoreView {
	if l.score == nil {
		return nil
	}
	b := l.score
	v := &scoreView{Mode: b.Mode, Entries: b.Entries, Target: b.Target, Keepers: b.Keepers, History: b.History, Leaders: b.Leaders(), CanEdit: []string{}}
	if v.Leaders == nil {
		v.Leaders = []string{}
	}
	if v.History == nil {
		v.History = []*scoring.Change{}
	}
	if len(v.History) > 50 {
		v.History = v.History[len(v.History)-50:]
	}
	for _, e := range l.score.Entries {
		if l.score.CanEdit(viewer, e.ID, l.OwnerID == viewer) {
			v.CanEdit = append(v.CanEdit, e.ID)
		}
	}
	return v
}

func (c *actx) board() (*scoring.Board, error) {
	if c.l.score == nil {
		return nil, errorf(CodeInvalid, "Scores start when the table starts.")
	}
	return c.l.score, nil
}

// entryName names a scoreboard line for the history.
func (l *Lobby) entryName(id string) string {
	for _, t := range Teams {
		if id == scoring.TeamEntry(t) {
			return "team " + t
		}
	}
	return l.name(id)
}

func init() {
	register("score.add", func(c *actx) error {
		var a struct {
			Entry  string `json:"entry"`
			Delta  int    `json:"delta"`
			Reason string `json:"reason"`
		}
		if err := decode(c, &a); err != nil {
			return err
		}
		b, err := c.board()
		if err != nil {
			return err
		}
		return c.addScore(b, a.Entry, a.Delta, a.Reason)
	})
	register("score.revert", func(c *actx) error {
		var a struct {
			Change int `json:"change"`
		}
		if err := decode(c, &a); err != nil {
			return err
		}
		b, err := c.board()
		if err != nil {
			return err
		}
		if err := b.Revert(c.p.ID, a.Change, c.isOwner(), c.l.now()); err != nil {
			return err
		}
		last := b.History[len(b.History)-1]
		c.l.addLog(c.p.ID, "%s corrected a score: %+d for %s.", c.p.Name, last.Delta, c.l.entryName(last.Entry))
		return nil
	})
	register("score.reset", func(c *actx) error {
		b, err := c.board()
		if err != nil {
			return err
		}
		if err := b.Reset(c.isOwner()); err != nil {
			return err
		}
		c.l.addLog(c.p.ID, "%s reset the scores.", c.p.Name)
		return nil
	})
	register("score.keeper", func(c *actx) error {
		var a struct {
			Team   string `json:"team"`
			Player string `json:"player"`
		}
		if err := decode(c, &a); err != nil {
			return err
		}
		b, err := c.board()
		if err != nil {
			return err
		}
		if err := b.SetKeeper(a.Team, a.Player, c.isOwner(), c.l.lineup()); err != nil {
			return err
		}
		c.l.addLog(c.p.ID, "%s keeps score for team %s.", c.l.name(a.Player), a.Team)
		return nil
	})
	register("score.target", func(c *actx) error {
		var a struct {
			Target int `json:"target"`
		}
		if err := decode(c, &a); err != nil {
			return err
		}
		b, err := c.board()
		if err != nil {
			return err
		}
		if err := b.SetTarget(a.Target, c.isOwner()); err != nil {
			return err
		}
		c.l.addLog(c.p.ID, "%s set the target score to %d.", c.p.Name, a.Target)
		return nil
	})
}

// addScore is the one way scores change, for manual entries and preset
// scoring alike, so the same permissions always apply.
func (c *actx) addScore(b *scoring.Board, entry string, delta int, reason string) error {
	if err := b.Add(c.p.ID, entry, delta, reason, c.isOwner(), c.l.now()); err != nil {
		return err
	}
	why := ""
	if reason != "" {
		why = " (" + reason + ")"
	}
	c.l.addLog(c.p.ID, "%s gave %s %+d%s.", c.p.Name, c.l.entryName(entry), delta, why)
	return nil
}
