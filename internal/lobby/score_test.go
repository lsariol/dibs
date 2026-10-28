package lobby

import (
	"strings"
	"testing"

	"github.com/lsariol/dibs/internal/scoring"
)

func teamLobby(t *testing.T, mode string) (*Lobby, []string) {
	t.Helper()
	_, l, ids := newLobby(t, Settings{MaxPlayers: 4, ScoreMode: mode}, "Ann", "Bob", "Cy", "Dee")
	for i, id := range ids {
		mustOK(t, l.Do(ids[0], req("lobby.team", map[string]any{"player": id, "team": []string{"A", "B"}[i%2]})))
	}
	mustOK(t, l.Do(ids[0], req("table.start", nil)))
	return l, ids
}

func score(l *Lobby, entry string) int {
	for _, e := range l.score.Entries {
		if e.ID == entry {
			return e.Score
		}
	}
	return -999
}

func TestScoreModesThroughTheLobby(t *testing.T) {
	A, B := scoring.TeamEntry("A"), scoring.TeamEntry("B")

	l, ids := teamLobby(t, ScoreTeam)
	ann, bob, cy := ids[0], ids[1], ids[2]
	mustOK(t, l.Do(ann, req("score.add", map[string]any{"entry": A, "delta": 2, "reason": "march"})))
	mustFail(t, l.Do(ann, req("score.add", map[string]any{"entry": B, "delta": 1})), CodeForbidden)
	mustFail(t, l.Do(cy, req("score.add", map[string]any{"entry": A, "delta": 1})), CodeForbidden)
	mustOK(t, l.Do(bob, req("score.add", map[string]any{"entry": B, "delta": 1})))
	mustFail(t, l.Do(bob, req("score.keeper", map[string]any{"team": "A", "player": cy})), CodeForbidden)
	mustOK(t, l.Do(ann, req("score.keeper", map[string]any{"team": "A", "player": cy})))
	mustOK(t, l.Do(cy, req("score.add", map[string]any{"entry": A, "delta": 1})))
	if score(l, A) != 3 || score(l, B) != 1 {
		t.Fatalf("team scores A %d B %d", score(l, A), score(l, B))
	}

	l, ids = teamLobby(t, ScoreOwner)
	mustFail(t, l.Do(ids[1], req("score.add", map[string]any{"entry": B, "delta": 1})), CodeForbidden)
	mustOK(t, l.Do(ids[0], req("score.add", map[string]any{"entry": B, "delta": 1})))

	l, ids = teamLobby(t, ScoreSolo)
	if len(l.score.Entries) != 4 {
		t.Fatal("solo mode in a team game scores players")
	}
	mustOK(t, l.Do(ids[1], req("score.add", map[string]any{"entry": ids[1], "delta": 5})))
	mustFail(t, l.Do(ids[1], req("score.add", map[string]any{"entry": ids[3], "delta": 5})), CodeForbidden)
	mustFail(t, l.Do(ids[0], req("score.add", map[string]any{"entry": ids[3], "delta": 5})), CodeForbidden)
}

func TestScoresSurviveTableResetAndCorrections(t *testing.T) {
	l, ids := teamLobby(t, ScoreTeam)
	A := scoring.TeamEntry("A")
	mustOK(t, l.Do(ids[0], req("score.add", map[string]any{"entry": A, "delta": 4})))
	change := l.score.History[0].ID
	mustOK(t, l.Do(ids[0], req("table.reset", nil)))
	if score(l, A) != 4 {
		t.Fatal("reset table lost the scores")
	}
	mustFail(t, l.Do(ids[1], req("score.revert", map[string]any{"change": change})), CodeForbidden)
	mustOK(t, l.Do(ids[0], req("score.revert", map[string]any{"change": change})))
	if score(l, A) != 0 {
		t.Fatal("correction")
	}
	mustFail(t, l.Do(ids[1], req("score.reset", nil)), CodeForbidden)
	mustOK(t, l.Do(ids[0], req("score.target", map[string]any{"target": 10})))
	snap, _ := l.Snapshot(ids[1])
	if !contains(string(snap), `"target":10`) || !contains(string(snap), `"canEdit":["team:B"]`) {
		t.Fatalf("score view: %s", snap)
	}
}

func TestTeamModeWithoutTeamsCantStart(t *testing.T) {
	_, l, ids := newLobby(t, Settings{ScoreMode: ScoreTeam}, "Ann", "Bob")
	mustFail(t, l.Do(ids[0], req("table.start", nil)), CodeInvalid)
	if l.State != Waiting || l.game != nil || l.score != nil {
		t.Fatal("a failed start changed the lobby")
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
