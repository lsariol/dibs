package presets

import (
	"encoding/json"
	"fmt"

	"github.com/lsariol/dibs/internal/cards"
	"github.com/lsariol/dibs/internal/table"
)

// blackjack: the dealer is a seated player who runs the shoe and the
// dealer's hand; everyone else has a face-up spot. Hits, stands, the hole
// card and collecting are buttons; no bets, payouts or house rules are
// automated.
type blackjack struct{}

func init() { register(blackjack{}) }

// Blackjack container IDs.
const (
	bjShoe    = "shoe"
	bjHouse   = "house"
	bjDiscard = "discard"
)

func (blackjack) Info() Info {
	return Info{ID: "blackjack", Name: "Blackjack", MinPlayers: 2, MaxPlayers: 8, ScoreMode: "solo"}
}

func (b blackjack) Setup(t *table.Table, o Options) error {
	if err := checkSeats(b, o); err != nil {
		return err
	}
	deck, err := cards.Build(cards.Standard52, max(o.DeckCount, 1), cards.Composition{})
	if err != nil {
		return err
	}
	shoe := &table.Container{ID: bjShoe, Kind: table.Stack, Label: "Shoe", Take: table.Dealer, Place: table.Dealer, Manage: table.Dealer, Peek: table.Nobody}
	if err := t.AddContainer(shoe, 0); err != nil {
		return err
	}
	if err := t.AddCards(bjShoe, deck); err != nil {
		return err
	}
	house := &table.Container{ID: bjHouse, Kind: table.Stack, Label: "Dealer's hand", FaceUp: true, Spread: true, Take: table.Dealer, Place: table.Dealer, Manage: table.Dealer, Peek: table.Dealer}
	if err := t.AddContainer(house, 0); err != nil {
		return err
	}
	discard := &table.Container{ID: bjDiscard, Kind: table.Stack, Label: "Discard", Take: table.Dealer, Place: table.Anyone, Manage: table.Dealer, Peek: table.Nobody}
	if err := t.AddContainer(discard, 0); err != nil {
		return err
	}
	for _, p := range o.Seats {
		spot := &table.Container{ID: SpotID(p), Kind: table.Stack, Label: "Spot", Owner: p, FaceUp: true, Spread: true}
		if err := t.AddContainer(spot, 0); err != nil {
			return err
		}
	}
	t.Dealer = 0
	return nil
}

// bjTotal counts a blackjack hand from its face-up cards, aces as 11 when
// that doesn't bust. hidden counts face-down cards left out.
func bjTotal(t *table.Table, c *table.Container) (total, hidden int) {
	aces := 0
	for _, id := range c.Cards {
		cs := t.Cards[id]
		if !cs.FaceUp {
			hidden++
			continue
		}
		switch cs.Def.Rank {
		case "A":
			aces++
			total += 11
		case "J", "Q", "K":
			total += 10
		default:
			n := 0
			fmt.Sscan(cs.Def.Rank, &n)
			total += n
		}
	}
	for total > 21 && aces > 0 {
		total -= 10
		aces--
	}
	return total, hidden
}

func (b blackjack) players(t *table.Table) []string {
	var out []string
	for _, p := range t.DealOrder(1) {
		if p != t.DealerID() {
			out = append(out, p)
		}
	}
	return out
}

func (b blackjack) Guide(t *table.Table, viewer string, env Env) Guide {
	dealer := t.DealerID()
	isDealer := viewer == dealer
	g := Guide{Steps: []Step{}}
	house := t.Container(bjHouse)
	shoe := t.Container(bjShoe)
	total, hidden := bjTotal(t, house)
	g.Status = fmt.Sprintf("%s deals. Shoe: %d cards.", t.Name(dealer), shoe.Count())
	if house.Count() > 0 {
		g.Status += fmt.Sprintf(" Dealer shows %d", total)
		if hidden > 0 {
			g.Status += " plus a hole card"
		}
		g.Status += "."
	}
	if isDealer {
		if house.Count() == 0 {
			if shoe.Count() == len(t.Cards) && t.CutOffer == nil {
				g.Steps = append(g.Steps, standardDeckSteps(t, viewer, bjShoe)...)
			}
			g.Steps = append(g.Steps, primary(step("Deal the round", "deal", nil)))
		} else {
			if hidden > 0 {
				g.Steps = append(g.Steps, tableStep("Peek at the hole card", "card.peek", map[string]any{"ref": map[string]any{"container": bjHouse, "index": 1}}))
				g.Steps = append(g.Steps, primary(step("Turn over the hole card", "hole", nil)))
			}
			g.Steps = append(g.Steps, step("Dealer draws a card", "hit", map[string]any{"player": ""}))
			for _, p := range b.players(t) {
				if t.Markers["stand:"+p] == "" {
					g.Steps = append(g.Steps, step("Hit "+t.Name(p), "hit", map[string]any{"player": p}))
				}
			}
			g.Steps = append(g.Steps, confirm(step("Collect the cards", "collect", nil), "Move every card on the table to the discard pile?"))
		}
		if t.Container(bjDiscard).Count() > 0 && house.Count() == 0 {
			g.Steps = append(g.Steps, step("Shuffle the discards back into the shoe", "reshoe", nil))
		}
	} else if spot := t.Container(SpotID(viewer)); spot != nil && spot.Count() > 0 {
		mine, _ := bjTotal(t, spot)
		if t.Markers["stand:"+viewer] != "" {
			g.Hint = fmt.Sprintf("You stand on %d.", mine)
		} else {
			g.Hint = fmt.Sprintf("You have %d.", mine)
			if mine < 21 {
				g.Steps = append(g.Steps, primary(step("Hit me", "hit", map[string]any{"player": viewer})))
			}
			g.Steps = append(g.Steps, step("Stand", "stand", nil))
		}
	}
	return g
}

func (b blackjack) Do(t *table.Table, actor, name string, args json.RawMessage, env Env) error {
	if !t.IsSeated(actor) {
		return refuse("forbidden", "Only seated players can do that.")
	}
	dealer := t.DealerID()
	isDealer := actor == dealer
	switch name {
	case "deal":
		if !isDealer {
			return refuse("forbidden", "The dealer deals.")
		}
		if t.Container(bjHouse).Count() > 0 {
			return refuse("invalid", "Collect the last round first.")
		}
		var to []string
		for _, p := range b.players(t) {
			to = append(to, SpotID(p))
		}
		need := 2*len(to) + 2
		if t.Container(bjShoe).Count() < need {
			return refuse("invalid", "The shoe needs %d cards; shuffle the discards back in.", need)
		}
		for k := range t.Markers {
			delete(t.Markers, k)
		}
		// One card each face up and one to the dealer, then the second round with the dealer's face down.
		for round := range 2 {
			if err := t.Deal(actor, table.DealPlan{From: bjShoe, To: to, Passes: [][]int{{1}}, Face: "up"}); err != nil {
				return err
			}
			face := table.Face("up")
			if round == 1 {
				face = "down"
			}
			if err := t.Transfer(actor, bjShoe, bjHouse, 1, face); err != nil {
				return err
			}
		}
		return nil
	case "hit":
		var a struct {
			Player string `json:"player"`
		}
		if err := decodeArgs(args, &a); err != nil {
			return err
		}
		if a.Player == "" {
			if !isDealer {
				return refuse("forbidden", "Only the dealer draws for the dealer's hand.")
			}
			return t.Transfer(actor, bjShoe, bjHouse, 1, "up")
		}
		if !isDealer && a.Player != actor {
			return refuse("forbidden", "You can only ask for a card for yourself.")
		}
		if a.Player == dealer || !t.IsSeated(a.Player) {
			return refuse("invalid", "Choose a player's spot.")
		}
		if t.Markers["stand:"+a.Player] != "" {
			return refuse("invalid", "%s stands.", t.Name(a.Player))
		}
		if t.Container(bjHouse).Count() == 0 {
			return refuse("invalid", "Wait for the deal.")
		}
		return t.Transfer(actor, bjShoe, SpotID(a.Player), 1, "up")
	case "stand":
		if isDealer {
			return refuse("invalid", "The dealer doesn't stand; reveal the hole card and draw instead.")
		}
		if t.Container(SpotID(actor)).Count() == 0 {
			return refuse("invalid", "You have no cards yet.")
		}
		t.Markers["stand:"+actor] = "1"
		total, _ := bjTotal(t, t.Container(SpotID(actor)))
		t.Logf(actor, "%s stands on %d.", t.Name(actor), total)
		return nil
	case "hole":
		if !isDealer {
			return refuse("forbidden", "The dealer turns over the hole card.")
		}
		house := t.Container(bjHouse)
		for i, id := range house.Cards {
			if !t.Cards[id].FaceUp {
				return t.Flip(actor, table.Ref{Container: bjHouse, Index: i}, t.Rev)
			}
		}
		return refuse("invalid", "There's no hole card.")
	case "collect":
		if !isDealer && !env.IsOwner(actor) {
			return refuse("forbidden", "The dealer collects the cards.")
		}
		for _, c := range t.Containers {
			if c.ID == bjShoe || c.ID == bjDiscard || c.Count() == 0 {
				continue
			}
			if err := t.Transfer(actor, c.ID, bjDiscard, c.Count(), "down"); err != nil {
				return err
			}
		}
		for k := range t.Markers {
			delete(t.Markers, k)
		}
		return nil
	case "reshoe":
		if !isDealer {
			return refuse("forbidden", "The dealer refills the shoe.")
		}
		if t.Container(bjHouse).Count() > 0 {
			return refuse("invalid", "Collect the round first.")
		}
		if err := t.Collect(actor, bjDiscard, bjShoe, "down"); err != nil {
			return err
		}
		return t.Shuffle(actor, bjShoe)
	}
	return refuse("bad_request", "Unknown Blackjack step %q.", name)
}
