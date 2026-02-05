package commands

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestFormatStatusOutput_BasicIdentity(t *testing.T) {
	result := &StatusResult{
		Alias: "test-agent",
		Role:  "implementer",
	}

	output := formatStatusOutput(result, false)

	if !strings.Contains(output, "## You") {
		t.Error("output should contain '## You' header")
	}
	if !strings.Contains(output, "Alias: test-agent") {
		t.Error("output should contain alias")
	}
	if !strings.Contains(output, "Role: implementer") {
		t.Error("output should contain role")
	}
}

func TestFormatStatusOutput_NoRole(t *testing.T) {
	result := &StatusResult{
		Alias: "test-agent",
		Role:  "",
	}

	output := formatStatusOutput(result, false)

	if strings.Contains(output, "Role:") {
		t.Error("output should not contain Role when empty")
	}
}

func TestFormatStatusOutput_YourClaims(t *testing.T) {
	claimedAt := time.Now().Add(-30 * time.Minute).Format(time.RFC3339)
	result := &StatusResult{
		Alias: "test-agent",
		Role:  "implementer",
		YourClaims: []ClaimInfo{
			{BeadID: "test-123", Title: "Fix the bug", ClaimedAt: claimedAt},
		},
	}

	output := formatStatusOutput(result, false)

	if !strings.Contains(output, "## Your Claims") {
		t.Error("output should contain claims header")
	}
	if !strings.Contains(output, "test-123") {
		t.Error("output should contain bead ID")
	}
	if !strings.Contains(output, "Fix the bug") {
		t.Error("output should contain claim title")
	}
}

func TestFormatStatusOutput_StaleClaim(t *testing.T) {
	// Claim older than 24 hours
	claimedAt := time.Now().Add(-25 * time.Hour).Format(time.RFC3339)
	result := &StatusResult{
		Alias: "test-agent",
		YourClaims: []ClaimInfo{
			{BeadID: "old-123", Title: "Old claim", ClaimedAt: claimedAt},
		},
	}

	output := formatStatusOutput(result, false)

	if !strings.Contains(output, "⚠️") {
		t.Error("stale claim should have warning indicator")
	}
}

func TestFormatStatusOutput_YourReservations(t *testing.T) {
	result := &StatusResult{
		Alias: "test-agent",
		YourLocks: []LockSummary{
			{Path: "src/main.go", TTLRemainingSeconds: 300},
			{Path: "src/utils.go", TTLRemainingSeconds: 120},
		},
	}

	output := formatStatusOutput(result, false)

	if !strings.Contains(output, "## Your Reservations") {
		t.Error("output should contain reservations header")
	}
	if !strings.Contains(output, "src/main.go") {
		t.Error("output should contain first reservation path")
	}
	if !strings.Contains(output, "src/utils.go") {
		t.Error("output should contain second reservation path")
	}
	if !strings.Contains(output, "expires in") {
		t.Error("output should contain expiry info")
	}
}

func TestFormatStatusOutput_EmptyTeam(t *testing.T) {
	result := &StatusResult{
		Alias: "test-agent",
		Team:  []TeamMemberInfo{},
	}

	output := formatStatusOutput(result, false)

	if !strings.Contains(output, "## Team") {
		t.Error("output should contain team header")
	}
	if !strings.Contains(output, "No other team members") {
		t.Error("output should indicate no other team members")
	}
}

func TestFormatStatusOutput_TeamMember(t *testing.T) {
	claimedAt := time.Now().Add(-10 * time.Minute).Format(time.RFC3339)
	lastSeen := time.Now().Add(-5 * time.Minute).Format(time.RFC3339)

	result := &StatusResult{
		Alias: "test-agent",
		Team: []TeamMemberInfo{
			{
				Alias:    "alice-coordinator",
				Role:     "coordinator",
				Status:   "active",
				LastSeen: lastSeen,
				RepoName: "github.com/test/repo",
				ApexID:   "task-456",
				ApexTitle: "Implement feature",
				Claims: []ClaimInfo{
					{BeadID: "task-456", Title: "Implement feature", ClaimedAt: claimedAt},
				},
			},
		},
	}

	output := formatStatusOutput(result, false)

	if !strings.Contains(output, "alice-coordinator") {
		t.Error("output should contain team member alias")
	}
	if !strings.Contains(output, "coordinator") {
		t.Error("output should contain team member role")
	}
	if !strings.Contains(output, "active") {
		t.Error("output should contain team member status")
	}
	if !strings.Contains(output, "github.com/test/repo") {
		t.Error("output should contain repo name")
	}
	if !strings.Contains(output, "Working on: task-456") {
		t.Error("output should contain working on info")
	}
	if !strings.Contains(output, "Claims:") {
		t.Error("output should contain claims section")
	}
}

func TestFormatStatusOutput_TeamMemberWithBranch(t *testing.T) {
	lastSeen := time.Now().Format(time.RFC3339)

	result := &StatusResult{
		Alias: "test-agent",
		Team: []TeamMemberInfo{
			{
				Alias:    "bob-agent",
				Status:   "active",
				LastSeen: lastSeen,
				RepoName: "github.com/test/repo",
				Branch:   "feature-branch",
			},
		},
	}

	output := formatStatusOutput(result, false)

	if !strings.Contains(output, "github.com/test/repo (feature-branch)") {
		t.Error("output should show repo with branch when not main/master")
	}
}

func TestFormatStatusOutput_TeamMemberMainBranchHidden(t *testing.T) {
	lastSeen := time.Now().Format(time.RFC3339)

	result := &StatusResult{
		Alias: "test-agent",
		Team: []TeamMemberInfo{
			{
				Alias:    "bob-agent",
				Status:   "active",
				LastSeen: lastSeen,
				RepoName: "github.com/test/repo",
				Branch:   "main",
			},
		},
	}

	output := formatStatusOutput(result, false)

	if strings.Contains(output, "(main)") {
		t.Error("output should NOT show branch when it's main")
	}
	if !strings.Contains(output, "Repo: github.com/test/repo\n") {
		t.Error("output should show repo without branch")
	}
}

func TestFormatStatusOutput_TeamMemberEpic(t *testing.T) {
	lastSeen := time.Now().Format(time.RFC3339)

	result := &StatusResult{
		Alias: "test-agent",
		Team: []TeamMemberInfo{
			{
				Alias:     "bob-agent",
				Status:    "active",
				LastSeen:  lastSeen,
				ApexID:    "epic-789",
				ApexTitle: "Big project",
				ApexType:  "epic",
			},
		},
	}

	output := formatStatusOutput(result, false)

	if !strings.Contains(output, "Epic: epic-789") {
		t.Error("output should show 'Epic:' prefix for epic type")
	}
}

func TestFormatStatusOutput_TeamMemberReservations(t *testing.T) {
	lastSeen := time.Now().Format(time.RFC3339)

	result := &StatusResult{
		Alias: "test-agent",
		Team: []TeamMemberInfo{
			{
				Alias:    "bob-agent",
				Status:   "active",
				LastSeen: lastSeen,
				Locks: []LockSummary{
					{Path: "file1.go", TTLRemainingSeconds: 300},
					{Path: "file2.go", TTLRemainingSeconds: 200},
				},
			},
		},
	}

	output := formatStatusOutput(result, false)

	if !strings.Contains(output, "Reservations:") {
		t.Error("output should contain reservations section")
	}
	if !strings.Contains(output, "file1.go") {
		t.Error("output should contain first reservation")
	}
	if !strings.Contains(output, "file2.go") {
		t.Error("output should contain second reservation")
	}
}

func TestFormatStatusOutput_TeamMemberReservationsTruncated(t *testing.T) {
	lastSeen := time.Now().Format(time.RFC3339)

	// Create more locks than the max limit
	locks := make([]LockSummary, 15)
	for i := range locks {
		locks[i] = LockSummary{
			Path:                "file" + string(rune('a'+i)) + ".go",
			TTLRemainingSeconds: 300,
		}
	}

	result := &StatusResult{
		Alias: "test-agent",
		Team: []TeamMemberInfo{
			{
				Alias:    "bob-agent",
				Status:   "active",
				LastSeen: lastSeen,
				Locks:    locks,
			},
		},
	}

	output := formatStatusOutput(result, false)

	// Should show first 5 (defaultStatusTeamReservationsMax) and then "...10 more"
	if !strings.Contains(output, "...10 more") {
		t.Errorf("output should indicate truncation, got:\n%s", output)
	}
}

func TestFormatStatusOutput_Escalations(t *testing.T) {
	result := &StatusResult{
		Alias:              "test-agent",
		EscalationsPending: 3,
	}

	output := formatStatusOutput(result, false)

	if !strings.Contains(output, "## Escalations") {
		t.Error("output should contain escalations header")
	}
	if !strings.Contains(output, "3 pending escalation(s)") {
		t.Error("output should show escalation count")
	}
}

func TestFormatStatusOutput_NoEscalations(t *testing.T) {
	result := &StatusResult{
		Alias:              "test-agent",
		EscalationsPending: 0,
	}

	output := formatStatusOutput(result, false)

	if strings.Contains(output, "## Escalations") {
		t.Error("output should NOT contain escalations header when count is 0")
	}
}

func TestFormatStatusOutput_JSON(t *testing.T) {
	claimedAt := time.Now().Format(time.RFC3339)

	result := &StatusResult{
		Alias: "test-agent",
		Role:  "implementer",
		YourClaims: []ClaimInfo{
			{BeadID: "test-123", Title: "Test claim", ClaimedAt: claimedAt},
		},
		Team: []TeamMemberInfo{
			{Alias: "alice", Role: "coordinator", Status: "active", LastSeen: claimedAt},
		},
		EscalationsPending: 1,
	}

	output := formatStatusOutput(result, true)

	// Verify it's valid JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("output should be valid JSON: %v", err)
	}

	// Check key fields
	if parsed["Alias"] != "test-agent" {
		t.Error("JSON should contain Alias")
	}
	if parsed["Role"] != "implementer" {
		t.Error("JSON should contain Role")
	}

	claims, ok := parsed["YourClaims"].([]interface{})
	if !ok || len(claims) != 1 {
		t.Error("JSON should contain YourClaims array with 1 element")
	}

	team, ok := parsed["Team"].([]interface{})
	if !ok || len(team) != 1 {
		t.Error("JSON should contain Team array with 1 element")
	}
}

func TestFormatStatusOutput_ClaimWithoutTitle(t *testing.T) {
	claimedAt := time.Now().Format(time.RFC3339)
	result := &StatusResult{
		Alias: "test-agent",
		YourClaims: []ClaimInfo{
			{BeadID: "test-123", Title: "", ClaimedAt: claimedAt},
		},
	}

	output := formatStatusOutput(result, false)

	// Should show just the bead ID without quotes
	if !strings.Contains(output, "- test-123 —") {
		t.Errorf("claim without title should show just ID, got:\n%s", output)
	}
}
