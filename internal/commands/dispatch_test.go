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
	claim := ClaimInfo{BeadID: "bdh-421.3", Title: "Dispatch logic"}
	task := runReadyTask{ID: "bdh-54y", Title: "List active"}

	chatDecision := selectRunDispatch(runDispatchSummary{
		PendingChatAlias: "grace",
		UnreadMailCount:  3,
		CurrentClaim:     &claim,
		ReadyTask:        &task,
	})
	if !containsText(chatDecision.Prompt, "Respond to chat from grace") {
		t.Fatalf("expected chat prompt, got: %q", chatDecision.Prompt)
	}
	if chatDecision.WaitSeconds != 5 {
		t.Fatalf("expected short wait for chat, got %d", chatDecision.WaitSeconds)
	}

	mailDecision := selectRunDispatch(runDispatchSummary{
		UnreadMailCount: 2,
		UnreadMailFrom:  "mia",
		CurrentClaim:    &claim,
		ReadyTask:       &task,
	})
	if !containsText(mailDecision.Prompt, "unread mail from mia") {
		t.Fatalf("expected mail prompt, got: %q", mailDecision.Prompt)
	}

	claimDecision := selectRunDispatch(runDispatchSummary{
		CurrentClaim: &claim,
		ReadyTask:    &task,
	})
	if !containsText(claimDecision.Prompt, "Continue working on bdh-421.3: Dispatch logic") {
		t.Fatalf("expected current-claim prompt, got: %q", claimDecision.Prompt)
	}
	if !containsText(claimDecision.Prompt, "code-reviewer pass") {
		t.Fatalf("expected review reminder, got: %q", claimDecision.Prompt)
	}

	readyDecision := selectRunDispatch(runDispatchSummary{ReadyTask: &task})
	if !containsText(readyDecision.Prompt, "Pick up bdh-54y: List active") {
		t.Fatalf("expected ready-task prompt, got: %q", readyDecision.Prompt)
	}

	idleDecision := selectRunDispatch(runDispatchSummary{})
	if idleDecision.WaitSeconds != 60 {
		t.Fatalf("expected long idle wait, got %d", idleDecision.WaitSeconds)
	}
	if !containsText(idleDecision.Prompt, "Check in with your coordinator") {
		t.Fatalf("expected idle prompt, got: %q", idleDecision.Prompt)
	}
}

func TestRunLoopUsesDispatcherAfterInitialPrompt(t *testing.T) {
	dispatcher := &fakeRunDispatcher{
		decisions: []runDispatchDecision{
			{Prompt: "dispatch prompt", WaitSeconds: 7},
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
	if !containsText(joinArgs(commands[0]), "initial prompt") {
		t.Fatalf("expected first run to use explicit prompt, got %q", joinArgs(commands[0]))
	}
	if !containsText(joinArgs(commands[1]), "dispatch prompt") {
		t.Fatalf("expected second run to use dispatch prompt, got %q", joinArgs(commands[1]))
	}
}

func TestRunLoopFallsBackToExplicitPromptOnDispatchError(t *testing.T) {
	dispatcher := &fakeRunDispatcher{err: errors.New("server down")}
	var commands [][]string
	var output strings.Builder

	loop := &runLoop{
		provider: claudeProvider{},
		now:      time.Now,
		out:      &output,
		sleep:    func(context.Context, time.Duration) error { return nil },
		dispatch: dispatcher,
		runner: func(_ context.Context, _ string, argv []string, onLine func(string)) error {
			commands = append(commands, append([]string(nil), argv...))
			onLine(`{"type":"result","duration_ms":1000}`)
			return nil
		},
	}

	err := loop.Run(context.Background(), runLoopOptions{
		Prompt:      "stable prompt",
		WaitSeconds: 0,
		MaxRuns:     2,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(commands) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(commands))
	}
	if !containsText(joinArgs(commands[1]), "stable prompt") {
		t.Fatalf("expected fallback explicit prompt, got %q", joinArgs(commands[1]))
	}
	if !containsText(output.String(), "dispatch failed: server down") {
		t.Fatalf("expected dispatch failure log, got %q", output.String())
	}
}

func TestRunLoopFallsBackToIdlePromptOnDispatchErrorWithoutExplicitPrompt(t *testing.T) {
	dispatcher := &fakeRunDispatcher{err: errors.New("server down")}
	var commands [][]string
	var output strings.Builder

	loop := &runLoop{
		provider: claudeProvider{},
		now:      time.Now,
		out:      &output,
		sleep:    func(context.Context, time.Duration) error { return nil },
		dispatch: dispatcher,
		runner: func(_ context.Context, _ string, argv []string, onLine func(string)) error {
			commands = append(commands, append([]string(nil), argv...))
			onLine(`{"type":"result","duration_ms":1000}`)
			return nil
		},
	}

	err := loop.Run(context.Background(), runLoopOptions{
		WaitSeconds: 0,
		MaxRuns:     1,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("expected 1 run, got %d", len(commands))
	}
	if !containsText(joinArgs(commands[0]), "Check in with your coordinator") {
		t.Fatalf("expected idle fallback prompt, got %q", joinArgs(commands[0]))
	}
	if !containsText(output.String(), "falling back to idle prompt") {
		t.Fatalf("expected idle fallback log, got %q", output.String())
	}
}
