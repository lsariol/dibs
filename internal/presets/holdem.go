package presets

import (
	"encoding/json"
	"fmt"

	"github.com/lsariol/dibs/internal/cards"
	"github.com/lsariol/dibs/internal/table"
)

// holdem: a 52-card deck, two hole cards each dealt one at a time from the
// dealer's left, a burn pile nobody can look at, and a five-slot board for
// the flop, turn and river. No betting or hand ranking: players settle that
// themselves (the scoreboard can count chips).
type holdem struct{}

func init() { register(holdem{}) }

// Hold'em container IDs.
const (
	heDeck  = "deck"
	heBoard = "board"
	heBurn  = "burn"
	heMuck  = "muck"
)

func (holdem) Info() Info {
	return Info{ID: "holdem", Name: "Texas Hold'em", MinPlayers: 2, MaxPlayers: 8, ScoreMode: "solo"}
}

func (h holdem) Setup(t *table.Table, o Options) error {
	if err := checkSeats(h, o); err != nil {
		return err
	}
	deck, err := cards.Build(cards.Standard52, 1, cards.Composition{})
	if err != nil {
		return err
	}
	if err := addDeck(t, heDeck, "Deck", deck); err != nil {
		return err
	}
	board := &table.Container{ID: heBoard, Kind: table.Grid, Label: "Board", Cols: 5, FaceUp: true, Take: table.Dealer, Place: table.Dealer, Manage: table.Dealer, Peek: table.Nobody}
	if err := t.AddContainer(board, 5); err != nil {
		return err
	}
	burn := &table.Container{ID: heBurn, Kind: table.Stack, Label: "Burn", Take: table.Nobody, Place: table.Dealer, Manage: table.Dealer, Peek: table.Nobody}
	if err := t.AddContainer(burn, 0); err != nil {
		return err
	}
	if err := addPile(t, heMuck, "Folded", false, false); err != nil {
		return err
	}
	if err := addHands(t, o); err != nil {
		return err
	}
	t.Dealer = 0
	return nil
}

// blinds returns the small and big blind players: left of the button, or
// the button itself heads-up.
func blinds(t *table.Table) (string, string) {
	d := t.DealerID()
	if d == "" {
		return "", ""
	}
	if len(t.Seats) == 2 {
		return d, t.Left(d)
	}
	return t.Left(d), t.Left(t.Left(d))
}

func (h holdem) Guide(t *table.Table, viewer string, env Env) Guide {
	dealer := t.DealerID()
	isDealer := viewer == dealer
	board := t.Container(heBoard).Count()
	sb, bb := blinds(t)
	g := Guide{Steps: []Step{}}
	pos := fmt.Sprintf("Button: %s · small blind: %s · big blind: %s.", t.Name(dealer), t.Name(sb), t.Name(bb))
	hand := t.Container(HandID(viewer))
	switch {
	case t.Dealing != nil:
		g.Status = fmt.Sprintf("%s is dealing.", t.Name(t.Dealing.By))
	case !handsDealt(t) && board == 0:
		g.Status = pos + " Shuffle, offer the cut, then deal two cards each."
		g.Steps = append(g.Steps, standardDeckSteps(t, viewer, heDeck)...)
		if isDealer && t.CutOffer == nil {
			g.Steps = append(g.Steps, primary(step("Deal two hole cards each", "deal", nil)))
		}
	case board < 5:
		g.Status = pos + " " + []string{"Pre-flop.", "", "", "Flop is out.", "Turn is out."}[board]
		if isDealer {
			switch board {
			case 0:
				g.Steps = append(g.Steps, primary(step("Burn one, deal the flop", "street", map[string]any{"cards": 3})))
			case 3:
				g.Steps = append(g.Steps, primary(step("Burn one, deal the turn", "street", map[string]any{"cards": 1})))
			case 4:
				g.Steps = append(g.Steps, primary(step("Burn one, deal the river", "street", map[string]any{"cards": 1})))
			}
		}
	default:
		g.Status = pos + " River is out: show your cards for the showdown."
	}
	if hand != nil && hand.Count() > 0 {
		up := t.Cards[hand.Cards[0]].FaceUp
		if !up {
			g.Steps = append(g.Steps, tableStep("Show my cards", "hand.reveal", map[string]any{"up": true}))
		}
		g.Steps = append(g.Steps, confirm(step("Fold", "fold", nil), "Fold your hand face down?"))
	}
	if handsDealt(t) || board > 0 {
		if isDealer || env.IsOwner(viewer) {
			g.Steps = append(g.Steps, step("Next hand (the button moves left)", "next", nil))
		}
	}
	return g
}

func (h holdem) Do(t *table.Table, actor, name string, args json.RawMessage, env Env) error {
	if !t.IsSeated(actor) {
		return refuse("forbidden", "Only seated players can do that.")
	}
	dealer := t.DealerID()
	switch name {
	case "deal":
		if handsDealt(t) {
			return refuse("invalid", "Hole cards are already dealt.")
		}
		return t.Deal(actor, dealPlanToHands(t, heDeck, [][]int{{1}, {1}}, "down", false))
	case "street":
		var a struct {
			Cards int `json:"cards"`
		}
		if err := decodeArgs(args, &a); err != nil {
			return err
		}
		if actor != dealer {
			return refuse("forbidden", "The dealer deals the board.")
		}
		board := t.Container(heBoard)
		want := map[int]int{0: 3, 3: 1, 4: 1}[board.Count()]
		if want == 0 || a.Cards != want {
			return refuse("invalid", "That isn't the next street.")
		}
		if err := t.Move(actor, table.Ref{Container: heDeck, Index: -1}, heBurn, -1, "down", "", t.Rev); err != nil {
			return err
		}
		for range a.Cards {
			if err := t.Move(actor, table.Ref{Container: heDeck, Index: -1}, heBoard, -1, "up", "", t.Rev); err != nil {
				return err
			}
		}
		return nil
	case "fold":
		if t.Container(HandID(actor)).Count() == 0 {
			return refuse("invalid", "You have no cards to fold.")
		}
		return t.Collect(actor, HandID(actor), heMuck, "down")
	case "next":
		if actor != dealer && !env.IsOwner(actor) {
			return refuse("forbidden", "The dealer or the lobby owner starts the next hand.")
		}
		if err := t.CollectAll(actor, heDeck, true); err != nil {
			return err
		}
		return t.PassDeal(actor, true)
	}
	return refuse("bad_request", "Unknown Hold'em step %q.", name)
}
