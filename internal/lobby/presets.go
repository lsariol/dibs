package lobby

import "github.com/lsariol/dibs/internal/presets"

// validatePreset checks the preset exists and fits the player limit.
func validatePreset(s *Settings) error {
	p := presets.Get(s.Preset)
	if p == nil {
		return errorf(CodeInvalid, "Unknown game %q.", s.Preset)
	}
	info := p.Info()
	if s.ScoreMode == "" {
		s.ScoreMode = info.ScoreMode
	}
	if s.MaxPlayers > info.MaxPlayers {
		s.MaxPlayers = info.MaxPlayers
	}
	if s.MaxPlayers < info.MinPlayers {
		return errorf(CodeInvalid, "%s needs at least %d players.", info.Name, info.MinPlayers)
	}
	return nil
}
