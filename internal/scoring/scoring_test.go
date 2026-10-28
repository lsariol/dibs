package scoring

import (
	"errors"
	"testing"
	"time"
)

var now = time.Now()

var lineup = Lineup{
	Players: []string{"ann", "bob", "cy", "dee"},
	Teams:   map[string]string{"ann": "A", "cy": "A", "bob": "B", "dee": "B"},
}

func code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func board(t *testing.T, m Mode) *Board {
	t.Helper()
	b := &Board{}
	if err := b.Rebuild(m, lineup); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSoloEditsOwnOnly(t *testing.T) {
	b := board(t, Solo)
	if len(b.Entries) != 4 {
		t.Fatal("solo scores per player, even with teams")
	}
	if err := b.Add("ann", "ann", 3, "", false, now); err != nil {
		t.Fatal(err)
	}
	if code(b.Add("ann", "bob", 3, "", false, now)) != "forbidden" {
		t.Fatal("solo: changed another player's score")
	}
	if code(b.Add("ann", "bob", 3, "", true, now)) != "forbidden" {
		t.Fatal("solo: the owner changed another player's score")
	}
}

func TestTeamScorekeepers(t *testing.T) {
	b := board(t, Team)
	if len(b.Entries) != 2 || b.Keepers["A"] != "ann" || b.Keepers["B"] != "bob" {
		t.Fatalf("team entries %v keepers %v", len(b.Entries), b.Keepers)
	}
	if err := b.Add("ann", TeamEntry("A"), 2, "march", false, now); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		actor, entry string
		owner        bool
	}{
		{"ann", TeamEntry("B"), false}, // keeper of the other team
		{"cy", TeamEntry("A"), false},  // on the team but not its keeper
		{"cy", TeamEntry("A"), true},   // the owner doesn't bypass team keepers
	} {
		if code(b.Add(tc.actor, tc.entry, 1, "", tc.owner, now)) != "forbidden" {
			t.Errorf("%+v was allowed", tc)
		}
	}
	if code(b.SetKeeper("A", "cy", false, lineup)) != "forbidden" {
		t.Fatal("non-owner appointed a keeper")
	}
	if code(b.SetKeeper("A", "bob", true, lineup)) != "invalid" {
		t.Fatal("keeper from the other team")
	}
	if err := b.SetKeeper("A", "cy", true, lineup); err != nil {
		t.Fatal(err)
	}
	if b.Add("ann", TeamEntry("A"), 1, "", false, now) == nil || b.Add("cy", TeamEntry("A"), 1, "", false, now) != nil {
		t.Fatal("replacing the keeper")
	}
}

func TestOwnerMode(t *testing.T) {
	b := board(t, Owner)
	if len(b.Entries) != 2 {
		t.Fatal("owner mode scores teams when there are teams")
	}
	if code(b.Add("bob", TeamEntry("B"), 1, "", false, now)) != "forbidden" {
		t.Fatal("non-owner scored")
	}
	if err := b.Add("ann", TeamEntry("B"), 1, "", true, now); err != nil {
		t.Fatal(err)
	}
	solo := &Board{}
	_ = solo.Rebuild(Owner, Lineup{Players: []string{"x", "y"}})
	if len(solo.Entries) != 2 || solo.Entries[0].Player != "x" {
		t.Fatal("owner mode without teams scores players")
	}
}

func TestTeamModeNeedsTeams(t *testing.T) {
	b := &Board{}
	if code(b.Rebuild(Team, Lineup{Players: []string{"x", "y"}})) != "invalid" {
		t.Fatal("team mode without teams accepted")
	}
}

func TestRevertAndHistory(t *testing.T) {
	b := board(t, Team)
	_ = b.Add("ann", TeamEntry("A"), 4, "lone sweep", false, now)
	id := b.History[0].ID
	if code(b.Revert("bob", id, false, now)) != "forbidden" {
		t.Fatal("other team's keeper reverted")
	}
	if err := b.Revert("ann", id, false, now); err != nil {
		t.Fatal(err)
	}
	if b.entry(TeamEntry("A")).Score != 0 || len(b.History) != 2 || b.History[1].Reverts != id {
		t.Fatal("revert")
	}
	if code(b.Revert("ann", id, false, now)) != "invalid" || code(b.Revert("ann", b.History[1].ID, false, now)) != "invalid" {
		t.Fatal("double revert")
	}
}

func TestRebuildKeepsScoresAndValidates(t *testing.T) {
	b := board(t, Team)
	_ = b.Add("ann", TeamEntry("A"), 7, "", false, now)
	_ = b.Rebuild(Team, lineup)
	if b.entry(TeamEntry("A")).Score != 7 {
		t.Fatal("rebuild lost a score")
	}
	if code(b.Add("ann", TeamEntry("A"), 0, "", false, now)) != "bad_request" || code(b.Add("ann", "nope", 1, "", false, now)) != "not_found" {
		t.Fatal("validation")
	}
	if b.Reset(false) == nil || b.Reset(true) != nil || b.entry(TeamEntry("A")).Score != 0 {
		t.Fatal("reset")
	}
}

func TestLeaders(t *testing.T) {
	b := board(t, Owner)
	_ = b.SetTarget(10, true)
	_ = b.Add("o", TeamEntry("A"), 11, "", true, now)
	_ = b.Add("o", TeamEntry("B"), 9, "", true, now)
	if l := b.Leaders(); len(l) != 1 || l[0] != TeamEntry("A") {
		t.Fatalf("leaders %v", l)
	}
}
