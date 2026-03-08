package commands

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type fakeRunInputController struct {
	events  chan runControlEvent
	mu      sync.Mutex
	pending bool
}

func newFakeRunInputController() *fakeRunInputController {
	return &fakeRunInputController{events: make(chan runControlEvent, 32)}
}

func (f *fakeRunInputController) Start() error                   { return nil }
func (f *fakeRunInputController) Stop() error                    { return nil }
func (f *fakeRunInputController) Events() <-chan runControlEvent { return f.events }
func (f *fakeRunInputController) HasPendingInput() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pending
}
func (f *fakeRunInputController) send(event runControlEvent) {
	f.events <- event
}
func (f *fakeRunInputController) setPending(pending bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pending = pending
}

func TestRunLoopStartsFreshThenKeepsExactSessionWithoutContinueMode(t *testing.T) {
	provider := claudeProvider{}
	var commands [][]string

	loop := &runLoop{
		provider: provider,
		now:      func() time.Time { return time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC) },
		sleep:    func(context.Context, time.Duration) error { return nil },
		out:      io.Discard,
		runner: func(_ context.Context, _ string, argv []string, onLine func(string), _ io.Writer) error {
			commands = append(commands, append([]string(nil), argv...))
			onLine(`{"type":"result","duration_ms":1000,"session_id":"sess-42"}`)
			return nil
		},
	}

	err := loop.Run(context.Background(), runLoopOptions{
		Prompt:  "continue working",
		MaxRuns: 2,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(commands))
	}
	if strings.Contains(strings.Join(commands[0], " "), "--continue") {
		t.Fatalf("first run should start fresh: %q", strings.Join(commands[0], " "))
	}
	if strings.Contains(strings.Join(commands[0], " "), "--resume") {
		t.Fatalf("first run should not resume an existing session: %q", strings.Join(commands[0], " "))
	}
	if !strings.Contains(strings.Join(commands[1], " "), "--resume sess-42") {
		t.Fatalf("second run should resume exact captured session even without --continue: %q", strings.Join(commands[1], " "))
	}
}

func TestRunLoopUsesContinueWhenEnabled(t *testing.T) {
	provider := claudeProvider{}
	var commands [][]string

	loop := &runLoop{
		provider: provider,
		now:      func() time.Time { return time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC) },
		sleep:    func(context.Context, time.Duration) error { return nil },
		out:      io.Discard,
		runner: func(_ context.Context, _ string, argv []string, onLine func(string), _ io.Writer) error {
			commands = append(commands, append([]string(nil), argv...))
			onLine(`{"type":"result","duration_ms":1000}`)
			return nil
		},
	}

	err := loop.Run(context.Background(), runLoopOptions{
		Prompt:       "continue working",
		ContinueMode: true,
		MaxRuns:      2,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(commands))
	}
	if !strings.Contains(strings.Join(commands[0], " "), "--continue") {
		t.Fatalf("first run should continue the most recent provider session: %q", strings.Join(commands[0], " "))
	}
	if !strings.Contains(strings.Join(commands[1], " "), "--continue") {
		t.Fatalf("second run should continue the provider session when no exact session id was captured: %q", strings.Join(commands[1], " "))
	}
}

func TestRunLoopUsesExactSessionIDAfterFirstRunWhenContinueEnabled(t *testing.T) {
	provider := claudeProvider{}
	var commands [][]string

	loop := &runLoop{
		provider: provider,
		now:      func() time.Time { return time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC) },
		sleep:    func(context.Context, time.Duration) error { return nil },
		out:      io.Discard,
		runner: func(_ context.Context, _ string, argv []string, onLine func(string), _ io.Writer) error {
			commands = append(commands, append([]string(nil), argv...))
			onLine(`{"type":"result","duration_ms":1000,"session_id":"sess-42"}`)
			return nil
		},
	}

	err := loop.Run(context.Background(), runLoopOptions{
		Prompt:       "continue working",
		ContinueMode: true,
		MaxRuns:      2,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(commands))
	}
	if !strings.Contains(strings.Join(commands[0], " "), "--continue") {
		t.Fatalf("first run should use provider continue mode, got: %q", strings.Join(commands[0], " "))
	}
	if !strings.Contains(strings.Join(commands[1], " "), "--resume sess-42") {
		t.Fatalf("second run should resume exact captured session id, got: %q", strings.Join(commands[1], " "))
	}
}

func TestRunLoopFormatsOutput(t *testing.T) {
	var output strings.Builder

	loop := &runLoop{
		provider: claudeProvider{},
		now:      func() time.Time { return time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC) },
		sleep:    func(context.Context, time.Duration) error { return nil },
		out:      &output,
		runner: func(_ context.Context, _ string, _ []string, onLine func(string), _ io.Writer) error {
			onLine(`{"type":"stream_event","event":{"delta":{"type":"text_delta","text":"Thinking..."}}}`)
			onLine(`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"exec_command","input":{"cmd":"pwd"}}]}}`)
			onLine(`{"type":"tool_result","content":"ok"}`)
			onLine(`{"type":"result","duration_ms":2100,"cost_usd":0.0015,"session_id":"s1"}`)
			return nil
		},
	}

	err := loop.Run(context.Background(), runLoopOptions{
		Prompt:      "inspect workspace",
		MaxRuns:     1,
		WaitSeconds: 0,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	text := output.String()
	if !strings.Contains(text, "run #1  12:00:00  >  inspect workspace") {
		t.Fatalf("expected run header, got: %q", text)
	}
	if !strings.Contains(text, "Thinking...") {
		t.Fatalf("expected streamed text, got: %q", text)
	}
	if !strings.Contains(text, "Thinking...\n\n- exec_command(\"pwd\")") {
		t.Fatalf("expected blank line between text and tool block, got: %q", text)
	}
	if !strings.Contains(text, `- exec_command("pwd")`) {
		t.Fatalf("expected tool call output, got: %q", text)
	}
	if !strings.Contains(text, "-> ok") {
		t.Fatalf("expected tool result output, got: %q", text)
	}
	if !strings.Contains(text, "done  2.1s  $0.0015") {
		t.Fatalf("expected completion output, got: %q", text)
	}
}

func TestRunLoopAddsBlankLineBetweenStructuredOutputAndText(t *testing.T) {
	var output strings.Builder

	loop := &runLoop{
		provider: claudeProvider{},
		now:      func() time.Time { return time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC) },
		sleep:    func(context.Context, time.Duration) error { return nil },
		out:      &output,
		runner: func(_ context.Context, _ string, _ []string, onLine func(string), _ io.Writer) error {
			onLine(`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"pwd"}}]}}`)
			onLine(`{"type":"stream_event","event":{"delta":{"type":"text_delta","text":"Done reading."}}}`)
			onLine(`{"type":"result","duration_ms":1000}`)
			return nil
		},
	}

	err := loop.Run(context.Background(), runLoopOptions{
		Prompt:      "inspect workspace",
		MaxRuns:     1,
		WaitSeconds: 0,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	text := output.String()
	if !strings.Contains(text, "- Bash(\"pwd\")\n\nDone reading.") {
		t.Fatalf("expected blank line between tool block and text, got: %q", text)
	}
}

func TestComposeRunPromptUsesBaseAndCycleSections(t *testing.T) {
	got := composeRunPrompt("chat with grace and coordinate", "Respond to unread chat from grace.")
	if !strings.Contains(got, "Primary mission:\nchat with grace and coordinate") {
		t.Fatalf("expected primary mission section, got %q", got)
	}
	if !strings.Contains(got, "Current cycle:\nRespond to unread chat from grace.") {
		t.Fatalf("expected current cycle section, got %q", got)
	}
}

func TestComposeRunPromptWithoutBaseUsesCycleOnly(t *testing.T) {
	got := composeRunPrompt("", "Respond to unread chat from grace.")
	if got != "Respond to unread chat from grace." {
		t.Fatalf("expected cycle-only prompt, got %q", got)
	}
}

func TestComposeRunPromptWithServicesAddsSection(t *testing.T) {
	got := composeRunPromptWithServices(
		"chat with grace and coordinate",
		"Respond to unread chat from grace.",
		[]runServiceConfig{
			{Name: "backend", Description: "Backend API server on http://localhost:8000"},
			{Name: "frontend", Command: "make run-frontend"},
		},
	)
	if !strings.Contains(got, "Services available:\n- backend: Backend API server on http://localhost:8000\n- frontend: make run-frontend") {
		t.Fatalf("expected services section, got %q", got)
	}
}

func TestRunLoopInitialPromptAppliesOnlyToFirstRun(t *testing.T) {
	provider := claudeProvider{}
	var commands [][]string

	loop := &runLoop{
		provider: provider,
		now:      func() time.Time { return time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC) },
		sleep:    func(context.Context, time.Duration) error { return nil },
		out:      io.Discard,
		runner: func(_ context.Context, _ string, argv []string, onLine func(string), _ io.Writer) error {
			commands = append(commands, append([]string(nil), argv...))
			onLine(`{"type":"result","duration_ms":1000}`)
			return nil
		},
	}

	err := loop.Run(context.Background(), runLoopOptions{
		InitialPrompt: "first-run mission",
		Prompt:        "persistent mission",
		MaxRuns:       2,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(commands))
	}
	first := strings.Join(commands[0], " ")
	second := strings.Join(commands[1], " ")
	if !strings.Contains(first, "first-run mission") {
		t.Fatalf("expected first run to use initial prompt, got %q", first)
	}
	if strings.Contains(first, "persistent mission") {
		t.Fatalf("expected first run not to use persistent base prompt, got %q", first)
	}
	if !strings.Contains(second, "persistent mission") {
		t.Fatalf("expected second run to use persistent base prompt, got %q", second)
	}
}

func TestRunLoopStopsAfterOneRunWhenOnlyInitialPromptExistsWithoutDispatch(t *testing.T) {
	var output strings.Builder
	runCount := 0

	loop := &runLoop{
		provider: claudeProvider{},
		now:      time.Now,
		out:      &output,
		sleep:    func(context.Context, time.Duration) error { return nil },
		runner: func(_ context.Context, _ string, _ []string, onLine func(string), _ io.Writer) error {
			runCount++
			onLine(`{"type":"result","duration_ms":1000}`)
			return nil
		},
	}

	err := loop.Run(context.Background(), runLoopOptions{
		InitialPrompt: "one-shot mission",
		WaitSeconds:   0,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if runCount != 1 {
		t.Fatalf("expected exactly one run, got %d", runCount)
	}
	if !strings.Contains(output.String(), "done: initial prompt consumed; use --base-prompt for a persistent mission.") {
		t.Fatalf("expected completion notice, got %q", output.String())
	}
}

func TestResolveRunMissionPromptPrefersOneRunOverride(t *testing.T) {
	got := resolveRunMissionPrompt("persistent mission", "one-run override")
	if got != "one-run override" {
		t.Fatalf("expected one-run override, got %q", got)
	}
}

func TestRunLoopIdleCountdown(t *testing.T) {
	var slept []time.Duration
	var output strings.Builder

	loop := &runLoop{
		provider: claudeProvider{},
		now:      time.Now,
		out:      &output,
		sleep: func(_ context.Context, d time.Duration) error {
			slept = append(slept, d)
			return nil
		},
		runner: func(_ context.Context, _ string, _ []string, onLine func(string), _ io.Writer) error {
			onLine(`{"type":"result","duration_ms":1000}`)
			return nil
		},
	}

	err := loop.Run(context.Background(), runLoopOptions{
		Prompt:      "keep going",
		WaitSeconds: 2,
		MaxRuns:     1,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(slept) != 0 {
		t.Fatalf("expected no idle sleep after final run, got %d calls", len(slept))
	}

	if err := loop.idle(context.Background(), 2); err != nil {
		t.Fatalf("idle returned error: %v", err)
	}
	if len(slept) != 2 {
		t.Fatalf("expected 2 sleep calls, got %d", len(slept))
	}
	if !strings.Contains(output.String(), "next run in 2s") {
		t.Fatalf("expected countdown output, got: %q", output.String())
	}
}

func TestRunLoopStopCancelsActiveRunAndPausesUntilResume(t *testing.T) {
	controller := newFakeRunInputController()
	runStarted := make(chan struct{})
	secondRunStarted := make(chan struct{})
	var mu sync.Mutex
	runCount := 0

	loop := &runLoop{
		provider: claudeProvider{},
		now:      time.Now,
		out:      io.Discard,
		sleep:    func(context.Context, time.Duration) error { return nil },
		control:  controller,
		runner: func(ctx context.Context, _ string, _ []string, onLine func(string), _ io.Writer) error {
			mu.Lock()
			runCount++
			currentRun := runCount
			mu.Unlock()

			if currentRun == 1 {
				close(runStarted)
				<-ctx.Done()
				return ctx.Err()
			}

			close(secondRunStarted)
			onLine(`{"type":"result","duration_ms":1000}`)
			return nil
		},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- loop.Run(context.Background(), runLoopOptions{
			Prompt:      "keep going",
			WaitSeconds: 0,
			MaxRuns:     2,
		})
	}()

	select {
	case <-runStarted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for run to start")
	}

	controller.send(runControlEvent{Type: runControlStop})

	select {
	case <-secondRunStarted:
		t.Fatal("second run should not start before /resume after /stop")
	case <-time.After(150 * time.Millisecond):
	}

	select {
	case err := <-errCh:
		t.Fatalf("loop should stay alive after /stop, got %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	controller.send(runControlEvent{Type: runControlResume})

	select {
	case <-secondRunStarted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for second run after /resume")
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected graceful completion, got: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for loop to finish")
	}
}

func TestRunLoopStopTreatsCanceledProcessExitAsNonFatal(t *testing.T) {
	controller := newFakeRunInputController()
	runStarted := make(chan struct{})
	secondRunStarted := make(chan struct{})
	var mu sync.Mutex
	runCount := 0

	loop := &runLoop{
		provider: claudeProvider{},
		now:      time.Now,
		out:      io.Discard,
		sleep:    func(context.Context, time.Duration) error { return nil },
		control:  controller,
		runner: func(ctx context.Context, _ string, _ []string, onLine func(string), _ io.Writer) error {
			mu.Lock()
			runCount++
			currentRun := runCount
			mu.Unlock()

			if currentRun == 1 {
				close(runStarted)
				<-ctx.Done()
				return errors.New("signal: killed")
			}

			close(secondRunStarted)
			onLine(`{"type":"result","duration_ms":1000}`)
			return nil
		},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- loop.Run(context.Background(), runLoopOptions{
			Prompt:      "keep going",
			WaitSeconds: 0,
			MaxRuns:     2,
		})
	}()

	select {
	case <-runStarted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for run to start")
	}

	controller.send(runControlEvent{Type: runControlStop})

	select {
	case <-secondRunStarted:
		t.Fatal("second run should not start before /resume after /stop")
	case <-time.After(150 * time.Millisecond):
	}

	select {
	case err := <-errCh:
		t.Fatalf("loop should stay alive after canceled process exit, got %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	controller.send(runControlEvent{Type: runControlResume})

	select {
	case <-secondRunStarted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for second run after /resume")
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected graceful completion, got: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for loop to finish")
	}
}

func TestRunLoopStopPrintsSinglePauseNotice(t *testing.T) {
	controller := newFakeRunInputController()
	runStarted := make(chan struct{})
	var output strings.Builder

	loop := &runLoop{
		provider: claudeProvider{},
		now:      time.Now,
		out:      &output,
		sleep:    func(context.Context, time.Duration) error { return nil },
		control:  controller,
		runner: func(ctx context.Context, _ string, _ []string, onLine func(string), _ io.Writer) error {
			close(runStarted)
			<-ctx.Done()
			return ctx.Err()
		},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- loop.Run(context.Background(), runLoopOptions{
			Prompt:      "keep going",
			WaitSeconds: 0,
			MaxRuns:     1,
		})
	}()

	select {
	case <-runStarted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for run to start")
	}

	controller.send(runControlEvent{Type: runControlStop})

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected graceful stop handling, got %v", err)
		}
	case <-time.After(200 * time.Millisecond):
	}

	text := output.String()
	if strings.Count(text, "paused. use /resume, /quit, or type a prompt to continue.") != 1 {
		t.Fatalf("expected exactly one pause notice after /stop, got %q", text)
	}
}

func TestApplyControlEventStopLatchesPauseAfterRunDuringActiveRun(t *testing.T) {
	loop := &runLoop{out: io.Discard}
	state := &runState{}
	canceled := false

	loop.applyControlEvent(runControlEvent{Type: runControlStop}, state, true, func() {
		canceled = true
	})

	if !canceled {
		t.Fatal("expected active /stop to cancel the run")
	}
	if !state.RunInterrupted {
		t.Fatal("expected /stop to mark the run as interrupted")
	}
	if !state.Paused {
		t.Fatal("expected /stop to leave the loop paused")
	}
	if !state.PauseAfterRun {
		t.Fatal("expected /stop to latch pause-after-run across the cancellation boundary")
	}
}

func TestApplyControlEventInterruptClearsPendingInput(t *testing.T) {
	screen := &runScreenManager{promptLabel: defaultRunInputPromptLabel}
	loop := &runLoop{screen: screen}
	state := &runState{
		PendingInput: true,
		InputBuffer:  "draft",
	}
	canceled := false

	loop.applyControlEvent(runControlEvent{Type: runControlInterrupt}, state, true, func() {
		canceled = true
	})

	if canceled {
		t.Fatal("expected ctrl-c with pending input to clear the buffer, not cancel the run")
	}
	if state.PendingInput {
		t.Fatal("expected pending input to clear")
	}
	if state.InputBuffer != "" {
		t.Fatalf("expected input buffer to clear, got %q", state.InputBuffer)
	}
	if screen.inputLine != defaultRunInputPromptLabel {
		t.Fatalf("expected cleared screen input line, got %q", screen.inputLine)
	}
}

func TestApplyControlEventInterruptStopsActiveRunWithoutInput(t *testing.T) {
	loop := &runLoop{out: io.Discard}
	state := &runState{}
	canceled := false

	loop.applyControlEvent(runControlEvent{Type: runControlInterrupt}, state, true, func() {
		canceled = true
	})

	if !canceled {
		t.Fatal("expected ctrl-c during active run to stop the run")
	}
	if !state.RunInterrupted {
		t.Fatal("expected ctrl-c during active run to mark the run interrupted")
	}
}

func TestApplyControlEventInterruptOffersExitWhenIdle(t *testing.T) {
	screen := &runScreenManager{promptLabel: defaultRunInputPromptLabel}
	loop := &runLoop{screen: screen}
	state := &runState{}

	loop.applyControlEvent(runControlEvent{Type: runControlInterrupt}, state, false, nil)

	if !state.ExitConfirmPending {
		t.Fatal("expected ctrl-c with no input to offer exit")
	}
	if !screen.exitConfirm {
		t.Fatal("expected screen to enter exit confirmation mode")
	}
}

func TestApplyControlEventInterruptConfirmsExitWhenAlreadyPrompting(t *testing.T) {
	screen := &runScreenManager{promptLabel: defaultRunInputPromptLabel}
	loop := &runLoop{screen: screen}
	state := &runState{ExitConfirmPending: true}

	loop.applyControlEvent(runControlEvent{Type: runControlInterrupt}, state, false, nil)

	if !state.StopRequested {
		t.Fatal("expected second ctrl-c to confirm exit")
	}
	if screen.exitConfirm {
		t.Fatal("expected exit confirmation mode to clear after confirming")
	}
}

func TestApplyControlEventExitPromptOffersThenConfirmsExit(t *testing.T) {
	screen := &runScreenManager{promptLabel: defaultRunInputPromptLabel}
	loop := &runLoop{screen: screen}
	state := &runState{
		PendingInput: true,
		InputBuffer:  "draft",
	}

	loop.applyControlEvent(runControlEvent{Type: runControlExitPrompt}, state, false, nil)
	if !state.ExitConfirmPending {
		t.Fatal("expected ctrl-d to offer exit")
	}
	if state.InputBuffer != "draft" {
		t.Fatalf("expected ctrl-d to preserve input, got %q", state.InputBuffer)
	}

	loop.applyControlEvent(runControlEvent{Type: runControlExitConfirm}, state, false, nil)
	if !state.StopRequested {
		t.Fatal("expected confirmed exit to stop the loop")
	}
}

func TestApplyControlEventExitPromptDoesNotStopActiveRunUntilConfirmed(t *testing.T) {
	loop := &runLoop{out: io.Discard}
	state := &runState{}
	canceled := false

	loop.applyControlEvent(runControlEvent{Type: runControlExitPrompt}, state, true, func() {
		canceled = true
	})

	if canceled {
		t.Fatal("expected first ctrl-d to offer exit, not cancel the run")
	}
	if !state.ExitConfirmPending {
		t.Fatal("expected exit confirmation to be pending")
	}
}

func TestRunLoopQuitCancelsActiveRunAndExits(t *testing.T) {
	controller := newFakeRunInputController()
	runStarted := make(chan struct{})

	loop := &runLoop{
		provider: claudeProvider{},
		now:      time.Now,
		out:      io.Discard,
		sleep:    func(context.Context, time.Duration) error { return nil },
		control:  controller,
		runner: func(ctx context.Context, _ string, _ []string, _ func(string), _ io.Writer) error {
			close(runStarted)
			<-ctx.Done()
			return ctx.Err()
		},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- loop.Run(context.Background(), runLoopOptions{Prompt: "keep going"})
	}()

	select {
	case <-runStarted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for run to start")
	}

	controller.send(runControlEvent{Type: runControlQuit})

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected graceful quit, got: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for loop to quit")
	}
}

func TestRunLoopWaitPausesUntilResume(t *testing.T) {
	controller := newFakeRunInputController()
	firstRunStarted := make(chan struct{})
	releaseFirstRun := make(chan struct{})
	secondRunStarted := make(chan struct{})
	var mu sync.Mutex
	runCount := 0

	loop := &runLoop{
		provider: claudeProvider{},
		now:      time.Now,
		out:      io.Discard,
		sleep:    func(context.Context, time.Duration) error { return nil },
		control:  controller,
		runner: func(_ context.Context, _ string, _ []string, onLine func(string), _ io.Writer) error {
			mu.Lock()
			runCount++
			currentRun := runCount
			mu.Unlock()

			if currentRun == 1 {
				close(firstRunStarted)
				<-releaseFirstRun
			} else if currentRun == 2 {
				close(secondRunStarted)
			}

			onLine(`{"type":"result","duration_ms":1000}`)
			return nil
		},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- loop.Run(context.Background(), runLoopOptions{
			Prompt:      "keep going",
			WaitSeconds: 0,
			MaxRuns:     2,
		})
	}()

	select {
	case <-firstRunStarted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for first run")
	}

	controller.send(runControlEvent{Type: runControlWait})
	close(releaseFirstRun)

	select {
	case <-secondRunStarted:
		t.Fatal("second run should not start before /resume")
	case <-time.After(150 * time.Millisecond):
	}

	controller.send(runControlEvent{Type: runControlResume})

	select {
	case <-secondRunStarted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for second run after /resume")
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for loop to finish")
	}
}

func TestRunLoopPromptOverrideFromActiveRun(t *testing.T) {
	controller := newFakeRunInputController()
	firstRunStarted := make(chan struct{})
	releaseFirstRun := make(chan struct{})
	var commands [][]string
	var mu sync.Mutex
	runCount := 0

	loop := &runLoop{
		provider: claudeProvider{},
		now:      time.Now,
		out:      io.Discard,
		sleep:    func(context.Context, time.Duration) error { return nil },
		control:  controller,
		runner: func(_ context.Context, _ string, argv []string, onLine func(string), _ io.Writer) error {
			mu.Lock()
			runCount++
			currentRun := runCount
			commands = append(commands, append([]string(nil), argv...))
			mu.Unlock()

			if currentRun == 1 {
				close(firstRunStarted)
				<-releaseFirstRun
			}

			onLine(`{"type":"result","duration_ms":1000}`)
			return nil
		},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- loop.Run(context.Background(), runLoopOptions{
			Prompt:      "base prompt",
			WaitSeconds: 0,
			MaxRuns:     2,
		})
	}()

	select {
	case <-firstRunStarted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for first run")
	}

	controller.send(runControlEvent{Type: runControlTypingStarted})
	controller.send(runControlEvent{Type: runControlPrompt, Text: "override prompt"})
	close(releaseFirstRun)

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("timed out waiting for loop to finish")
	}

	if len(commands) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(commands))
	}
	if !strings.Contains(strings.Join(commands[1], " "), "override prompt") {
		t.Fatalf("expected override prompt on second run, got %q", strings.Join(commands[1], " "))
	}
}

func TestRunLoopPromptOverrideForcesRunWhenDispatchWouldSkip(t *testing.T) {
	controller := newFakeRunInputController()
	firstRunStarted := make(chan struct{})
	releaseFirstRun := make(chan struct{})
	var commands [][]string
	var mu sync.Mutex
	runCount := 0

	dispatcher := &fakeRunDispatcher{
		decisions: []runDispatchDecision{
			{Prompt: "claimed work", WaitSeconds: 5},
			{Skip: true, WaitSeconds: 30},
		},
	}

	loop := &runLoop{
		provider: claudeProvider{},
		now:      time.Now,
		out:      io.Discard,
		sleep:    func(context.Context, time.Duration) error { return nil },
		control:  controller,
		dispatch: dispatcher,
		runner: func(_ context.Context, _ string, argv []string, onLine func(string), _ io.Writer) error {
			mu.Lock()
			runCount++
			currentRun := runCount
			commands = append(commands, append([]string(nil), argv...))
			mu.Unlock()

			if currentRun == 1 {
				close(firstRunStarted)
				<-releaseFirstRun
			}

			onLine(`{"type":"result","duration_ms":1000}`)
			return nil
		},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- loop.Run(context.Background(), runLoopOptions{
			Prompt:      "persistent mission",
			WaitSeconds: 0,
			MaxRuns:     2,
		})
	}()

	select {
	case <-firstRunStarted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for first run")
	}

	controller.send(runControlEvent{Type: runControlPrompt, Text: "one-run override"})
	close(releaseFirstRun)

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("timed out waiting for loop to finish")
	}

	if len(commands) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(commands))
	}
	if !containsText(joinArgs(commands[1]), "one-run override") {
		t.Fatalf("expected one-run override on forced run, got %q", joinArgs(commands[1]))
	}
	if containsText(joinArgs(commands[1]), "persistent mission") {
		t.Fatalf("expected override to replace persistent mission for one run, got %q", joinArgs(commands[1]))
	}
}

func TestRunLoopPromptOverrideBypassesDispatchCyclePrompt(t *testing.T) {
	controller := newFakeRunInputController()
	firstRunStarted := make(chan struct{})
	releaseFirstRun := make(chan struct{})
	var commands [][]string
	var mu sync.Mutex
	runCount := 0

	dispatcher := &fakeRunDispatcher{
		decisions: []runDispatchDecision{
			{Prompt: "claimed work", WaitSeconds: 5},
			{Prompt: "Pick up beadhub-018 and work on it.", WaitSeconds: 5},
		},
	}

	loop := &runLoop{
		provider: claudeProvider{},
		now:      time.Now,
		out:      io.Discard,
		sleep:    func(context.Context, time.Duration) error { return nil },
		control:  controller,
		dispatch: dispatcher,
		runner: func(_ context.Context, _ string, argv []string, onLine func(string), _ io.Writer) error {
			mu.Lock()
			runCount++
			currentRun := runCount
			commands = append(commands, append([]string(nil), argv...))
			mu.Unlock()

			if currentRun == 1 {
				close(firstRunStarted)
				<-releaseFirstRun
			}

			onLine(`{"type":"result","duration_ms":1000}`)
			return nil
		},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- loop.Run(context.Background(), runLoopOptions{
			Prompt:      "persistent mission",
			WaitSeconds: 0,
			MaxRuns:     2,
		})
	}()

	select {
	case <-firstRunStarted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for first run")
	}

	controller.send(runControlEvent{Type: runControlPrompt, Text: "are we ready to release"})
	close(releaseFirstRun)

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("timed out waiting for loop to finish")
	}

	if len(commands) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(commands))
	}
	second := joinArgs(commands[1])
	if !containsText(second, "are we ready to release") {
		t.Fatalf("expected queued user prompt on second run, got %q", second)
	}
	if containsText(second, "Pick up beadhub-018 and work on it.") {
		t.Fatalf("expected queued user prompt to bypass dispatch cycle prompt, got %q", second)
	}
}

func TestRunLoopInitialPromptForcesRunWhenDispatchWouldSkip(t *testing.T) {
	dispatcher := &fakeRunDispatcher{
		decisions: []runDispatchDecision{
			{Skip: true, WaitSeconds: 30},
		},
	}
	var commands [][]string

	loop := &runLoop{
		provider: claudeProvider{},
		now:      time.Now,
		out:      io.Discard,
		sleep:    func(context.Context, time.Duration) error { return nil },
		dispatch: dispatcher,
		runner: func(_ context.Context, _ string, argv []string, onLine func(string), _ io.Writer) error {
			commands = append(commands, append([]string(nil), argv...))
			onLine(`{"type":"result","duration_ms":1000}`)
			return nil
		},
	}

	err := loop.Run(context.Background(), runLoopOptions{
		InitialPrompt: "are we ready to release",
		WaitSeconds:   0,
		MaxRuns:       1,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("expected initial prompt to force one run, got %d runs", len(commands))
	}
	if !containsText(joinArgs(commands[0]), "are we ready to release") {
		t.Fatalf("expected initial prompt in forced run, got %q", joinArgs(commands[0]))
	}
}

func TestRunLoopInitialPromptBypassesDispatchCyclePrompt(t *testing.T) {
	dispatcher := &fakeRunDispatcher{
		decisions: []runDispatchDecision{
			{Prompt: "Pick up beadhub-018 and work on it.", WaitSeconds: 30},
		},
	}
	var commands [][]string

	loop := &runLoop{
		provider: claudeProvider{},
		now:      time.Now,
		out:      io.Discard,
		sleep:    func(context.Context, time.Duration) error { return nil },
		dispatch: dispatcher,
		runner: func(_ context.Context, _ string, argv []string, onLine func(string), _ io.Writer) error {
			commands = append(commands, append([]string(nil), argv...))
			onLine(`{"type":"result","duration_ms":1000}`)
			return nil
		},
	}

	err := loop.Run(context.Background(), runLoopOptions{
		InitialPrompt: "are we ready to release",
		WaitSeconds:   0,
		MaxRuns:       1,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("expected initial prompt to force one run, got %d runs", len(commands))
	}
	first := joinArgs(commands[0])
	if !containsText(first, "are we ready to release") {
		t.Fatalf("expected initial prompt in forced run, got %q", first)
	}
	if containsText(first, "Pick up beadhub-018 and work on it.") {
		t.Fatalf("expected initial prompt to bypass dispatch cycle prompt, got %q", first)
	}
}

func TestRunLoopTypingDuringActiveRunDoesNotPauseLoop(t *testing.T) {
	controller := newFakeRunInputController()
	firstRunStarted := make(chan struct{})
	releaseFirstRun := make(chan struct{})
	secondRunStarted := make(chan struct{})
	var mu sync.Mutex
	runCount := 0

	loop := &runLoop{
		provider: claudeProvider{},
		now:      time.Now,
		out:      io.Discard,
		sleep:    func(context.Context, time.Duration) error { return nil },
		control:  controller,
		runner: func(_ context.Context, _ string, _ []string, onLine func(string), _ io.Writer) error {
			mu.Lock()
			runCount++
			currentRun := runCount
			mu.Unlock()

			if currentRun == 1 {
				close(firstRunStarted)
				<-releaseFirstRun
			} else if currentRun == 2 {
				close(secondRunStarted)
			}

			onLine(`{"type":"result","duration_ms":1000}`)
			return nil
		},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- loop.Run(context.Background(), runLoopOptions{
			Prompt:      "keep going",
			WaitSeconds: 0,
			MaxRuns:     2,
		})
	}()

	select {
	case <-firstRunStarted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for first run")
	}

	controller.send(runControlEvent{Type: runControlTypingStarted})
	controller.send(runControlEvent{Type: runControlBufferUpdated, Text: "draft input"})
	close(releaseFirstRun)

	select {
	case <-secondRunStarted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("typing should not pause the loop after the current run")
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for loop to finish")
	}
}

func TestIdleWithControlsTypingPausesCountdownUntilResume(t *testing.T) {
	controller := newFakeRunInputController()
	sleepStarted := make(chan struct{}, 1)
	releaseSleep := make(chan struct{})
	done := make(chan error, 1)

	loop := &runLoop{
		out:     io.Discard,
		control: controller,
		sleep: func(_ context.Context, _ time.Duration) error {
			select {
			case sleepStarted <- struct{}{}:
			default:
			}
			<-releaseSleep
			return nil
		},
	}

	state := &runState{}
	go func() {
		done <- loop.idleWithControlsLabel(context.Background(), 2, state, "next run")
	}()

	select {
	case <-sleepStarted:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for countdown sleep to start")
	}

	controller.send(runControlEvent{Type: runControlTypingStarted})
	controller.send(runControlEvent{Type: runControlBufferUpdated, Text: "draft"})
	close(releaseSleep)

	select {
	case err := <-done:
		t.Fatalf("countdown should pause instead of completing, got %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	if !state.Paused {
		t.Fatal("expected typing during countdown to pause the loop")
	}

	controller.send(runControlEvent{Type: runControlResume})

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected countdown to resume cleanly, got %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for countdown to finish after /resume")
	}
}

func TestRunLoopPropagatesRunnerError(t *testing.T) {
	expected := errors.New("runner failed")

	loop := &runLoop{
		provider: claudeProvider{},
		now:      time.Now,
		out:      io.Discard,
		sleep:    func(context.Context, time.Duration) error { return nil },
		runner: func(_ context.Context, _ string, _ []string, _ func(string), _ io.Writer) error {
			return expected
		},
	}

	err := loop.Run(context.Background(), runLoopOptions{
		Prompt:  "keep going",
		MaxRuns: 1,
	})
	if !errors.Is(err, expected) {
		t.Fatalf("expected runner error, got: %v", err)
	}
}

func TestRunLoopPrefersProviderStructuredErrorOverRawExitStatus(t *testing.T) {
	loop := &runLoop{
		provider: claudeProvider{},
		now:      time.Now,
		out:      io.Discard,
		sleep:    func(context.Context, time.Duration) error { return nil },
		runner: func(_ context.Context, _ string, _ []string, onLine func(string), _ io.Writer) error {
			onLine(`{"type":"result","duration_ms":320,"session_id":"s1","is_error":true,"result":"You've hit your limit"}`)
			return errors.New("exit status 1")
		},
	}

	err := loop.Run(context.Background(), runLoopOptions{
		Prompt:  "keep going",
		MaxRuns: 1,
	})
	if err == nil {
		t.Fatal("expected run to fail")
	}
	if err.Error() != "You've hit your limit" {
		t.Fatalf("expected structured provider error, got %q", err)
	}
}

func TestRunLoopAutoCompactsBeforeWaitWhenUsageExceedsThreshold(t *testing.T) {
	var commands [][]string
	sleepCalls := 0

	loop := &runLoop{
		provider: claudeProvider{},
		now:      time.Now,
		out:      io.Discard,
		sleep: func(context.Context, time.Duration) error {
			sleepCalls++
			return nil
		},
		runner: func(_ context.Context, _ string, argv []string, onLine func(string), _ io.Writer) error {
			commands = append(commands, append([]string(nil), argv...))
			if len(commands) == 1 {
				onLine(`{"type":"assistant","message":{"model":"claude-opus-4-6","usage":{"input_tokens":170000,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":800},"content":[]}}`)
			}
			onLine(`{"type":"result","duration_ms":1000,"session_id":"s1"}`)
			return nil
		},
	}

	err := loop.Run(context.Background(), runLoopOptions{
		Prompt:              "keep going",
		WaitSeconds:         2,
		MaxRuns:             1,
		CompactThresholdPct: 80,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(commands) != 2 {
		t.Fatalf("expected work run plus compact run, got %d commands", len(commands))
	}
	if containsText(joinArgs(commands[0]), "/compact") {
		t.Fatalf("expected first run to be normal work, got %q", joinArgs(commands[0]))
	}
	if !containsText(joinArgs(commands[1]), "/compact") {
		t.Fatalf("expected second run to be compact, got %q", joinArgs(commands[1]))
	}
	if sleepCalls != 0 {
		t.Fatalf("expected compact to happen before any wait, got %d sleep calls", sleepCalls)
	}
}

func TestRunLoopAutoCompactReplacesWaitBeforeNextCycle(t *testing.T) {
	var commands [][]string
	sleepCalls := 0

	loop := &runLoop{
		provider: claudeProvider{},
		now:      time.Now,
		out:      io.Discard,
		sleep: func(context.Context, time.Duration) error {
			sleepCalls++
			return nil
		},
		runner: func(_ context.Context, _ string, argv []string, onLine func(string), _ io.Writer) error {
			commands = append(commands, append([]string(nil), argv...))
			switch len(commands) {
			case 1:
				onLine(`{"type":"assistant","message":{"model":"claude-opus-4-6","usage":{"input_tokens":170000,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":800},"content":[]}}`)
			case 2:
				onLine(`{"type":"assistant","message":{"model":"claude-opus-4-6","usage":{"input_tokens":1000,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":10},"content":[]}}`)
			}
			onLine(`{"type":"result","duration_ms":1000,"session_id":"s1"}`)
			return nil
		},
	}

	err := loop.Run(context.Background(), runLoopOptions{
		Prompt:              "keep going",
		WaitSeconds:         2,
		MaxRuns:             2,
		CompactThresholdPct: 80,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(commands) != 3 {
		t.Fatalf("expected work, compact, work; got %d commands", len(commands))
	}
	if !containsText(joinArgs(commands[1]), "/compact") {
		t.Fatalf("expected compact command second, got %q", joinArgs(commands[1]))
	}
	if containsText(joinArgs(commands[2]), "/compact") {
		t.Fatalf("expected third command to be next work cycle, got %q", joinArgs(commands[2]))
	}
	if sleepCalls != 0 {
		t.Fatalf("expected auto-compact to replace wait before next cycle, got %d sleep calls", sleepCalls)
	}
}

func TestApplyControlEvent_BufferUpdatedRendersInputPrompt(t *testing.T) {
	var output strings.Builder
	loop := &runLoop{out: &output}
	state := &runState{Paused: true}

	loop.applyControlEvent(runControlEvent{Type: runControlBufferUpdated, Text: "/wait"}, state, false, nil)

	if state.InputBuffer != "/wait" {
		t.Fatalf("expected input buffer to update, got %q", state.InputBuffer)
	}
	if !containsText(output.String(), defaultRunInputPromptLabel+"/wait") {
		t.Fatalf("expected rendered input prompt, got %q", output.String())
	}
}

func TestApplyControlEvent_BufferUpdatedRendersInputPromptOnScreen(t *testing.T) {
	screen := &runScreenManager{promptLabel: defaultRunInputPromptLabel}
	loop := &runLoop{screen: screen}
	state := &runState{Paused: true}

	loop.applyControlEvent(runControlEvent{Type: runControlBufferUpdated, Text: "/wait"}, state, false, nil)

	if screen.inputLine != defaultRunInputPromptLabel+"/wait" {
		t.Fatalf("expected screen input line, got %q", screen.inputLine)
	}
}

func TestApplyControlEvent_BufferUpdatedPreservesLeadingSpace(t *testing.T) {
	screen := &runScreenManager{promptLabel: defaultRunInputPromptLabel}
	loop := &runLoop{screen: screen}
	state := &runState{Paused: true}

	loop.applyControlEvent(runControlEvent{Type: runControlBufferUpdated, Text: " leading"}, state, false, nil)

	if !state.PendingInput {
		t.Fatal("expected leading-space input to remain pending")
	}
	if screen.inputLine != defaultRunInputPromptLabel+" leading" {
		t.Fatalf("expected screen input line to preserve leading space, got %q", screen.inputLine)
	}
}

func TestShouldSuppressText_PromptEcho(t *testing.T) {
	var output strings.Builder
	loop := &runLoop{out: &output}
	state := &runState{}

	suppressed := loop.shouldSuppressText(state, strings.Repeat("intro ", 40)+" BeadHub Coordination Rules "+"bdh :policy")
	if !suppressed {
		t.Fatal("expected boilerplate text to be suppressed")
	}
	if !state.SuppressText {
		t.Fatal("expected suppression flag to be set")
	}
	if !containsText(output.String(), "[suppressed prompt/policy echo]") {
		t.Fatalf("expected suppression notice, got %q", output.String())
	}
}

func TestRenderIdleLineUsesStatusAreaOnScreen(t *testing.T) {
	screen := &runScreenManager{promptLabel: defaultRunInputPromptLabel}
	loop := &runLoop{screen: screen}
	state := &runState{
		PendingInput: true,
		InputBuffer:  "/resume soon",
	}

	loop.renderIdleLine("next run", 12, state)

	if screen.statusLine != "next run in 12s" {
		t.Fatalf("expected status line to hold countdown, got %q", screen.statusLine)
	}
	if screen.inputLine != defaultRunInputPromptLabel+"/resume soon" {
		t.Fatalf("expected input line to remain separate, got %q", screen.inputLine)
	}
}

func TestRenderInputPromptDoesNotRewriteActiveScreenInput(t *testing.T) {
	screen := &runScreenManager{
		promptLabel: "beadhub:bdh:noah> ",
		inputLine:   "beadhub:bdh:noah> typed text",
		program:     &tea.Program{},
	}
	loop := &runLoop{
		screen:           screen,
		inputPromptLabel: "beadhub:bdh:noah> ",
	}

	loop.renderInputPrompt(&runState{
		PendingInput: true,
		InputBuffer:  "stale text",
	})

	if screen.inputLine != "beadhub:bdh:noah> typed text" {
		t.Fatalf("expected active screen input to remain untouched, got %q", screen.inputLine)
	}
}

func TestRenderIdleLineWaitingForWorkUsesStatusAreaOnScreen(t *testing.T) {
	screen := &runScreenManager{}
	loop := &runLoop{screen: screen}

	loop.renderIdleLine("waiting for work", 30, &runState{})

	if screen.statusLine != "waiting for work in 30s" {
		t.Fatalf("expected waiting status line, got %q", screen.statusLine)
	}
}

func joinArgs(args []string) string {
	return strings.Join(args, " ")
}

func containsText(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
