package lobby

// presetInfo is what the lobby needs to know about a preset before the table exists.
type presetInfo struct {
	Name       string
	MinPlayers int
	MaxPlayers int
}

var presetList = map[string]presetInfo{
	"blank":     {"Blank table", 2, 8},
	"euchre":    {"Euchre", 4, 4},
	"holdem":    {"Texas Hold'em", 2, 8},
	"golf":      {"Six-card Golf", 2, 8},
	"blackjack": {"Blackjack", 2, 8},
}

// validatePreset checks the preset exists and fits the player limit.
func validatePreset(s *Settings) error {
	info, ok := presetList[s.Preset]
	if !ok {
		return errorf(CodeInvalid, "Unknown game %q.", s.Preset)
	}
	if s.MaxPlayers > info.MaxPlayers {
		s.MaxPlayers = info.MaxPlayers
	}
	if s.MaxPlayers < info.MinPlayers {
		return errorf(CodeInvalid, "%s needs at least %d players.", info.Name, info.MinPlayers)
	}
	return nil
}
