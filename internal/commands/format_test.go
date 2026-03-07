package commands

import "testing"

func TestFormatRunToolCallLinesUsesCompactCommandAndDescription(t *testing.T) {
	lines := formatRunToolCallLines(runToolCall{
		Name: "Bash",
		Input: map[string]any{
			"command":     "go test ./... 2>&1",
			"description": "Run all the tests",
		},
	})

	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %#v", lines)
	}
	if lines[0] != `tool: Bash("go test ./... 2>&1")` {
		t.Fatalf("unexpected summary line %q", lines[0])
	}
	if lines[1] != "      Run all the tests" {
		t.Fatalf("unexpected description line %q", lines[1])
	}
}

func TestFormatRunToolCallLinesKeepsExtraArgumentsInline(t *testing.T) {
	lines := formatRunToolCallLines(runToolCall{
		Name: "ToolSearch",
		Input: map[string]any{
			"query":       "select:Bash",
			"max_results": 1,
		},
	})

	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %#v", lines)
	}
	if lines[0] != `tool: ToolSearch("select:Bash", max_results=1)` {
		t.Fatalf("unexpected summary line %q", lines[0])
	}
}
