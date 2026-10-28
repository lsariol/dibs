package lobby

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/lsariol/dibs/internal/random"
)

// Expiry: a lobby nobody is connected to goes after EmptyTimeout; any lobby
// with no actions or connection changes goes after IdleTimeout.
const (
	EmptyTimeout = 2 * time.Hour
	IdleTimeout  = 6 * time.Hour
	MaxLobbies   = 2000
)

// Manager holds every lobby by code.
type Manager struct {
	mu        sync.RWMutex
	lobbies   map[string]*Lobby
	publicURL string
	now       func() time.Time
}

// NewManager makes an empty Manager; publicURL is used for share links.
func NewManager(publicURL string) *Manager {
	return &Manager{lobbies: map[string]*Lobby{}, publicURL: strings.TrimRight(publicURL, "/"), now: time.Now}
}

// Create makes a lobby owned by a new player and returns the owner's ID and session token.
func (m *Manager) Create(ownerName string, s Settings) (*Lobby, string, string, error) {
	if err := s.validate(); err != nil {
		return nil, "", "", err
	}
	if err := validatePreset(&s); err != nil {
		return nil, "", "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.lobbies) >= MaxLobbies {
		return nil, "", "", errorf(CodeFull, "Dibs is very busy right now. Try again in a few minutes.")
	}
	code := random.Code(CodeLength)
	for m.lobbies[code] != nil {
		code = random.Code(CodeLength)
	}
	now := m.now()
	l := &Lobby{
		Code:     code,
		Settings: s,
		State:    Waiting,
		sessions: map[[32]byte]string{},
		created:  now,
		activity: now,
		shareURL: m.publicURL + "/l/" + code,
		now:      m.now,
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	p, token, err := l.addPlayer(ownerName)
	if err != nil {
		return nil, "", "", err
	}
	l.OwnerID = p.ID
	l.noConnsAt = now
	l.addLog(p.ID, "%s created the lobby.", p.Name)
	m.lobbies[code] = l
	return l, p.ID, token, nil
}

// Get returns an open lobby by code (any case, spaces and dashes ignored), or nil.
func (m *Manager) Get(code string) *Lobby {
	code = NormalizeCode(code)
	if code == "" {
		return nil
	}
	m.mu.RLock()
	l := m.lobbies[code]
	m.mu.RUnlock()
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.State == Ended {
		return nil
	}
	return l
}

// NormalizeCode uppercases a typed code and drops spaces and dashes; "" if it can't be a code.
func NormalizeCode(code string) string {
	code = strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(code)))
	if len(code) != CodeLength {
		return ""
	}
	for _, r := range code {
		if !strings.ContainsRune(random.CodeAlphabet, r) {
			return ""
		}
	}
	return code
}

// Join adds a player and returns their ID and session token.
func (l *Lobby) Join(name string) (string, string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.State == Ended {
		return "", "", errorf(CodeNotFound, "This lobby has ended.")
	}
	p, token, err := l.addPlayer(name)
	if err != nil {
		return "", "", err
	}
	l.addLog(p.ID, "%s joined.", p.Name)
	l.rev++
	l.broadcast()
	return p.ID, token, nil
}

// Authenticate returns the player a session token belongs to.
func (l *Lobby) Authenticate(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	id, ok := l.sessions[hashToken(token)]
	if !ok || l.player(id) == nil {
		return "", false
	}
	return id, true
}

// ShareURL is the link players open to join; it holds no credential.
func (l *Lobby) ShareURL() string { return l.shareURL }

// Sweep removes ended and expired lobbies and returns how many went.
func (m *Manager) Sweep() int {
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	removed := 0
	for code, l := range m.lobbies {
		l.mu.Lock()
		expired := l.State == Ended ||
			now.Sub(l.activity) > IdleTimeout ||
			(!l.noConnsAt.IsZero() && now.Sub(l.noConnsAt) > EmptyTimeout)
		if expired {
			if l.State != Ended {
				l.end("This lobby expired after a long time without activity.")
			}
			delete(m.lobbies, code)
			removed++
		}
		l.mu.Unlock()
	}
	return removed
}

// Run sweeps every minute until ctx ends.
func (m *Manager) Run(ctx context.Context, onSweep func(removed int)) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n := m.Sweep(); n > 0 && onSweep != nil {
				onSweep(n)
			}
		}
	}
}

// Stats counts open lobbies and connected players.
func (m *Manager) Stats() (lobbies, connected int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, l := range m.lobbies {
		l.mu.Lock()
		lobbies++
		for _, p := range l.active() {
			if p.Connected() {
				connected++
			}
		}
		l.mu.Unlock()
	}
	return lobbies, connected
}
