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
		{line: `- Bash("go test ./... 2>&1")`, want: "tool"},
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

func TestRunInputValueFromLinePreservesSpaces(t *testing.T) {
	got := runInputValueFromLine("beadhub:bdh:noah> hello world ", "beadhub:bdh:noah> ")
	if got != "hello world " {
		t.Fatalf("expected trailing/internal spaces to be preserved, got %q", got)
	}

	got = runInputValueFromLine("beadhub:bdh:noah>  leading", "beadhub:bdh:noah> ")
	if got != " leading" {
		t.Fatalf("expected leading spaces after prompt to be preserved, got %q", got)
	}
}

func TestRunScreenSetInputLineKeepsLeadingSpace(t *testing.T) {
	screen := &runScreenManager{promptLabel: "beadhub:bdh:noah> "}

	screen.SetInputLine("beadhub:bdh:noah>  leading")

	if !screen.pending {
		t.Fatal("expected leading-space input to count as pending")
	}
	if screen.inputLine != "beadhub:bdh:noah>  leading" {
		t.Fatalf("expected input line to preserve leading space, got %q", screen.inputLine)
	}
}

func TestRunIdentityPromptLabelUsesProjectRepoAndAlias(t *testing.T) {
	got := runIdentityPromptLabel("beadhub", "github.com/beadhub/bdh", "", "noah")
	if got != "beadhub:bdh:noah> " {
		t.Fatalf("expected identity prompt label, got %q", got)
	}
}

func TestRunShortRepoNameFallsBackToRepoOrigin(t *testing.T) {
	got := runShortRepoName("", "git@github.com:beadhub/bdh.git")
	if got != "bdh" {
		t.Fatalf("expected repo short name from repo origin, got %q", got)
	}
}

func TestWrapRunScreenLineWrapsLongToolFields(t *testing.T) {
	lines := wrapRunScreenLine(`  command="git fetch origin main && git log --oneline origin/main -5"`, 32)
	if len(lines) < 2 {
		t.Fatalf("expected wrapped lines, got %#v", lines)
	}
	for _, line := range lines[1:] {
		if line == "" || line[:2] != "  " {
			t.Fatalf("expected wrapped continuation lines to keep indentation, got %#v", lines)
		}
	}
}

func TestWrapRunScreenLineKeepsToolArgIndent(t *testing.T) {
	lines := wrapRunScreenLine(`       file_path="/Users/juanre/prj/beadhub-all/beadhub/src/beadhub/routes/tasks.py",`, 40)
	if len(lines) < 2 {
		t.Fatalf("expected wrapped lines, got %#v", lines)
	}
	for _, line := range lines[1:] {
		if line == "" || line[:7] != "       " {
			t.Fatalf("expected wrapped continuation lines to keep tool arg indentation, got %#v", lines)
		}
	}
}
