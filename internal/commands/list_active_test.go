package commands

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/beadhub/bdh/internal/client"
)

func TestFormatListActiveOutput_GroupedByRepo(t *testing.T) {
	resp := &client.BeadsIssuesResponse{
		Issues: []client.BeadsIssue{
			{BeadID: "bdh-001", Repo: "github.com/org/repo-a", Branch: "main", Title: "Fix auth", Status: "in_progress", Priority: 1, Type: "bug", Assignee: "alice"},
			{BeadID: "bdh-002", Repo: "github.com/org/repo-a", Branch: "main", Title: "Add tests", Status: "in_progress", Priority: 2, Type: "task", Assignee: "bob"},
			{BeadID: "bdh-003", Repo: "github.com/org/repo-b", Branch: "main", Title: "Deploy pipeline", Status: "in_progress", Priority: 2, Type: "feature", Assignee: "alice"},
		},
	}

	output := formatListActiveOutput(resp, false)

	// Should have repo headers
	if !strings.Contains(output, "github.com/org/repo-a") {
		t.Error("output should contain repo-a header")
	}
	if !strings.Contains(output, "github.com/org/repo-b") {
		t.Error("output should contain repo-b header")
	}

	// Should show bead IDs
	if !strings.Contains(output, "bdh-001") {
		t.Error("output should contain bdh-001")
	}
	if !strings.Contains(output, "bdh-003") {
		t.Error("output should contain bdh-003")
	}

	// Should show assignees
	if !strings.Contains(output, "alice") {
		t.Error("output should contain assignee alice")
	}
	if !strings.Contains(output, "bob") {
		t.Error("output should contain assignee bob")
	}

	// repo-a should appear before repo-b (alphabetical)
	idxA := strings.Index(output, "github.com/org/repo-a")
	idxB := strings.Index(output, "github.com/org/repo-b")
	if idxA >= idxB {
		t.Error("repos should be sorted alphabetically")
	}
}

func TestFormatListActiveOutput_Empty(t *testing.T) {
	resp := &client.BeadsIssuesResponse{
		Issues: []client.BeadsIssue{},
	}

	output := formatListActiveOutput(resp, false)

	if !strings.Contains(output, "No active beads") {
		t.Errorf("empty result should say no active beads, got: %q", output)
	}
}

func TestFormatListActiveOutput_JSON(t *testing.T) {
	resp := &client.BeadsIssuesResponse{
		Issues: []client.BeadsIssue{
			{BeadID: "bdh-001", Repo: "github.com/org/repo", Branch: "main", Title: "Fix auth", Status: "in_progress", Priority: 1, Type: "bug", Assignee: "alice"},
		},
	}

	output := formatListActiveOutput(resp, true)

	var parsed map[string]any
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("output should be valid JSON: %v", err)
	}

	issues, ok := parsed["issues"].([]any)
	if !ok || len(issues) != 1 {
		t.Error("JSON should contain issues array with 1 element")
	}
}

func TestFormatListActiveOutput_ShowsCount(t *testing.T) {
	resp := &client.BeadsIssuesResponse{
		Issues: []client.BeadsIssue{
			{BeadID: "bdh-001", Repo: "github.com/org/repo", Branch: "main", Title: "Fix auth", Status: "in_progress", Priority: 1, Type: "bug"},
			{BeadID: "bdh-002", Repo: "github.com/org/repo", Branch: "main", Title: "Add tests", Status: "in_progress", Priority: 2, Type: "task"},
		},
	}

	output := formatListActiveOutput(resp, false)

	if !strings.Contains(output, "Active beads across project (2)") {
		t.Error("output should contain the count header")
	}
}

func TestFormatListActiveOutput_NoAssignee(t *testing.T) {
	resp := &client.BeadsIssuesResponse{
		Issues: []client.BeadsIssue{
			{BeadID: "bdh-001", Repo: "github.com/org/repo", Branch: "main", Title: "Unassigned task", Status: "in_progress", Priority: 2, Type: "task", Assignee: ""},
		},
	}

	output := formatListActiveOutput(resp, false)

	if !strings.Contains(output, "bdh-001") {
		t.Error("output should contain bead ID")
	}
	if !strings.Contains(output, "Unassigned task") {
		t.Error("output should contain title")
	}
}

func TestFormatListActiveOutput_NonMainBranch(t *testing.T) {
	resp := &client.BeadsIssuesResponse{
		Issues: []client.BeadsIssue{
			{BeadID: "bdh-001", Repo: "github.com/org/repo", Branch: "feature-x", Title: "Feature work", Status: "in_progress", Priority: 2, Type: "feature"},
		},
	}

	output := formatListActiveOutput(resp, false)

	// Non-main branch should be visible
	if !strings.Contains(output, "feature-x") {
		t.Error("output should show non-main branch name")
	}
}

func TestFormatListActiveOutput_HasMore(t *testing.T) {
	resp := &client.BeadsIssuesResponse{
		Issues: []client.BeadsIssue{
			{BeadID: "bdh-001", Repo: "github.com/org/repo", Branch: "main", Title: "Task", Status: "in_progress", Priority: 2, Type: "task"},
		},
		HasMore: true,
	}

	output := formatListActiveOutput(resp, false)

	if !strings.Contains(output, "more available") {
		t.Error("output should warn about truncated results when HasMore is true")
	}
}

func TestFormatListActiveOutput_NoHasMoreWarning(t *testing.T) {
	resp := &client.BeadsIssuesResponse{
		Issues: []client.BeadsIssue{
			{BeadID: "bdh-001", Repo: "github.com/org/repo", Branch: "main", Title: "Task", Status: "in_progress", Priority: 2, Type: "task"},
		},
		HasMore: false,
	}

	output := formatListActiveOutput(resp, false)

	if strings.Contains(output, "more available") {
		t.Error("output should not warn about truncation when HasMore is false")
	}
}
