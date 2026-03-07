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
	if lines[0] != `- Bash("go test ./... 2>&1")` {
		t.Fatalf("unexpected first line %q", lines[0])
	}
	if lines[1] != "  Run all the tests" {
		t.Fatalf("unexpected description line %q", lines[1])
	}
}

func TestFormatRunToolCallLinesWrapsExtraArgumentsWithAlignedIndent(t *testing.T) {
	lines := formatRunToolCallLines(runToolCall{
		Name: "Read",
		Input: map[string]any{
			"file_path": "/tmp/example.txt",
			"limit":     17,
			"offset":    48,
		},
	})

	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %#v", lines)
	}
	if lines[0] != `- Read(file_path="/tmp/example.txt",` {
		t.Fatalf("unexpected first line %q", lines[0])
	}
	if lines[1] != `       limit=17,` {
		t.Fatalf("unexpected continuation line %q", lines[1])
	}
	if lines[2] != `       offset=48)` {
		t.Fatalf("unexpected final line %q", lines[2])
	}
}
