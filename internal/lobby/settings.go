package lobby

// Score-edit modes.
const (
	ScoreSolo  = "solo"  // each player edits only their own score
	ScoreTeam  = "team"  // one appointed scorekeeper per team edits that team's score
	ScoreOwner = "owner" // only the lobby owner edits scores
)

// Interaction policies.
const (
	PolicyStrict = "strict" // shared piles follow their own rules (dealer-run decks)
	PolicyShared = "shared" // anyone may manage shared piles; private hands stay private
)

// Settings are chosen at creation; the owner can change them before the table starts.
type Settings struct {
	Preset     string `json:"preset"`
	MaxPlayers int    `json:"maxPlayers"`
	ScoreMode  string `json:"scoreMode"`
	Policy     string `json:"policy"`
	DeckType   string `json:"deckType"`  // blank table and blackjack: see cards.DeckTypes
	DeckCount  int    `json:"deckCount"` // how many of that deck, 1-8
	BlindDraw  bool   `json:"blindDraw"` // players may draw unseen from another's hand (Old Maid)
	GridRows   int    `json:"gridRows"`  // blank table: a personal grid per player, 0 for none
	GridCols   int    `json:"gridCols"`
	Custom     Custom `json:"custom"` // deckType "custom"
}

// Custom is a deck made of chosen standard ranks and suits.
type Custom struct {
	Ranks  []string `json:"ranks,omitempty"`
	Suits  []string `json:"suits,omitempty"`
	Jokers int      `json:"jokers"`
}

// validate checks settings and fills defaults; presets add their own checks.
func (s *Settings) validate() error {
	if s.Preset == "" {
		s.Preset = "blank"
	}
	if s.MaxPlayers == 0 {
		s.MaxPlayers = MaxPlayers
	}
	if s.MaxPlayers < MinPlayers || s.MaxPlayers > MaxPlayers {
		return errorf(CodeInvalid, "A lobby holds %d to %d players.", MinPlayers, MaxPlayers)
	}
	switch s.ScoreMode {
	case "":
		s.ScoreMode = ScoreOwner
	case ScoreSolo, ScoreTeam, ScoreOwner:
	default:
		return errorf(CodeInvalid, "Score mode must be solo, team or owner.")
	}
	switch s.Policy {
	case "":
		s.Policy = PolicyStrict
	case PolicyStrict, PolicyShared:
	default:
		return errorf(CodeInvalid, "Interaction policy must be strict or shared.")
	}
	if s.DeckCount == 0 {
		s.DeckCount = 1
	}
	if s.DeckCount < 1 || s.DeckCount > 8 {
		return errorf(CodeInvalid, "Use 1 to 8 decks.")
	}
	if s.GridRows < 0 || s.GridRows > 4 || s.GridCols < 0 || s.GridCols > 6 || (s.GridRows == 0) != (s.GridCols == 0) {
		return errorf(CodeInvalid, "A personal grid is up to 4 rows by 6 columns, or none.")
	}
	return nil
}
