// Package scoring is a generic scoreboard: entries for players or teams, a
// target, a history of every change, and who may change what.
package scoring

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

// Mode is who may edit scores.
type Mode string

// Modes.
const (
	Solo  Mode = "solo"  // each player edits only their own entry
	Team  Mode = "team"  // each team's appointed scorekeeper edits only that team's entry
	Owner Mode = "owner" // only the lobby owner edits
)

// Entry is one line of the scoreboard.
type Entry struct {
	ID     string `json:"id"`               // a player ID, or "team:<T>"
	Team   string `json:"team,omitempty"`   // for team entries
	Player string `json:"player,omitempty"` // for player entries
	Score  int    `json:"score"`
}

// Change is one recorded score change.
type Change struct {
	ID         int       `json:"id"`
	Entry      string    `json:"entry"`
	Delta      int       `json:"delta"`
	Reason     string    `json:"reason"`
	By         string    `json:"by"`
	At         time.Time `json:"at"`
	Reverts    int       `json:"reverts,omitempty"`    // the change this one corrects
	RevertedBy int       `json:"revertedBy,omitempty"` // the change that corrected this one
}

// Board is a scoreboard.
type Board struct {
	Mode    Mode              `json:"mode"`
	Entries []*Entry          `json:"entries"`
	Target  int               `json:"target"`
	Keepers map[string]string `json:"keepers"` // team -> scorekeeper player ID
	History []*Change         `json:"history"`
	next    int
}

// Error is a refused change, with a client error code.
type Error struct {
	Code string
	Msg  string
}

func (e *Error) Error() string     { return e.Msg }
func (e *Error) ErrorCode() string { return e.Code }

func fail(code, format string, args ...any) error {
	return &Error{Code: code, Msg: fmt.Sprintf(format, args...)}
}

// TeamEntry is the entry ID of a team.
func TeamEntry(team string) string { return "team:" + team }

// Lineup is who's playing: players in seat order and their teams.
type Lineup struct {
	Players []string
	Teams   map[string]string // player -> team; empty when not on a team
}

// Rebuild sets the entries for a mode and lineup, keeping the scores of
// entries that remain. Solo scores players; Team scores teams; Owner scores
// teams when anyone is on a team, else players.
func (b *Board) Rebuild(mode Mode, l Lineup) error {
	var teams []string
	for _, p := range l.Players {
		if t := l.Teams[p]; t != "" && !slices.Contains(teams, t) {
			teams = append(teams, t)
		}
	}
	slices.Sort(teams)
	byTeam := mode == Team || (mode == Owner && len(teams) > 0)
	if mode == Team && len(teams) == 0 {
		return fail("invalid", "Scores are kept by team, but nobody is on a team. Put players on teams, or change the score mode.")
	}
	old := map[string]int{}
	for _, e := range b.Entries {
		old[e.ID] = e.Score
	}
	var entries []*Entry
	if byTeam {
		for _, t := range teams {
			entries = append(entries, &Entry{ID: TeamEntry(t), Team: t, Score: old[TeamEntry(t)]})
		}
	} else {
		for _, p := range l.Players {
			entries = append(entries, &Entry{ID: p, Player: p, Score: old[p]})
		}
	}
	b.Mode = mode
	b.Entries = entries
	b.Keepers = maps.Clone(b.Keepers)
	if b.Keepers == nil {
		b.Keepers = map[string]string{}
	}
	if mode == Team {
		for _, t := range teams {
			if k := b.Keepers[t]; k == "" || l.Teams[k] != t || !slices.Contains(l.Players, k) {
				for _, p := range l.Players {
					if l.Teams[p] == t {
						b.Keepers[t] = p
						break
					}
				}
			}
		}
	}
	return nil
}

// Clone returns a deep copy, so a change can be tried and thrown away.
func (b *Board) Clone() *Board {
	n := &Board{Mode: b.Mode, Target: b.Target, Keepers: maps.Clone(b.Keepers), next: b.next}
	for _, e := range b.Entries {
		c := *e
		n.Entries = append(n.Entries, &c)
	}
	for _, h := range b.History {
		c := *h
		n.History = append(n.History, &c)
	}
	return n
}

func (b *Board) entry(id string) *Entry {
	for _, e := range b.Entries {
		if e.ID == id {
			return e
		}
	}
	return nil
}

// CanEdit reports whether actor may change an entry.
func (b *Board) CanEdit(actor, entry string, isOwner bool) bool {
	e := b.entry(entry)
	if e == nil {
		return false
	}
	switch b.Mode {
	case Solo:
		return e.Player == actor
	case Team:
		return e.Team != "" && b.Keepers[e.Team] == actor
	case Owner:
		return isOwner
	}
	return false
}

func (b *Board) refusal(entry string) error {
	switch b.Mode {
	case Solo:
		return fail("forbidden", "In solo scoring each player changes only their own score.")
	case Team:
		if e := b.entry(entry); e != nil {
			return fail("forbidden", "Only team %s's scorekeeper can change its score.", e.Team)
		}
	}
	return fail("forbidden", "Only the lobby owner can change scores.")
}

// Add changes an entry by delta and records why.
func (b *Board) Add(actor, entry string, delta int, reason string, isOwner bool, now time.Time) error {
	if b.entry(entry) == nil {
		return fail("not_found", "That score line doesn't exist.")
	}
	if delta == 0 || delta < -1000 || delta > 1000 {
		return fail("bad_request", "Change a score by -1000 to 1000, not 0.")
	}
	reason = strings.TrimSpace(reason)
	if len([]rune(reason)) > 60 {
		return fail("bad_request", "Keep the reason to 60 characters.")
	}
	if !b.CanEdit(actor, entry, isOwner) {
		return b.refusal(entry)
	}
	b.record(&Change{Entry: entry, Delta: delta, Reason: reason, By: actor, At: now})
	return nil
}

func (b *Board) record(c *Change) {
	b.next++
	c.ID = b.next
	b.entry(c.Entry).Score += c.Delta
	b.History = append(b.History, c)
	if len(b.History) > 500 {
		b.History = slices.Clone(b.History[len(b.History)-500:])
	}
}

// Revert corrects an earlier change with an opposite one; whoever may edit that entry may.
func (b *Board) Revert(actor string, change int, isOwner bool, now time.Time) error {
	var c *Change
	for _, h := range b.History {
		if h.ID == change {
			c = h
		}
	}
	if c == nil {
		return fail("not_found", "That score change isn't in the history.")
	}
	if c.RevertedBy != 0 || c.Reverts != 0 {
		return fail("invalid", "That change was already corrected (or is itself a correction).")
	}
	if b.entry(c.Entry) == nil {
		return fail("invalid", "That score line isn't on the board any more.")
	}
	if !b.CanEdit(actor, c.Entry, isOwner) {
		return b.refusal(c.Entry)
	}
	fix := &Change{Entry: c.Entry, Delta: -c.Delta, Reason: "correction", By: actor, At: now, Reverts: c.ID}
	b.record(fix)
	c.RevertedBy = fix.ID
	return nil
}

// Reset sets every score to 0 and clears the history; only the lobby owner may.
func (b *Board) Reset(isOwner bool) error {
	if !isOwner {
		return fail("forbidden", "Only the lobby owner can reset the scores.")
	}
	for _, e := range b.Entries {
		e.Score = 0
	}
	b.History = nil
	return nil
}

// SetKeeper appoints a team's scorekeeper; only the lobby owner may, and the
// keeper must be on that team.
func (b *Board) SetKeeper(team, player string, isOwner bool, l Lineup) error {
	if !isOwner {
		return fail("forbidden", "Only the lobby owner appoints scorekeepers.")
	}
	if b.Mode != Team {
		return fail("invalid", "Scorekeepers are only used when scores are kept by team.")
	}
	if b.entry(TeamEntry(team)) == nil {
		return fail("not_found", "There's no team %s.", team)
	}
	if l.Teams[player] != team {
		return fail("invalid", "A scorekeeper must be on the team they keep score for.")
	}
	b.Keepers[team] = player
	return nil
}

// SetTarget sets the winning score (0 for none); only the lobby owner may.
func (b *Board) SetTarget(target int, isOwner bool) error {
	if !isOwner {
		return fail("forbidden", "Only the lobby owner sets the target.")
	}
	if target < 0 || target > 100000 {
		return fail("bad_request", "The target is 0 (none) to 100000.")
	}
	b.Target = target
	return nil
}

// Leaders returns the entries at or past the target, best first.
func (b *Board) Leaders() []string {
	if b.Target <= 0 {
		return nil
	}
	var out []*Entry
	for _, e := range b.Entries {
		if e.Score >= b.Target {
			out = append(out, e)
		}
	}
	slices.SortFunc(out, func(a, c *Entry) int { return c.Score - a.Score })
	ids := make([]string, len(out))
	for i, e := range out {
		ids[i] = e.ID
	}
	return ids
}
