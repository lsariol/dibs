package lobby

// view is one player's snapshot. It's built per recipient: nothing in it is
// meant for anyone else.
type view struct {
	Type  string     `json:"type"` // always "state"
	Rev   uint64     `json:"rev"`
	You   string     `json:"you"`
	Lobby viewLobby  `json:"lobby"`
	Log   []LogEntry `json:"log"`
}

type viewLobby struct {
	Code     string       `json:"code"`
	URL      string       `json:"url"`
	Owner    string       `json:"owner"`
	State    State        `json:"state"`
	Settings Settings     `json:"settings"`
	Players  []viewPlayer `json:"players"`
	Teams    []string     `json:"teams"`
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
		},
	}
	for _, o := range l.active() {
		v.Lobby.Players = append(v.Lobby.Players, viewPlayer{
			ID: o.ID, Name: o.Name, Seat: o.Seat, Team: o.Team, Ready: o.Ready, Connected: o.Connected(),
		})
	}
	v.Log = l.log
	if len(v.Log) > logShown {
		v.Log = v.Log[len(v.Log)-logShown:]
	}
	return mustJSON(v)
}
