package lobby

import (
	"github.com/lsariol/dibs/internal/presets"
	"github.com/lsariol/dibs/internal/table"
)

// view is one player's snapshot. It's built per recipient: nothing in it is
// meant for anyone else.
type view struct {
	Type  string      `json:"type"` // always "state"
	Rev   uint64      `json:"rev"`
	You   string      `json:"you"`
	Lobby viewLobby   `json:"lobby"`
	Log   []LogEntry  `json:"log"`
	Table *table.View `json:"table"`
	Undo  string      `json:"undo,omitempty"` // what the viewer can undo right now
}

type viewLobby struct {
	Code     string         `json:"code"`
	URL      string         `json:"url"`
	Owner    string         `json:"owner"`
	State    State          `json:"state"`
	Settings Settings       `json:"settings"`
	Players  []viewPlayer   `json:"players"`
	Teams    []string       `json:"teams"`
	Presets  []presets.Info `json:"presets"`
}

type viewPlayer struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Seat      int    `json:"seat"`
	Team      string `json:"team"`
	Ready     bool   `json:"ready"`
	Connected bool   `json:"connected"`
}

const logShown = 60

// snapshot builds p's view; the caller holds mu.
func (l *Lobby) snapshot(p *Player) []byte {
	v := view{
		Type: "state",
		Rev:  l.rev,
		You:  p.ID,
		Lobby: viewLobby{
			Code:     l.Code,
			URL:      l.shareURL,
			Owner:    l.OwnerID,
			State:    l.State,
			Settings: l.Settings,
			Teams:    Teams,
			Presets:  presetInfos,
		},
	}
	for _, o := range l.active() {
		v.Lobby.Players = append(v.Lobby.Players, viewPlayer{
			ID: o.ID, Name: o.Name, Seat: o.Seat, Team: o.Team, Ready: o.Ready, Connected: o.Connected(),
		})
	}
	if g := l.game; g != nil && l.State == Playing {
		tv := g.table.ViewFor(p.ID)
		v.Table = &tv
		if g.undo != nil && g.undo.rev == l.rev && g.undo.actor == p.ID {
			v.Undo = g.undo.text
		}
	}
	v.Log = l.log
	if len(v.Log) > logShown {
		v.Log = v.Log[len(v.Log)-logShown:]
	}
	return mustJSON(v)
}

var presetInfos = func() []presets.Info {
	var out []presets.Info
	for _, p := range presets.All() {
		out = append(out, p.Info())
	}
	return out
}()
