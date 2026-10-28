package presets

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lsariol/dibs/internal/cards"
	"github.com/lsariol/dibs/internal/table"
)

// euchre: four players in opposite partnerships, a 24-card deck, a two-pass
// deal from the dealer's left, the upcard on the kitty, two rounds of
// bidding, five tricks, and the usual scoring to 10. Every step is a
// button the players choose to press; none is forced, and the generic
// table actions stay available for anything the guide doesn't cover.
type euchre struct{}

func init() { register(euchre{}) }

// Euchre container IDs and markers.
const (
	euDeck  = "deck"
	euTrick = "trick"
)

func euWon(team string) string { return "won:" + team }

func (euchre) Info() Info {
	return Info{ID: "euchre", Name: "Euchre", MinPlayers: 4, MaxPlayers: 4, Teams: true, ScoreMode: "team", TargetScore: 10}
}

func (e euchre) Setup(t *table.Table, o Options) error {
	if err := checkSeats(e, o); err != nil {
		return err
	}
	for i, p := range o.Seats {
		q := o.Seats[(i+2)%4]
		if o.Teams[p] == "" || o.Teams[p] != o.Teams[q] || o.Teams[p] == o.Teams[o.Seats[(i+1)%4]] {
			return fmt.Errorf("euchre partners sit opposite each other on two teams")
		}
	}
	deck, err := cards.Build(cards.Euchre24, 1, cards.Composition{})
	if err != nil {
		return err
	}
	if err := addDeck(t, euDeck, "Deck", deck); err != nil {
		return err
	}
	if err := addPile(t, euTrick, "Trick", true, true); err != nil {
		return err
	}
	for _, team := range []string{o.Teams[o.Seats[0]], o.Teams[o.Seats[1]]} {
		c := &table.Container{ID: euWon(team), Kind: table.Stack, Label: "Team " + team + "'s tricks", Team: team, Peek: table.Nobody}
		if err := t.AddContainer(c, 0); err != nil {
			return err
		}
	}
	if err := addHands(t, o); err != nil {
		return err
	}
	for p, team := range o.Teams {
		t.Teams[p] = team
	}
	t.Dealer = 0
	return nil
}

// euState reads the hand's progress from the table.
type euState struct {
	deck      *table.Container
	upcard    *cards.Def // the face-up top of the kitty, if any
	trump     string
	maker     string // team
	alone     string // player going alone
	round     string // "1" or "2" while bidding
	tricks    map[string]int
	dealt     bool
	trickSize int
}

func (euchre) state(t *table.Table) euState {
	s := euState{deck: t.Container(euDeck), trump: t.Markers["trump"], maker: t.Markers["maker"], alone: t.Markers["alone"], round: t.Markers["round"], tricks: map[string]int{}}
	s.dealt = handsDealt(t)
	if top := s.deck.Cards; len(top) > 0 {
		if cs := t.Cards[top[len(top)-1]]; cs.FaceUp {
			d := cs.Def
			s.upcard = &d
		}
	}
	for _, team := range teamsOf(t) {
		s.tricks[team] = t.Counters["tricks:"+team]
	}
	s.trickSize = t.Container(euTrick).Count()
	return s
}

// teamsOf lists the two teams in seat order (the first seat's team first).
func teamsOf(t *table.Table) []string {
	var out []string
	for _, p := range t.Seats {
		if tm := t.Teams[p]; tm != "" && !contains(out, tm) {
			out = append(out, tm)
		}
	}
	return out
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

var suitNames = map[string]string{"S": "Spades", "H": "Hearts", "D": "Diamonds", "C": "Clubs"}

func (e euchre) Guide(t *table.Table, viewer string, env Env) Guide {
	s := e.state(t)
	dealer := t.DealerID()
	isDealer := viewer == dealer
	seated := t.IsSeated(viewer)
	g := Guide{Steps: []Step{}}
	tricksDone := s.tricks[teamsOf(t)[0]] + s.tricks[teamsOf(t)[1]]

	switch {
	case !s.dealt && s.deck.Count() == 24:
		g.Status = fmt.Sprintf("%s deals. Shuffle, offer the cut to %s, then deal.", t.Name(dealer), t.Name(t.Right(dealer)))
		g.Steps = append(g.Steps, standardDeckSteps(t, viewer, euDeck)...)
		if isDealer && t.CutOffer == nil && t.Dealing == nil {
			g.Steps = append(g.Steps,
				primary(step("Deal 3-2, then 2-3", "deal", map[string]any{"pattern": "3-2"})),
				step("Deal 2-3, then 3-2", "deal", map[string]any{"pattern": "2-3"}),
				step("Deal 2s, then 3s", "deal", map[string]any{"pattern": "2s"}),
				step("Deal 3s, then 2s", "deal", map[string]any{"pattern": "3s"}),
				step("Deal packet by packet (3-2)", "deal", map[string]any{"pattern": "3-2", "step": true}))
		}
	case t.Dealing != nil:
		g.Status = fmt.Sprintf("%s is dealing.", t.Name(t.Dealing.By))
	case s.dealt && s.trump == "" && s.round == "" && s.upcard == nil:
		g.Status = fmt.Sprintf("%s turns up the top card of the kitty.", t.Name(dealer))
		if isDealer {
			g.Steps = append(g.Steps, primary(tableStep("Turn up the top card", "card.flip", map[string]any{"ref": map[string]any{"container": euDeck, "index": -1}})))
		}
	case s.trump == "" && s.upcard != nil:
		g.Status = fmt.Sprintf("Round 1: the upcard is %s. Starting left of %s, each player passes or orders it up.", s.upcard.Label(), t.Name(dealer))
		if seated {
			label := "Order it up"
			if isDealer {
				label = "Pick it up"
			}
			g.Steps = append(g.Steps,
				step("Pass", "pass", nil),
				step(label, "order", map[string]any{"alone": false}),
				step(label+", going alone", "order", map[string]any{"alone": true}))
		}
		if isDealer {
			g.Steps = append(g.Steps, step("Everyone passed: turn it down", "turn_down", nil))
		}
	case s.trump == "" && s.round == "2":
		g.Status = "Round 2: name any suit but the turned-down one, or pass. The dealer may have to choose if everyone passes (stick the dealer is a house rule)."
		if seated {
			turned := t.Markers["turned"]
			for _, su := range cards.Suits {
				if su != turned {
					g.Steps = append(g.Steps, step("Call "+suitNames[su], "call", map[string]any{"suit": su, "alone": false}))
				}
			}
			for _, su := range cards.Suits {
				if su != turned {
					g.Steps = append(g.Steps, step("Call "+suitNames[su]+" alone", "call", map[string]any{"suit": su, "alone": true}))
				}
			}
			g.Steps = append(g.Steps, step("Pass", "pass", nil))
		}
		if isDealer {
			g.Steps = append(g.Steps, confirm(step("Everyone passed: throw in the hand", "misdeal", nil), "Gather the cards and pass the deal without scoring?"))
		}
	case s.trump != "" && t.Markers["pickup"] == "1":
		g.Status = fmt.Sprintf("%s picks up the upcard, then discards one card face down onto the kitty.", t.Name(dealer))
		if isDealer {
			g.Steps = append(g.Steps, primary(step("Pick up the upcard", "pickup", nil)))
		}
	case s.trump != "" && tricksDone < 5 && (handsDealt(t) || s.trickSize > 0):
		g.Status = fmt.Sprintf("Trump is %s, made by team %s%s. Tricks: %s. Play cards onto the trick, then say who won it.",
			suitNames[s.trump], s.maker, aloneText(t, s), trickText(t, s))
		if dh := t.Container(HandID(dealer)); isDealer && dh != nil && dh.Count() > 5 {
			g.Hint = "Discard: tap a card in your hand and put it on the Deck, face down."
		}
		if s.trickSize > 0 && seated {
			for _, p := range t.Seats {
				g.Steps = append(g.Steps, step("Trick won by "+t.Name(p), "take_trick", map[string]any{"player": p}))
			}
		}
	case s.trump != "":
		pts, team, why := e.result(t, s)
		g.Status = fmt.Sprintf("Hand over. Team %s made %s: %s. %s.", s.maker, suitNames[s.trump], trickText(t, s), why)
		if team != "" && env.CanScore(viewer, "team:"+team) && t.Markers["scored"] == "" {
			g.Steps = append(g.Steps, primary(step(fmt.Sprintf("Score it: team %s +%d", team, pts), "score", nil)))
		}
		if isDealer || env.IsOwner(viewer) {
			g.Steps = append(g.Steps, step("Next hand (the deal passes left)", "next", nil))
		}
	default:
		g.Status = "Deal when ready. Use Next hand to gather the cards."
		if isDealer || env.IsOwner(viewer) {
			g.Steps = append(g.Steps, step("Next hand (the deal passes left)", "next", nil))
		}
	}
	return g
}

func aloneText(t *table.Table, s euState) string {
	if s.alone == "" {
		return ""
	}
	return fmt.Sprintf(", %s alone (partner sits out)", t.Name(s.alone))
}

func trickText(t *table.Table, s euState) string {
	var parts []string
	for _, team := range teamsOf(t) {
		parts = append(parts, fmt.Sprintf("team %s %d", team, s.tricks[team]))
	}
	return strings.Join(parts, ", ")
}

// result applies Euchre scoring: makers with 3-4 tricks get 1, all 5 get 2
// (4 if alone); otherwise the defenders get 2 for the euchre.
func (euchre) result(t *table.Table, s euState) (int, string, string) {
	m := s.tricks[s.maker]
	switch {
	case m == 5 && s.alone != "":
		return 4, s.maker, "A lone march: 4 points"
	case m == 5:
		return 2, s.maker, "A march: 2 points"
	case m >= 3:
		return 1, s.maker, fmt.Sprintf("Makers took %d: 1 point", m)
	}
	for _, team := range teamsOf(t) {
		if team != s.maker {
			return 2, team, fmt.Sprintf("Euchred! The makers took only %d: 2 points to the defenders", m)
		}
	}
	return 0, "", ""
}

func (e euchre) Do(t *table.Table, actor, name string, args json.RawMessage, env Env) error {
	if !t.IsSeated(actor) {
		return refuse("forbidden", "Only seated players can do that.")
	}
	s := e.state(t)
	dealer := t.DealerID()
	switch name {
	case "deal":
		var a struct {
			Pattern string `json:"pattern"`
			Step    bool   `json:"step"`
		}
		if err := decodeArgs(args, &a); err != nil {
			return err
		}
		passes, ok := map[string][][]int{
			"3-2": {{3, 2, 3, 2}, {2, 3, 2, 3}},
			"2-3": {{2, 3, 2, 3}, {3, 2, 3, 2}},
			"2s":  {{2}, {3}},
			"3s":  {{3}, {2}},
		}[a.Pattern]
		if !ok {
			return refuse("bad_request", "Choose a dealing pattern.")
		}
		if handsDealt(t) {
			return refuse("invalid", "The cards are already dealt. Use Next hand first.")
		}
		return t.Deal(actor, dealPlanToHands(t, euDeck, passes, "down", a.Step))
	case "pass":
		t.Markers["passes"] = fmt.Sprint(atoi(t.Markers["passes"]) + 1)
		t.Logf(actor, "%s passed.", t.Name(actor))
		return nil
	case "order":
		var a struct {
			Alone bool `json:"alone"`
		}
		if err := decodeArgs(args, &a); err != nil {
			return err
		}
		if s.upcard == nil || s.trump != "" {
			return refuse("invalid", "There's no upcard to order up.")
		}
		e.makeTrump(t, actor, s.upcard.Suit, a.Alone)
		t.Markers["pickup"] = "1"
		if actor == dealer {
			t.Logf(actor, "%s picked it up: %s is trump%s.", t.Name(actor), suitNames[s.upcard.Suit], aloneLog(a.Alone))
		} else {
			t.Logf(actor, "%s ordered it up: %s is trump%s. %s picks it up.", t.Name(actor), suitNames[s.upcard.Suit], aloneLog(a.Alone), t.Name(dealer))
		}
		return nil
	case "pickup":
		if actor != dealer {
			return refuse("forbidden", "The dealer picks up the upcard.")
		}
		if t.Markers["pickup"] != "1" || s.upcard == nil {
			return refuse("invalid", "There's no upcard to pick up.")
		}
		if err := t.Move(actor, table.Ref{Container: euDeck, Index: -1}, HandID(actor), -1, "down", "", t.Rev); err != nil {
			return err
		}
		delete(t.Markers, "pickup")
		return nil
	case "turn_down":
		if actor != dealer {
			return refuse("forbidden", "The dealer turns the upcard down.")
		}
		if s.upcard == nil || s.trump != "" {
			return refuse("invalid", "There's no upcard to turn down.")
		}
		if err := t.Flip(actor, table.Ref{Container: euDeck, Index: -1}, t.Rev); err != nil {
			return err
		}
		t.Markers["turned"] = s.upcard.Suit
		t.Markers["round"] = "2"
		return nil
	case "call":
		var a struct {
			Suit  string `json:"suit"`
			Alone bool   `json:"alone"`
		}
		if err := decodeArgs(args, &a); err != nil {
			return err
		}
		if s.round != "2" || s.trump != "" {
			return refuse("invalid", "Suits are named in the second round.")
		}
		if _, ok := suitNames[a.Suit]; !ok || a.Suit == t.Markers["turned"] {
			return refuse("invalid", "Name a suit other than the one turned down.")
		}
		e.makeTrump(t, actor, a.Suit, a.Alone)
		t.Logf(actor, "%s called %s%s.", t.Name(actor), suitNames[a.Suit], aloneLog(a.Alone))
		return nil
	case "take_trick":
		var a struct {
			Player string `json:"player"`
		}
		if err := decodeArgs(args, &a); err != nil {
			return err
		}
		team := t.Teams[a.Player]
		if team == "" {
			return refuse("invalid", "Choose the player who won the trick.")
		}
		if err := t.Collect(actor, euTrick, euWon(team), "down"); err != nil {
			return err
		}
		t.Counters["tricks:"+team]++
		t.Logf(actor, "%s won the trick for team %s (%d).", t.Name(a.Player), team, t.Counters["tricks:"+team])
		return nil
	case "score":
		if s.trump == "" || t.Markers["scored"] != "" {
			return refuse("invalid", "There's no finished hand to score.")
		}
		pts, team, why := e.result(t, s)
		if team == "" {
			return refuse("invalid", "There's nothing to score.")
		}
		t.Markers["scored"] = "1"
		return env.AddScore("team:"+team, pts, strings.ToLower(why[:1])+why[1:])
	case "misdeal", "next":
		if actor != dealer && !env.IsOwner(actor) {
			return refuse("forbidden", "The dealer or the lobby owner starts the next hand.")
		}
		if err := t.CollectAll(actor, euDeck, true); err != nil {
			return err
		}
		for _, k := range []string{"trump", "maker", "alone", "round", "turned", "pickup", "scored", "passes"} {
			delete(t.Markers, k)
		}
		for _, team := range teamsOf(t) {
			delete(t.Counters, "tricks:"+team)
		}
		return t.PassDeal(actor, true)
	}
	return refuse("bad_request", "Unknown Euchre step %q.", name)
}

func (euchre) makeTrump(t *table.Table, actor, suit string, alone bool) {
	t.Markers["trump"] = suit
	t.Markers["maker"] = t.Teams[actor]
	if alone {
		t.Markers["alone"] = actor
	}
	delete(t.Markers, "round")
}

func aloneLog(alone bool) string {
	if alone {
		return ", going alone"
	}
	return ""
}

func atoi(s string) int {
	n := 0
	fmt.Sscan(s, &n)
	return n
}
