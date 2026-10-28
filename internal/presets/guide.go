package presets

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/lsariol/dibs/internal/table"
)

// Step is one guided button offered to a player. It names an ordinary
// action (a table action, or "preset.step" for the preset's own) so guided
// play goes through exactly the same checks as manual play.
type Step struct {
	Label   string         `json:"label"`
	Action  string         `json:"action"`
	Params  map[string]any `json:"params,omitempty"`
	Primary bool           `json:"primary,omitempty"`
	Confirm string         `json:"confirm,omitempty"` // a question to ask first
}

// Guide is what a player sees from the preset right now.
type Guide struct {
	Status string `json:"status"` // where the game is, in a sentence
	Hint   string `json:"hint,omitempty"`
	Steps  []Step `json:"steps"`
}

// Env is what a preset may ask of the lobby while running a step.
type Env interface {
	IsOwner(player string) bool
	CanScore(player, entry string) bool
	// AddScore changes a score as the acting player, with their permissions.
	AddScore(entry string, delta int, reason string) error
}

// Guided is a preset with guided steps.
type Guided interface {
	Guide(t *table.Table, viewer string, env Env) Guide
	Do(t *table.Table, actor, step string, args json.RawMessage, env Env) error
}

// StepError is a refused preset step, with a client error code.
type StepError struct {
	Code string
	Msg  string
}

func (e *StepError) Error() string     { return e.Msg }
func (e *StepError) ErrorCode() string { return e.Code }

func refuse(code, format string, a ...any) error {
	return &StepError{Code: code, Msg: fmt.Sprintf(format, a...)}
}

// step is a button for one of the preset's own steps.
func step(label, name string, args map[string]any) Step {
	p := map[string]any{"step": name}
	if args != nil {
		p["args"] = args
	}
	return Step{Label: label, Action: "preset.step", Params: p}
}

// tableStep is a button for an ordinary table action.
func tableStep(label, action string, params map[string]any) Step {
	return Step{Label: label, Action: action, Params: params}
}

func primary(s Step) Step { s.Primary = true; return s }

func confirm(s Step, q string) Step { s.Confirm = q; return s }

// decodeArgs reads a step's arguments strictly.
func decodeArgs(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return nil
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return refuse("bad_request", "That step's options aren't valid.")
	}
	return nil
}

// standardDeckSteps are the usual pre-deal buttons for the dealer: shuffle and offer the cut to their right.
func standardDeckSteps(t *table.Table, viewer, deck string) []Step {
	if viewer != t.DealerID() || t.CutOffer != nil || t.Dealing != nil {
		return nil
	}
	out := []Step{tableStep("Shuffle", "pile.shuffle", map[string]any{"container": deck})}
	if right := t.Right(viewer); right != "" && right != viewer {
		out = append(out, tableStep("Offer the cut to "+t.Name(right), "cut.offer", map[string]any{"container": deck, "to": right}))
	}
	return out
}

// handsDealt reports whether any hand holds cards.
func handsDealt(t *table.Table) bool {
	for _, p := range t.Seats {
		if c := t.Container(HandID(p)); c != nil && c.Count() > 0 {
			return true
		}
	}
	return false
}

// dealPlanToHands deals from deck to every hand in order from the dealer's left.
func dealPlanToHands(t *table.Table, deck string, passes [][]int, face table.Face, stepwise bool) table.DealPlan {
	var to []string
	for _, p := range t.DealOrder(1) {
		to = append(to, HandID(p))
	}
	return table.DealPlan{From: deck, To: to, Passes: passes, Face: face, Step: stepwise}
}
