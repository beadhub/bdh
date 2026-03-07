package commands

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type fakeRunDispatcher struct {
	decisions []runDispatchDecision
	err       error
}

func (f *fakeRunDispatcher) Next(context.Context) (runDispatchDecision, error) {
	if f.err != nil {
		return runDispatchDecision{}, f.err
	}
	if len(f.decisions) == 0 {
		return runDispatchDecision{}, errors.New("no dispatch decisions left")
	}
	decision := f.decisions[0]
	f.decisions = f.decisions[1:]
	return decision, nil
}

func TestSelectRunDispatchPriority(t *testing.T) {
	defaults := withRunDispatchDefaults(runDispatchDefaults{})
	claim := ClaimInfo{BeadID: "bdh-421.3", Title: "Dispatch logic"}
	task := runReadyTask{ID: "bdh-54y", Title: "List active"}

	chatDecision := selectRunDispatch(runDispatchSummary{
		PendingChat: &runPendingChat{
			Alias: "grace",
			Messages: []runCommsMessage{
				{From: "grace", Body: "Please validate the latest run behavior."},
			},
		},
		UnreadMail:   []runUnreadMail{{From: "mia", Subject: "triage", Body: "mail body"}},
		CurrentClaim: &claim,
		ReadyTask:    &task,
	}, defaults)
	if !containsText(chatDecision.Prompt, "Respond to chat from grace") {
		t.Fatalf("expected chat prompt, got: %q", chatDecision.Prompt)
	}
	if !containsText(chatDecision.Prompt, "Please validate the latest run behavior.") {
		t.Fatalf("expected chat content in prompt, got: %q", chatDecision.Prompt)
	}
	if chatDecision.WaitSeconds != 5 {
		t.Fatalf("expected short wait for chat, got %d", chatDecision.WaitSeconds)
	}

	mailDecision := selectRunDispatch(runDispatchSummary{
		UnreadMail: []runUnreadMail{
			{From: "mia", Subject: "Review", Body: "Can you review the command flow?"},
		},
		CurrentClaim: &claim,
		ReadyTask:    &task,
	}, defaults)
	if !containsText(mailDecision.Prompt, "From: mia") {
		t.Fatalf("expected mail prompt, got: %q", mailDecision.Prompt)
	}
	if !containsText(mailDecision.Prompt, "Can you review the command flow?") {
		t.Fatalf("expected mail body in prompt, got: %q", mailDecision.Prompt)
	}

	claimDecision := selectRunDispatch(runDispatchSummary{
		CurrentClaim: &claim,
		ReadyTask:    &task,
	}, defaults)
	if !containsText(claimDecision.Prompt, "Continue working on bdh-421.3: Dispatch logic") {
		t.Fatalf("expected current-claim prompt, got: %q", claimDecision.Prompt)
	}
	if !containsText(claimDecision.Prompt, "code-reviewer pass") {
		t.Fatalf("expected review reminder, got: %q", claimDecision.Prompt)
	}

	readyDecision := selectRunDispatch(runDispatchSummary{ReadyTask: &task}, defaults)
	if !containsText(readyDecision.Prompt, "Pick up bdh-54y: List active") {
		t.Fatalf("expected ready-task prompt, got: %q", readyDecision.Prompt)
	}

	idleDecision := selectRunDispatch(runDispatchSummary{}, defaults)
	if idleDecision.WaitSeconds != 30 {
		t.Fatalf("expected long idle wait, got %d", idleDecision.WaitSeconds)
	}
	if !idleDecision.Skip {
		t.Fatalf("expected idle dispatch to skip provider launch")
	}
}

func TestTruncateRunDispatchBody(t *testing.T) {
	long := strings.Repeat("a", runDispatchBodyLimit+20)
	got := truncateRunDispatchBody(long)
	if len(got) <= runDispatchBodyLimit {
		t.Fatalf("expected truncated body with ellipsis, got length %d", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected ellipsis suffix, got %q", got)
	}
}

func TestRunLoopComposesBasePromptWithDispatcherPrompt(t *testing.T) {
	dispatcher := &fakeRunDispatcher{
		decisions: []runDispatchDecision{
			{Prompt: "dispatch prompt one", WaitSeconds: 7},
			{Prompt: "dispatch prompt two", WaitSeconds: 7},
		},
	}
	var commands [][]string

	loop := &runLoop{
		provider: claudeProvider{},
		now:      time.Now,
		out:      io.Discard,
		sleep:    func(context.Context, time.Duration) error { return nil },
		dispatch: dispatcher,
		runner: func(_ context.Context, _ string, argv []string, onLine func(string)) error {
			commands = append(commands, append([]string(nil), argv...))
			onLine(`{"type":"result","duration_ms":1000}`)
			return nil
		},
	}

	err := loop.Run(context.Background(), runLoopOptions{
		Prompt:      "initial prompt",
		WaitSeconds: 0,
		MaxRuns:     2,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(commands) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(commands))
	}
	for i, want := range []string{"dispatch prompt one", "dispatch prompt two"} {
		cmd := joinArgs(commands[i])
		if !containsText(cmd, "initial prompt") {
			t.Fatalf("expected base prompt in run %d, got %q", i+1, cmd)
		}
		if !containsText(cmd, want) {
			t.Fatalf("expected dispatch prompt %q in run %d, got %q", want, i+1, cmd)
		}
	}
}

func TestRunLoopWaitsForDispatchRecoveryOnDispatchErrorEvenWithBasePrompt(t *testing.T) {
	dispatcher := &fakeRunDispatcher{err: errors.New("server down")}
	var output strings.Builder
	var slept []time.Duration

	loop := &runLoop{
		provider: claudeProvider{},
		now:      time.Now,
		out:      &output,
		defaults: runDispatchDefaults{IdleWaitSeconds: 1},
		sleep: func(_ context.Context, d time.Duration) error {
			slept = append(slept, d)
			return nil
		},
		dispatch: dispatcher,
		runner: func(_ context.Context, _ string, _ []string, _ func(string)) error {
			t.Fatal("runner should not be called")
			return nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	loop.sleep = func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		cancel()
		return nil
	}
	err := loop.Run(ctx, runLoopOptions{
		Prompt:      "stable prompt",
		WaitSeconds: 0,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	if len(slept) == 0 {
		t.Fatal("expected idle sleep while waiting for dispatch recovery")
	}
	if !containsText(output.String(), "dispatch failed: server down") {
		t.Fatalf("expected dispatch failure log, got %q", output.String())
	}
	if !containsText(output.String(), "waiting for dispatch recovery") {
		t.Fatalf("expected dispatch recovery log, got %q", output.String())
	}
}

func TestRunLoopWaitsForDispatchRecoveryWithoutExplicitPrompt(t *testing.T) {
	dispatcher := &fakeRunDispatcher{err: errors.New("server down")}
	var output strings.Builder
	var slept []time.Duration

	loop := &runLoop{
		provider: claudeProvider{},
		now:      time.Now,
		out:      &output,
		defaults: runDispatchDefaults{IdleWaitSeconds: 1},
		sleep: func(_ context.Context, d time.Duration) error {
			slept = append(slept, d)
			return nil
		},
		dispatch: dispatcher,
		runner: func(_ context.Context, _ string, _ []string, _ func(string)) error {
			t.Fatal("runner should not be called")
			return nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	loop.sleep = func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		cancel()
		return nil
	}
	err := loop.Run(ctx, runLoopOptions{WaitSeconds: 0})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	if len(slept) == 0 {
		t.Fatal("expected idle sleep while waiting for dispatch recovery")
	}
	if !containsText(output.String(), "waiting for dispatch recovery") {
		t.Fatalf("expected dispatch recovery log, got %q", output.String())
	}
}
