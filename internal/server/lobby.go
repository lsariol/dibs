package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lsariol/dibs/internal/lobby"
	"github.com/lsariol/dibs/internal/presets"
)

const sessionCookie = "dibs_session"

// formData is what the create and join pages show back after an error.
type formData struct {
	Error    string
	Name     string
	Code     string
	Settings lobby.Settings
	Presets  []presetOption
	Decks    []presetOption
}

type presetOption struct {
	ID, Name string
}

// presetOptions lists the registered presets for the create form.
func presetOptions() []presetOption {
	var out []presetOption
	for _, p := range presets.All() {
		out = append(out, presetOption{p.Info().ID, p.Info().Name})
	}
	return out
}

var deckOptions = []presetOption{
	{"standard52", "Standard 52"},
	{"standard54", "Standard 54 (with jokers)"},
	{"euchre24", "Euchre 24 (9 to Ace)"},
	{"color108", "Color deck (108)"},
}

func (s *Server) handleNewForm(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusOK, "new", formData{
		Settings: lobby.Settings{Preset: presets.All()[0].Info().ID, MaxPlayers: presets.All()[0].Info().MaxPlayers, ScoreMode: presets.All()[0].Info().ScoreMode, Policy: lobby.PolicyStrict, DeckType: "standard52", DeckCount: 1},
		Presets:  presetOptions(), Decks: deckOptions,
	})
}

func (s *Server) handleNew(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad form", http.StatusBadRequest)
		return
	}
	set := lobby.Settings{
		Preset:     r.PostFormValue("preset"),
		MaxPlayers: atoi(r.PostFormValue("maxPlayers")),
		ScoreMode:  r.PostFormValue("scoreMode"),
		Policy:     r.PostFormValue("policy"),
		DeckType:   r.PostFormValue("deckType"),
		DeckCount:  atoi(r.PostFormValue("deckCount")),
		BlindDraw:  r.PostFormValue("blindDraw") == "on",
		GridRows:   atoi(r.PostFormValue("gridRows")),
		GridCols:   atoi(r.PostFormValue("gridCols")),
	}
	name := r.PostFormValue("name")
	show := func(status int, msg string) {
		s.render(w, status, "new", formData{Error: msg, Name: name, Settings: set, Presets: presetOptions(), Decks: deckOptions})
	}
	if !s.createLimit.allow(s.clientIP(r)) {
		show(http.StatusTooManyRequests, "Too many lobbies created from here. Wait a few minutes and try again.")
		return
	}
	l, _, token, err := s.lobbies.Create(name, set)
	if err != nil {
		show(http.StatusUnprocessableEntity, userMessage(err))
		return
	}
	s.log.Info("lobby created", "code", l.Code, "preset", set.Preset)
	s.setSession(w, l.Code, token)
	http.Redirect(w, r, "/l/"+l.Code, http.StatusSeeOther)
}

func (s *Server) handleJoinForm(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusOK, "join", formData{Code: lobby.NormalizeCode(r.URL.Query().Get("code"))})
}

// handleJoin joins by typed code (POST /join) or from a share link (POST /l/{code}/join).
func (s *Server) handleJoin(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad form", http.StatusBadRequest)
		return
	}
	code := r.PathValue("code")
	if code == "" {
		code = r.PostFormValue("code")
	}
	name := r.PostFormValue("name")
	show := func(status int, msg string) {
		s.render(w, status, "join", formData{Error: msg, Name: name, Code: lobby.NormalizeCode(code)})
	}
	ip := s.clientIP(r)
	if !s.joinLimit.allow(ip) || !s.missLimit.peek(ip) {
		show(http.StatusTooManyRequests, "Too many attempts. Wait a minute and try again.")
		return
	}
	l := s.lobbies.Get(code)
	if l == nil {
		s.missLimit.allow(ip)
		show(http.StatusNotFound, "No open lobby has that code. Check it with the person who made the lobby.")
		return
	}
	if id, ok := l.Authenticate(s.sessionToken(r, l.Code)); ok && id != "" {
		http.Redirect(w, r, "/l/"+l.Code, http.StatusSeeOther)
		return
	}
	_, token, err := l.Join(name)
	if err != nil {
		show(http.StatusUnprocessableEntity, userMessage(err))
		return
	}
	s.setSession(w, l.Code, token)
	http.Redirect(w, r, "/l/"+l.Code, http.StatusSeeOther)
}

// handleLobby shows the table to a player with a session, or the join form to anyone else.
func (s *Server) handleLobby(w http.ResponseWriter, r *http.Request) {
	ip := s.clientIP(r)
	code := lobby.NormalizeCode(r.PathValue("code"))
	if code != "" && code != r.PathValue("code") {
		http.Redirect(w, r, "/l/"+code, http.StatusMovedPermanently)
		return
	}
	var l *lobby.Lobby
	if s.missLimit.peek(ip) {
		l = s.lobbies.Get(code)
	}
	if l == nil {
		s.missLimit.allow(ip)
		s.render(w, http.StatusNotFound, "notfound", nil)
		return
	}
	if _, ok := l.Authenticate(s.sessionToken(r, l.Code)); ok {
		s.render(w, http.StatusOK, "table", struct{ Code string }{l.Code})
		return
	}
	s.render(w, http.StatusOK, "join", formData{Code: l.Code})
}

func (s *Server) setSession(w http.ResponseWriter, code, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/l/" + code,
		MaxAge:   int((24 * time.Hour).Seconds()),
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies(),
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) sessionToken(r *http.Request, code string) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

// userMessage is the message to show for an error from the lobby package.
func userMessage(err error) string {
	var e *lobby.Error
	if errors.As(err, &e) {
		return e.Message
	}
	return "Something went wrong. Try again."
}

func atoi(v string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(v))
	return n
}

// handleMe answers 204 while the session belongs to an open lobby, 403 when it
// doesn't, 404 when the lobby is gone; the page uses it to stop reconnecting.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	l := s.lobbies.Get(r.PathValue("code"))
	if l == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if _, ok := l.Authenticate(s.sessionToken(r, l.Code)); !ok {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
