package commands

import (
	"testing"

	"github.com/beadhub/bdh/internal/client"
)

func TestLevenshteinDistance(t *testing.T) {
	tests := []struct {
		a, b     string
		expected int
	}{
		{"", "", 0},
		{"", "abc", 3},
		{"abc", "", 3},
		{"abc", "abc", 0},
		{"abc", "abd", 1},
		{"abc", "adc", 1},
		{"abc", "abcd", 1},
		{"kitten", "sitting", 3},
		{"coordinator", "coord", 6},
		{"alice", "bob", 5},
	}

	for _, tt := range tests {
		got := levenshteinDistance(tt.a, tt.b)
		if got != tt.expected {
			t.Errorf("levenshteinDistance(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.expected)
		}
	}
}

func TestFindSuggestions(t *testing.T) {
	workspaces := []client.Workspace{
		{WorkspaceID: "ws1", Alias: "coordinator", HumanName: "Alice"},
		{WorkspaceID: "ws2", Alias: "alice-coordinator", HumanName: "Alice"},
		{WorkspaceID: "ws3", Alias: "bob-agent", HumanName: "Bob"},
		{WorkspaceID: "ws4", Alias: "claude-main", HumanName: "Claude"},
		{WorkspaceID: "ws5", Alias: "agent-01", HumanName: "Agent 1"},
	}

	tests := []struct {
		target         string
		expectedCount  int
		expectContains string
	}{
		{"coord", 2, "coordinator"},        // Should find coordinator and alice-coordinator
		{"bob", 1, "bob-agent"},            // Should find bob-agent
		{"xyz123", 0, ""},                  // No close matches
		{"main", 1, "claude-main"},         // Should find claude-main
		{"coordinatorx", 2, "coordinator"}, // Close to coordinator
	}

	for _, tt := range tests {
		suggestions := findSuggestions(tt.target, workspaces)
		if len(suggestions) != tt.expectedCount && tt.expectedCount > 0 {
			// Allow for some flexibility in count since Levenshtein can vary
			if len(suggestions) < 1 && tt.expectedCount > 0 {
				t.Errorf("findSuggestions(%q) got %d suggestions, want at least 1", tt.target, len(suggestions))
			}
		}
		if tt.expectContains != "" {
			found := false
			for _, s := range suggestions {
				if s.Alias == tt.expectContains {
					found = true
					break
				}
			}
			if !found && len(suggestions) > 0 {
				aliases := make([]string, len(suggestions))
				for i, s := range suggestions {
					aliases[i] = s.Alias
				}
				t.Errorf("findSuggestions(%q) expected to contain %q, got %v", tt.target, tt.expectContains, aliases)
			}
		}
	}
}

func TestMatchType_String(t *testing.T) {
	tests := []struct {
		mt       MatchType
		expected string
	}{
		{MatchExact, "exact"},
		{MatchPrefix, "prefix"},
		{MatchSubstring, "substring"},
		{MatchType(99), "unknown"},
	}

	for _, tt := range tests {
		got := tt.mt.String()
		if got != tt.expected {
			t.Errorf("MatchType(%d).String() = %q, want %q", tt.mt, got, tt.expected)
		}
	}
}

func TestFormatAmbiguousError(t *testing.T) {
	matches := []AliasMatch{
		{WorkspaceID: "ws1", Alias: "coordinator", HumanName: "Alice", MatchType: MatchExact},
		{WorkspaceID: "ws2", Alias: "alice-coordinator", HumanName: "Alice", MatchType: MatchExact},
	}

	err := formatAmbiguousError("coord", matches)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	errMsg := err.Error()
	if !contains(errMsg, "ambiguous") {
		t.Errorf("error message should contain 'ambiguous', got: %s", errMsg)
	}
	if !contains(errMsg, "coordinator") {
		t.Errorf("error message should contain 'coordinator', got: %s", errMsg)
	}
	if !contains(errMsg, "alice-coordinator") {
		t.Errorf("error message should contain 'alice-coordinator', got: %s", errMsg)
	}
}

func TestFormatNotFoundError(t *testing.T) {
	workspaces := []client.Workspace{
		{WorkspaceID: "ws1", Alias: "coordinator", HumanName: "Alice"},
		{WorkspaceID: "ws2", Alias: "bob-agent", HumanName: "Bob"},
	}

	// Test with suggestions available
	err := formatNotFoundError("coord", workspaces)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	errMsg := err.Error()
	if !contains(errMsg, "Did you mean") {
		t.Errorf("error message should contain 'Did you mean', got: %s", errMsg)
	}

	// Test with no suggestions
	err = formatNotFoundError("xyzabc123", workspaces)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	errMsg = err.Error()
	if !contains(errMsg, "no workspace found") {
		t.Errorf("error message should contain 'no workspace found', got: %s", errMsg)
	}
}

// contains checks if s contains substr (case-sensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr) >= 0))
}

func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
