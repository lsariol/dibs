package presets

import (
	"encoding/json"
	"fmt"

	"github.com/lsariol/dibs/internal/cards"
	"github.com/lsariol/dibs/internal/table"
)

// golf: six-card Golf. Each player has a 2x3 grid dealt face down, flips two,
// then on their turn draws from the deck or takes the discard and swaps it
// into the grid or throws it away. Scores are entered by hand (rules vary);
// the guide offers the common count as a suggestion.
type golf struct{}

func init() { register(golf{}) }

// Golf container IDs.
const (
	goDeck    = "deck"
	goDiscard = "discard"
)

func (golf) Info() Info {
	return Info{ID: "golf", Name: "Six-card Golf", MinPlayers: 2, MaxPlayers: 8, ScoreMode: "solo"}
}

func (g golf) Setup(t *table.Table, o Options) error {
	if err := checkSeats(g, o); err != nil {
		return err
	}
	decks := o.DeckCount
	if decks < 1 {
		decks = 1
	}
	if len(o.Seats) > 4 && decks < 2 {
		decks = 2 // six cards each plus a draw pile needs a second deck past four players
	}
	deck, err := cards.Build(cards.Standard52, decks, cards.Composition{})
	if err != nil {
		return err
	}
	if err := addDeck(t, goDeck, "Deck", deck); err != nil {
		return err
	}
	if err := addPile(t, goDiscard, "Discard", true, false); err != nil {
		return err
	}
	if err := addHands(t, o); err != nil {
		return err
	}
	for _, p := range o.Seats {
		c := &table.Container{ID: GridID(p), Kind: table.Grid, Label: "Grid", Owner: p, Cols: 3}
		if err := t.AddContainer(c, 6); err != nil {
			return err
		}
	}
	t.Dealer = 0
	return nil
}

// golfScore is the common count: A 1, 2 −2, 3-10 face value, J and Q 10,
// K 0, and a column of two equal ranks scores 0. ok is false while any
// card is face down.
func golfScore(t *table.Table, grid *table.Container) (int, bool) {
	value := map[string]int{"A": 1, "2": -2, "J": 10, "Q": 10, "K": 0}
	for n := 3; n <= 10; n++ {
		value[fmt.Sprint(n)] = n
	}
	total := 0
	for col := range 3 {
		top, bottom := grid.Cards[col], grid.Cards[col+3]
		if top == "" || bottom == "" {
			return 0, false
		}
		a, b := t.Cards[top], t.Cards[bottom]
		if !a.FaceUp || !b.FaceUp {
			return 0, false
		}
		if a.Def.Rank == b.Def.Rank {
			continue
		}
		total += value[a.Def.Rank] + value[b.Def.Rank]
	}
	return total, true
}

func (g golf) Guide(t *table.Table, viewer string, env Env) Guide {
	dealer := t.DealerID()
	isDealer := viewer == dealer
	gr := Guide{Steps: []Step{}}
	grid := t.Container(GridID(viewer))
	dealt := false
	for _, p := range t.Seats {
		if t.Container(GridID(p)).Count() > 0 {
			dealt = true
		}
	}
	switch {
	case t.Dealing != nil:
		gr.Status = fmt.Sprintf("%s is dealing.", t.Name(t.Dealing.By))
	case !dealt:
		gr.Status = fmt.Sprintf("%s deals six cards face down to each grid.", t.Name(dealer))
		gr.Steps = append(gr.Steps, standardDeckSteps(t, viewer, goDeck)...)
		if isDealer && t.CutOffer == nil {
			gr.Steps = append(gr.Steps, primary(step("Deal six cards each", "deal", nil)))
		}
	case t.Container(goDiscard).Count() == 0:
		gr.Status = "Turn up the first discard to begin."
		if isDealer {
			gr.Steps = append(gr.Steps, primary(step("Turn up the first discard", "start", nil)))
		}
	default:
		gr.Status = "Flip two of your cards to start. On your turn: draw from the deck or take the discard, then swap it into your grid or throw it on the discard."
		gr.Hint = "Tap the Deck or Discard to draw; tap the card in your hand and choose Move to… to swap it into a slot."
	}
	if grid != nil && grid.Count() > 0 {
		if pts, ok := golfScore(t, grid); ok {
			if env.CanScore(viewer, viewer) && t.Markers["scored:"+viewer] == "" {
				gr.Steps = append(gr.Steps, step(fmt.Sprintf("Record my score: %d (common rules)", pts), "score", nil))
			}
		} else {
			gr.Steps = append(gr.Steps, confirm(step("Turn all my cards up", "reveal", nil), "Turn every card in your grid face up?"))
		}
	}
	if dealt && (isDealer || env.IsOwner(viewer)) {
		gr.Steps = append(gr.Steps, step("Next hole (the deal passes left)", "next", nil))
	}
	return gr
}

func (g golf) Do(t *table.Table, actor, name string, args json.RawMessage, env Env) error {
	if !t.IsSeated(actor) {
		return refuse("forbidden", "Only seated players can do that.")
	}
	dealer := t.DealerID()
	switch name {
	case "deal":
		var to []string
		for _, p := range t.DealOrder(1) {
			to = append(to, GridID(p))
		}
		if t.Container(GridID(actor)) != nil && t.Container(GridID(t.Seats[0])).Count() > 0 {
			return refuse("invalid", "The grids are already dealt.")
		}
		return t.Deal(actor, table.DealPlan{From: goDeck, To: to, Passes: [][]int{{1}, {1}, {1}, {1}, {1}, {1}}, Face: "down"})
	case "start":
		if actor != dealer {
			return refuse("forbidden", "The dealer turns up the first discard.")
		}
		return t.Move(actor, table.Ref{Container: goDeck, Index: -1}, goDiscard, -1, "up", "", t.Rev)
	case "reveal":
		grid := t.Container(GridID(actor))
		for i, id := range grid.Cards {
			if id != "" && !t.Cards[id].FaceUp {
				if err := t.Flip(actor, table.Ref{Container: grid.ID, Index: i}, t.Rev); err != nil {
					return err
				}
			}
		}
		return nil
	case "score":
		pts, ok := golfScore(t, t.Container(GridID(actor)))
		if !ok {
			return refuse("invalid", "Turn all your cards up first.")
		}
		if t.Markers["scored:"+actor] != "" {
			return refuse("invalid", "You've already recorded this hole.")
		}
		t.Markers["scored:"+actor] = "1"
		if pts == 0 {
			t.Logf(actor, "%s scored 0 this hole.", t.Name(actor))
			return nil
		}
		return env.AddScore(actor, pts, "golf hole")
	case "next":
		if actor != dealer && !env.IsOwner(actor) {
			return refuse("forbidden", "The dealer or the lobby owner starts the next hole.")
		}
		if err := t.CollectAll(actor, goDeck, true); err != nil {
			return err
		}
		for k := range t.Markers {
			delete(t.Markers, k)
		}
		return t.PassDeal(actor, true)
	}
	return refuse("bad_request", "Unknown Golf step %q.", name)
}
