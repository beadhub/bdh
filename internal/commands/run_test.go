package commands

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type noResumeProvider struct{ claudeProvider }

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

func TestRunLoopUsesResumeOnSecondRun(t *testing.T) {
	provider := claudeProvider{}
	var commands [][]string
	runCount := 0

	loop := &runLoop{
		provider: provider,
		now:      func() time.Time { return time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC) },
		sleep:    func(context.Context, time.Duration) error { return nil },
		out:      io.Discard,
		runner: func(_ context.Context, _ string, argv []string, onLine func(string)) error {
			commands = append(commands, append([]string(nil), argv...))
			runCount++
			if runCount == 1 {
				onLine(`{"type":"result","duration_ms":1000,"session_id":"sess-42"}`)
				return nil
			}
			onLine(`{"type":"result","duration_ms":1000,"session_id":"sess-42"}`)
			return nil
		},
	}

	err := loop.Run(context.Background(), runLoopOptions{
		Prompt:      "continue working",
		SessionMode: true,
		MaxRuns:     2,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(commands))
	}
	if strings.Contains(strings.Join(commands[0], " "), "--resume") {
		t.Fatalf("first run should not resume: %q", strings.Join(commands[0], " "))
	}
	if !strings.Contains(strings.Join(commands[1], " "), "--resume sess-42") {
		t.Fatalf("second run should resume previous session: %q", strings.Join(commands[1], " "))
	}
}

func TestRunLoopFallsBackToContinueWithoutSessionID(t *testing.T) {
	provider := noResumeProvider{}
	var commands [][]string

	loop := &runLoop{
		provider: provider,
		now:      func() time.Time { return time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC) },
		sleep:    func(context.Context, time.Duration) error { return nil },
		out:      io.Discard,
		runner: func(_ context.Context, _ string, argv []string, onLine func(string)) error {
			commands = append(commands, append([]string(nil), argv...))
			onLine(`{"type":"result","duration_ms":1000}`)
			return nil
		},
	}

	err := loop.Run(context.Background(), runLoopOptions{
		Prompt:      "continue working",
		SessionMode: true,
		MaxRuns:     2,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(commands))
	}
	if !strings.Contains(strings.Join(commands[1], " "), "--continue") {
		t.Fatalf("second run should use --continue fallback: %q", strings.Join(commands[1], " "))
	}
}

func (noResumeProvider) Capabilities() runProviderCapabilities {
	return runProviderCapabilities{
		SupportsResume:   false,
		SupportsContinue: true,
	}
}

func TestRunLoopFormatsOutput(t *testing.T) {
	var output strings.Builder

	loop := &runLoop{
		provider: claudeProvider{},
		now:      func() time.Time { return time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC) },
		sleep:    func(context.Context, time.Duration) error { return nil },
		out:      &output,
		runner: func(_ context.Context, _ string, _ []string, onLine func(string)) error {
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
	if !strings.Contains(text, "tool: exec_command  cmd=\"pwd\"") {
		t.Fatalf("expected tool call output, got: %q", text)
	}
	if !strings.Contains(text, "-> ok") {
		t.Fatalf("expected tool result output, got: %q", text)
	}
	if !strings.Contains(text, "done  2.1s  $0.0015") {
		t.Fatalf("expected completion output, got: %q", text)
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
		runner: func(_ context.Context, _ string, _ []string, onLine func(string)) error {
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
		runner: func(ctx context.Context, _ string, _ []string, onLine func(string)) error {
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

func TestRunLoopQuitCancelsActiveRunAndExits(t *testing.T) {
	controller := newFakeRunInputController()
	runStarted := make(chan struct{})

	loop := &runLoop{
		provider: claudeProvider{},
		now:      time.Now,
		out:      io.Discard,
		sleep:    func(context.Context, time.Duration) error { return nil },
		control:  controller,
		runner: func(ctx context.Context, _ string, _ []string, _ func(string)) error {
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
		runner: func(_ context.Context, _ string, _ []string, onLine func(string)) error {
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
		runner: func(_ context.Context, _ string, argv []string, onLine func(string)) error {
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
		runner: func(_ context.Context, _ string, _ []string, onLine func(string)) error {
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

func TestRunLoopPropagatesRunnerError(t *testing.T) {
	expected := errors.New("runner failed")

	loop := &runLoop{
		provider: claudeProvider{},
		now:      time.Now,
		out:      io.Discard,
		sleep:    func(context.Context, time.Duration) error { return nil },
		runner: func(_ context.Context, _ string, _ []string, _ func(string)) error {
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

func TestApplyControlEvent_BufferUpdatedRendersInputPrompt(t *testing.T) {
	var output strings.Builder
	loop := &runLoop{out: &output}
	state := &runState{Paused: true}

	loop.applyControlEvent(runControlEvent{Type: runControlBufferUpdated, Text: "/wait"}, state, false, nil)

	if state.InputBuffer != "/wait" {
		t.Fatalf("expected input buffer to update, got %q", state.InputBuffer)
	}
	if !containsText(output.String(), "input> /wait") {
		t.Fatalf("expected rendered input prompt, got %q", output.String())
	}
}

func TestApplyControlEvent_BufferUpdatedRendersInputPromptOnScreen(t *testing.T) {
	screen := &runScreenManager{}
	loop := &runLoop{screen: screen}
	state := &runState{Paused: true}

	loop.applyControlEvent(runControlEvent{Type: runControlBufferUpdated, Text: "/wait"}, state, false, nil)

	if screen.inputLine != "input> /wait" {
		t.Fatalf("expected screen input line, got %q", screen.inputLine)
	}
}

func TestApplyControlEvent_BufferUpdatedPreservesLeadingSpace(t *testing.T) {
	screen := &runScreenManager{}
	loop := &runLoop{screen: screen}
	state := &runState{Paused: true}

	loop.applyControlEvent(runControlEvent{Type: runControlBufferUpdated, Text: " leading"}, state, false, nil)

	if !state.PendingInput {
		t.Fatal("expected leading-space input to remain pending")
	}
	if screen.inputLine != "input>  leading" {
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
	screen := &runScreenManager{}
	loop := &runLoop{screen: screen}
	state := &runState{
		PendingInput: true,
		InputBuffer:  "/resume soon",
	}

	loop.renderIdleLine(12, state)

	if screen.statusLine != "next run in 12s" {
		t.Fatalf("expected status line to hold countdown, got %q", screen.statusLine)
	}
	if screen.inputLine != "input> /resume soon" {
		t.Fatalf("expected input line to remain separate, got %q", screen.inputLine)
	}
}

func joinArgs(args []string) string {
	return strings.Join(args, " ")
}

func containsText(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
