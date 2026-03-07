package commands

import "testing"

func TestAppendRunScreenTextTracksCompleteAndPartialLines(t *testing.T) {
	lines := []string{}
	current := ""

	appendRunScreenText(&lines, &current, "first line\nsecond")
	appendRunScreenText(&lines, &current, " line\nthird line\n")

	if len(lines) != 3 {
		t.Fatalf("expected 3 completed lines, got %d", len(lines))
	}
	if lines[0] != "first line" || lines[1] != "second line" || lines[2] != "third line" {
		t.Fatalf("unexpected completed lines: %#v", lines)
	}
	if current != "" {
		t.Fatalf("expected no trailing partial line, got %q", current)
	}
}

func TestStyleRunScreenLineCategories(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{line: "run #1  12:00:00  >  prompt", want: "run_header"},
		{line: "tool: exec_command", want: "tool"},
		{line: "  -> ok", want: "result"},
		{line: "done  2.1s", want: "done"},
		{line: "info: session", want: "info"},
		{line: "type /wait, /stop", want: "hint"},
		{line: "plain text", want: "plain"},
	}

	for _, tc := range cases {
		if got := runScreenLineStyleKind(tc.line); got != tc.want {
			t.Fatalf("line %q: expected %s, got %s", tc.line, tc.want, got)
		}
	}
}

func TestParseRunControlSubmission(t *testing.T) {
	cases := []struct {
		input string
		want  runControlEventType
	}{
		{input: "/quit", want: runControlQuit},
		{input: "/exit", want: runControlQuit},
		{input: "/stop", want: runControlStop},
		{input: "/wait", want: runControlWait},
		{input: "/resume", want: runControlResume},
		{input: "fix the bug", want: runControlPrompt},
	}

	for _, tc := range cases {
		event := parseRunControlSubmission(tc.input)
		if event.Type != tc.want {
			t.Fatalf("input %q: expected %s, got %s", tc.input, tc.want, event.Type)
		}
	}
}
