package commands

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

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
		{line: "type /wait, /autofeed off, /stop", want: "hint"},
		{line: "plain text", want: "plain"},
	}

	for _, tc := range cases {
		if got := runScreenLineStyleKind(tc.line); got != tc.want {
			t.Fatalf("line %q: expected %s, got %s", tc.line, tc.want, got)
		}
	}
}

func TestStyleRunScreenLineKeepsToolArgumentsNeutralOnFirstLine(t *testing.T) {
	styles := newRunScreenStyles()
	got := styleRunScreenLine(`- View("/tmp/image.png")`, styles)
	want := styles.tool.Render(`- View(`) + `"/tmp/image.png"` + styles.tool.Render(`)`)
	if got != want {
		t.Fatalf("unexpected styled tool line %q", got)
	}
}

func TestStyleRunScreenLineColorsClosingParenOnContinuation(t *testing.T) {
	styles := newRunScreenStyles()
	got := styleRunScreenLine(`       offset=48)`, styles)
	want := `       offset=48` + styles.tool.Render(`)`)
	if got != want {
		t.Fatalf("unexpected styled continuation line %q", got)
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
		{input: "/autofeed on", want: runControlAutofeedOn},
		{input: "/autofeed off", want: runControlAutofeedOff},
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

func TestRunStatusIdentityLabelFormatsProviderProjectRepoAlias(t *testing.T) {
	got := runStatusIdentityLabel("claude", "beadhub", "github.com/beadhub/bdh", "", "noah")
	if got != "[claude]@beadhub:bdh:noah" {
		t.Fatalf("expected footer identity, got %q", got)
	}
}

func TestRunStatusIdentityLabelOmitsRepoWhenEmpty(t *testing.T) {
	got := runStatusIdentityLabel("claude", "beadhub", "", "", "noah")
	if got != "[claude]@beadhub:noah" {
		t.Fatalf("expected identity without repo, got %q", got)
	}
}

func TestRunStatusIdentityLabelFallsBackToProviderOnly(t *testing.T) {
	got := runStatusIdentityLabel("claude", "", "", "", "")
	if got != "[claude]" {
		t.Fatalf("expected provider-only identity, got %q", got)
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

func TestRunScreenViewAddsBottomBreathingSpace(t *testing.T) {
	model := newRunScreenModel(
		runScreenSnapshot{
			Lines:       []string{"line 1"},
			StatusLine:  "next run in 6s",
			InputLine:   ">> hello",
			PromptLabel: ">> ",
			FooterID:    "[claude]@beadhub:bdh:noah",
		},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	model.width = 40
	model.height = 10
	model.syncLayout()

	view := model.View()
	if !strings.Contains(view, "\n────────────────────────────────────────\n") {
		t.Fatalf("expected divider above input, got %q", view)
	}
	if !strings.Contains(view, ">> hello") {
		t.Fatalf("expected input below divider, got %q", view)
	}
	if !strings.Contains(view, "[claude]@beadhub:bdh:noah") || !strings.Contains(view, "running") {
		t.Fatalf("expected footer identity and status at bottom, got %q", view)
	}
}

func TestRunInputVisualHeightWrapsLongInput(t *testing.T) {
	got := runInputVisualHeight("beadhub:bdh:noah> ", strings.Repeat("x", 40), 30)
	if got < 2 {
		t.Fatalf("expected wrapped input height > 1, got %d", got)
	}
}

func TestRunScreenViewKeepsInputAtFixedHeight(t *testing.T) {
	model := newRunScreenModel(
		runScreenSnapshot{
			Lines:       []string{"line 1", "line 2"},
			StatusLine:  "next run in 6s",
			InputLine:   ">> " + strings.Repeat("x", 40),
			PromptLabel: ">> ",
		},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	model.width = 30
	model.height = 10
	model.syncLayout()

	if model.input.Height() != 2 {
		t.Fatalf("expected fixed two-line input height, got %d", model.input.Height())
	}
	if model.viewport.Height != 6 {
		t.Fatalf("expected viewport height to account for divider, input, and footer, got %d", model.viewport.Height)
	}

	view := model.View()
	if !strings.Contains(view, "running") {
		t.Fatalf("expected status line in view, got %q", view)
	}
	if !strings.Contains(view, ">> ") {
		t.Fatalf("expected prompt label in view, got %q", view)
	}
}

func TestRunScreenArrowUpMovesFocusToViewport(t *testing.T) {
	model := newRunScreenModel(
		runScreenSnapshot{
			Lines:       []string{"line 1", "line 2", "line 3", "line 4", "line 5", "line 6", "line 7", "line 8"},
			StatusLine:  "waiting for work in 30s",
			InputLine:   ">> draft",
			PromptLabel: ">> ",
		},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	model.width = 30
	model.height = 10
	model.syncLayout()

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(runScreenModel)

	if model.focus != runScreenFocusViewport {
		t.Fatalf("expected viewport focus after arrow up, got %v", model.focus)
	}
}

func TestRunScreenTypingFromViewportReturnsFocusToInput(t *testing.T) {
	model := newRunScreenModel(
		runScreenSnapshot{
			Lines:       []string{"line 1", "line 2", "line 3", "line 4", "line 5", "line 6", "line 7", "line 8"},
			StatusLine:  "waiting for work in 30s",
			InputLine:   ">> draft",
			PromptLabel: ">> ",
		},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	model.width = 30
	model.height = 10
	model.syncLayout()
	model.setFocus(runScreenFocusViewport)

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	model = updated.(runScreenModel)

	if model.focus != runScreenFocusInput {
		t.Fatalf("expected typing to return focus to input, got %v", model.focus)
	}
	if model.input.Value() != "draftx" {
		t.Fatalf("expected typed rune to go into input, got %q", model.input.Value())
	}
}

func TestRunScreenWaitingFooterUsesEllipsisStatus(t *testing.T) {
	if got := runScreenFooterStatus("waiting for work in 30s"); got != "...waiting for work" {
		t.Fatalf("expected waiting footer status, got %q", got)
	}
}

func TestRunScreenExitConfirmationAcceptsYWithoutTypingIntoInput(t *testing.T) {
	confirmed := false
	model := newRunScreenModel(
		runScreenSnapshot{
			InputLine:   ">> draft",
			PromptLabel: ">> ",
			ExitConfirm: true,
		},
		nil,
		nil,
		nil,
		nil,
		func() { confirmed = true },
		nil,
	)

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	model = updated.(runScreenModel)

	if !confirmed {
		t.Fatal("expected y to confirm exit")
	}
	if model.input.Value() != "draft" {
		t.Fatalf("expected input to remain unchanged, got %q", model.input.Value())
	}
}

func TestRunScreenExitConfirmationCancelsAndResumesTyping(t *testing.T) {
	canceled := false
	model := newRunScreenModel(
		runScreenSnapshot{
			InputLine:   ">> draft",
			PromptLabel: ">> ",
			ExitConfirm: true,
		},
		nil,
		nil,
		nil,
		nil,
		nil,
		func() { canceled = true },
	)

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	model = updated.(runScreenModel)

	if !canceled {
		t.Fatal("expected non-confirming input to cancel exit confirmation")
	}
	if model.exitConfirm {
		t.Fatal("expected exit confirmation mode to clear")
	}
	if model.input.Value() != "draftx" {
		t.Fatalf("expected typing to continue after canceling exit confirmation, got %q", model.input.Value())
	}
}
