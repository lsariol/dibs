package lobby

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
)

// Request is one action a client asks for.
type Request struct {
	ID     string          `json:"id"`     // client-chosen, unique per player; a repeat returns the first answer
	Rev    uint64          `json:"rev"`    // the revision the client last saw
	Action string          `json:"action"` // e.g. "lobby.seat", "card.move"
	Params json.RawMessage `json:"params"`
}

// Response answers one Request.
type Response struct {
	Type      string `json:"type"` // always "result"
	ID        string `json:"id"`
	OK        bool   `json:"ok"`
	Rev       uint64 `json:"rev"`
	Duplicate bool   `json:"duplicate,omitempty"`
	Error     *Error `json:"error,omitempty"`
}

// actx is what a handler works with; the lobby's lock is held.
type actx struct {
	l   *Lobby
	p   *Player
	req Request
}

type handler func(c *actx) error

var handlers = map[string]handler{}

func register(name string, h handler) { handlers[name] = h }

// Do authorizes, validates and applies one action atomically, then sends
// every connection its new snapshot. Failed actions change nothing.
func (l *Lobby) Do(playerID string, req Request) Response {
	l.mu.Lock()
	defer l.mu.Unlock()
	resp := Response{Type: "result", ID: req.ID}
	fail := func(e *Error) Response {
		resp.Error = e
		resp.Rev = l.rev
		return resp
	}

	p := l.player(playerID)
	if p == nil || l.State == Ended {
		return fail(errorf(CodeForbidden, "You're no longer in this lobby."))
	}
	if req.ID == "" || len(req.ID) > 64 {
		return fail(errorf(CodeBadRequest, "Every action needs an id of 1 to 64 characters."))
	}
	for _, c := range p.recent {
		if c.id == req.ID {
			r := c.resp
			r.Duplicate = true
			return r
		}
	}
	h, ok := handlers[req.Action]
	if !ok {
		return fail(errorf(CodeBadRequest, "Unknown action %q.", req.Action))
	}

	logLen := len(l.log)
	if err := h(&actx{l: l, p: p, req: req}); err != nil {
		l.log = l.log[:logLen]
		resp = fail(asError(err))
	} else {
		l.rev++
		l.touch()
		resp.OK = true
		resp.Rev = l.rev
		l.broadcast()
	}
	p.recent = append(p.recent, cachedResponse{id: req.ID, resp: resp})
	if len(p.recent) > dedupKeep {
		p.recent = slices.Clone(p.recent[len(p.recent)-dedupKeep:])
	}
	return resp
}

// asError turns any error into one a player can be shown, hiding internals.
func asError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	var c interface{ ErrorCode() string }
	if errors.As(err, &c) {
		return &Error{Code: c.ErrorCode(), Message: err.Error()}
	}
	return &Error{Code: CodeInvalid, Message: "That didn't work."}
}

// decode reads params strictly: unknown fields are an error, so typos don't pass silently.
func decode(c *actx, v any) error {
	if len(c.req.Params) == 0 {
		return nil
	}
	d := json.NewDecoder(bytes.NewReader(c.req.Params))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return errorf(CodeBadRequest, "The parameters for %s aren't valid.", c.req.Action)
	}
	return nil
}

func (c *actx) requireOwner() error {
	if c.l.OwnerID != c.p.ID {
		return errorf(CodeForbidden, "Only the lobby owner can do that.")
	}
	return nil
}

func (c *actx) requireWaiting() error {
	if c.l.State != Waiting {
		return errorf(CodeInvalid, "That can only be changed before the table starts. The owner can return the table to the lobby first.")
	}
	return nil
}

// target returns the active player named by id, or the actor when id is "".
func (c *actx) target(id string) (*Player, error) {
	if id == "" {
		return c.p, nil
	}
	t := c.l.player(id)
	if t == nil {
		return nil, errorf(CodeNotFound, "That player isn't in the lobby.")
	}
	return t, nil
}

func init() {
	register("lobby.seat", actSeat)
	register("lobby.team", actTeam)
	register("lobby.ready", actReady)
	register("lobby.rename", actRename)
	register("lobby.kick", actKick)
	register("lobby.leave", actLeave)
	register("lobby.owner", actOwner)
	register("lobby.settings", actSettings)
	register("lobby.end", actEnd)
	register("state.sync", func(c *actx) error { return nil })
}

// actSeat seats a player (-1 stands them up). Players take free seats; the
// owner may move anyone, swapping with whoever is in the seat.
func actSeat(c *actx) error {
	var a struct {
		Player string `json:"player"`
		Seat   int    `json:"seat"`
	}
	if err := decode(c, &a); err != nil {
		return err
	}
	if err := c.requireWaiting(); err != nil {
		return err
	}
	t, err := c.target(a.Player)
	if err != nil {
		return err
	}
	isOwner := c.l.OwnerID == c.p.ID
	if t != c.p && !isOwner {
		return errorf(CodeForbidden, "Only the lobby owner can move other players.")
	}
	if a.Seat < -1 || a.Seat >= c.l.Settings.MaxPlayers {
		return errorf(CodeInvalid, "There's no seat %d.", a.Seat+1)
	}
	if a.Seat == t.Seat {
		return nil
	}
	if a.Seat >= 0 {
		if h := c.l.seatHolder(a.Seat); h != nil {
			if !isOwner {
				return errorf(CodeInvalid, "Seat %d is taken.", a.Seat+1)
			}
			h.Seat = t.Seat
		}
	}
	t.Seat = a.Seat
	if a.Seat < 0 {
		c.l.addLog(c.p.ID, "%s stood up.", t.Name)
	} else {
		c.l.addLog(c.p.ID, "%s sat in seat %d.", t.Name, a.Seat+1)
	}
	return nil
}

func actTeam(c *actx) error {
	var a struct {
		Player string `json:"player"`
		Team   string `json:"team"`
	}
	if err := decode(c, &a); err != nil {
		return err
	}
	if err := c.requireOwner(); err != nil {
		return err
	}
	if err := c.requireWaiting(); err != nil {
		return err
	}
	t, err := c.target(a.Player)
	if err != nil {
		return err
	}
	if a.Team != "" && !slices.Contains(Teams, a.Team) {
		return errorf(CodeInvalid, "Teams are A, B, C and D.")
	}
	t.Team = a.Team
	if a.Team == "" {
		c.l.addLog(c.p.ID, "%s has no team.", t.Name)
	} else {
		c.l.addLog(c.p.ID, "%s joined team %s.", t.Name, a.Team)
	}
	return nil
}

func actReady(c *actx) error {
	var a struct {
		Ready bool `json:"ready"`
	}
	if err := decode(c, &a); err != nil {
		return err
	}
	c.p.Ready = a.Ready
	return nil
}

func actRename(c *actx) error {
	var a struct {
		Name string `json:"name"`
	}
	if err := decode(c, &a); err != nil {
		return err
	}
	name, err := CleanName(a.Name)
	if err != nil {
		return err
	}
	for _, o := range c.l.active() {
		if o != c.p && equalFold(o.Name, name) {
			return errorf(CodeInvalid, "Someone here is already called %q.", name)
		}
	}
	c.l.addLog(c.p.ID, "%s is now called %s.", c.p.Name, name)
	c.p.Name = name
	return nil
}

func actKick(c *actx) error {
	var a struct {
		Player string `json:"player"`
	}
	if err := decode(c, &a); err != nil {
		return err
	}
	if err := c.requireOwner(); err != nil {
		return err
	}
	t := c.l.player(a.Player)
	if t == nil {
		return errorf(CodeNotFound, "That player isn't in the lobby.")
	}
	if t == c.p {
		return errorf(CodeInvalid, "You can't remove yourself; use Leave instead.")
	}
	c.l.addLog(c.p.ID, "%s removed %s.", c.p.Name, t.Name)
	c.l.removePlayer(t, "The lobby owner removed you from this lobby.")
	return nil
}

func actLeave(c *actx) error {
	c.l.addLog(c.p.ID, "%s left.", c.p.Name)
	c.l.removePlayer(c.p, "You left the lobby.")
	if len(c.l.active()) == 0 {
		c.l.end("Everyone left.")
	}
	return nil
}

func actOwner(c *actx) error {
	var a struct {
		Player string `json:"player"`
	}
	if err := decode(c, &a); err != nil {
		return err
	}
	if err := c.requireOwner(); err != nil {
		return err
	}
	t := c.l.player(a.Player)
	if t == nil {
		return errorf(CodeNotFound, "That player isn't in the lobby.")
	}
	c.l.OwnerID = t.ID
	c.l.addLog(c.p.ID, "%s made %s the lobby owner.", c.p.Name, t.Name)
	return nil
}

func actSettings(c *actx) error {
	var s Settings
	if err := decode(c, &s); err != nil {
		return err
	}
	if err := c.requireOwner(); err != nil {
		return err
	}
	if err := c.requireWaiting(); err != nil {
		return err
	}
	if err := s.validate(); err != nil {
		return err
	}
	if err := validatePreset(&s); err != nil {
		return err
	}
	if n := len(c.l.active()); s.MaxPlayers < n {
		return errorf(CodeInvalid, "%d players are already here; the maximum can't be lower.", n)
	}
	for _, p := range c.l.active() {
		if p.Seat >= s.MaxPlayers {
			p.Seat = -1
		}
	}
	c.l.Settings = s
	c.l.addLog(c.p.ID, "%s changed the lobby settings.", c.p.Name)
	return nil
}

func actEnd(c *actx) error {
	if err := c.requireOwner(); err != nil {
		return err
	}
	c.l.addLog(c.p.ID, "%s ended the lobby.", c.p.Name)
	c.l.end("The lobby owner ended this lobby.")
	return nil
}

func equalFold(a, b string) bool { return bytes.EqualFold([]byte(a), []byte(b)) }
