package commands

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/beadhub/bdh/internal/client"
)

func TestFormatProjectsListOutput_Text(t *testing.T) {
	result := &ProjectsListResult{
		Projects: []client.ProjectSummary{
			{
				ID:             "proj-123",
				Slug:           "my-project",
				Name:           "My Project",
				RepoCount:      3,
				WorkspaceCount: 2,
			},
			{
				ID:             "proj-456",
				Slug:           "another-project",
				Name:           "another-project", // same as slug
				RepoCount:      1,
				WorkspaceCount: 5,
			},
		},
	}

	output := formatProjectsListOutput(result, false)

	// Check header
	if !strings.Contains(output, "PROJECTS:") {
		t.Errorf("expected PROJECTS: header, got: %s", output)
	}

	// Check first project with different name
	if !strings.Contains(output, "my-project (My Project)") {
		t.Errorf("expected project with name in parens, got: %s", output)
	}
	if !strings.Contains(output, "3 repos, 2 workspaces") {
		t.Errorf("expected repo/workspace counts for first project, got: %s", output)
	}
	if !strings.Contains(output, "proj-123") {
		t.Errorf("expected project ID, got: %s", output)
	}

	// Check second project where name == slug (no parens)
	if strings.Contains(output, "another-project (another-project)") {
		t.Errorf("should not show name in parens when same as slug, got: %s", output)
	}
	if !strings.Contains(output, "another-project") {
		t.Errorf("expected project slug, got: %s", output)
	}

	// Check total
	if !strings.Contains(output, "Total: 2 project(s)") {
		t.Errorf("expected total count, got: %s", output)
	}
}

func TestFormatProjectsListOutput_EmptyList(t *testing.T) {
	result := &ProjectsListResult{
		Projects: []client.ProjectSummary{},
	}

	output := formatProjectsListOutput(result, false)

	if !strings.Contains(output, "No projects found") {
		t.Errorf("expected 'No projects found' message, got: %s", output)
	}
}

func TestFormatProjectsListOutput_JSON(t *testing.T) {
	result := &ProjectsListResult{
		Projects: []client.ProjectSummary{
			{
				ID:             "proj-123",
				Slug:           "my-project",
				Name:           "My Project",
				RepoCount:      3,
				WorkspaceCount: 2,
			},
		},
	}

	output := formatProjectsListOutput(result, true)

	// Parse JSON
	var parsed struct {
		Projects []client.ProjectSummary `json:"projects"`
		Count    int                     `json:"count"`
	}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if parsed.Count != 1 {
		t.Errorf("expected count 1, got %d", parsed.Count)
	}
	if len(parsed.Projects) != 1 {
		t.Errorf("expected 1 project, got %d", len(parsed.Projects))
	}
	if parsed.Projects[0].Slug != "my-project" {
		t.Errorf("expected slug 'my-project', got %s", parsed.Projects[0].Slug)
	}
}

// Note: listProjects() integration tests require dependency injection for config.Load().
// The pure function formatProjectsListOutput() is tested above.

func TestFormatProjectsListOutput_SingleProject(t *testing.T) {
	result := &ProjectsListResult{
		Projects: []client.ProjectSummary{
			{
				ID:             "proj-single",
				Slug:           "solo",
				Name:           "",
				RepoCount:      0,
				WorkspaceCount: 0,
			},
		},
	}

	output := formatProjectsListOutput(result, false)

	// Empty name should not add parens
	if strings.Contains(output, "()") {
		t.Errorf("should not show empty parens, got: %s", output)
	}

	// Check counts are shown even when zero
	if !strings.Contains(output, "0 repos, 0 workspaces") {
		t.Errorf("expected zero counts, got: %s", output)
	}

	// Singular "project" for 1
	if !strings.Contains(output, "Total: 1 project(s)") {
		t.Errorf("expected singular project, got: %s", output)
	}
}
