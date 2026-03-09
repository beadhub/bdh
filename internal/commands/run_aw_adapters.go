package commands

import (
	"context"

	awrun "github.com/awebai/aw/run"
)

type awRunInputControllerAdapter struct {
	inner runInputController
}

func (a *awRunInputControllerAdapter) Start() error {
	if a == nil || a.inner == nil {
		return nil
	}
	return a.inner.Start()
}

func (a *awRunInputControllerAdapter) Stop() error {
	if a == nil || a.inner == nil {
		return nil
	}
	return a.inner.Stop()
}

func (a *awRunInputControllerAdapter) Events() <-chan awrun.ControlEvent {
	out := make(chan awrun.ControlEvent, 32)
	if a == nil || a.inner == nil {
		close(out)
		return out
	}

	go func() {
		defer close(out)
		for event := range a.inner.Events() {
			mapped, ok := mapRunControlEvent(event)
			if !ok {
				continue
			}
			out <- mapped
		}
	}()

	return out
}

func (a *awRunInputControllerAdapter) HasPendingInput() bool {
	if a == nil || a.inner == nil {
		return false
	}
	return a.inner.HasPendingInput()
}

func mapRunControlEvent(event runControlEvent) (awrun.ControlEvent, bool) {
	mapped := awrun.ControlEvent{Text: event.Text}
	switch event.Type {
	case runControlTypingStarted:
		mapped.Type = awrun.ControlTypingStarted
	case runControlBufferUpdated:
		mapped.Type = awrun.ControlBufferUpdated
	case runControlPrompt:
		mapped.Type = awrun.ControlPrompt
	case runControlQuit:
		mapped.Type = awrun.ControlQuit
	case runControlStop:
		mapped.Type = awrun.ControlStop
	case runControlWait:
		mapped.Type = awrun.ControlWait
	case runControlResume:
		mapped.Type = awrun.ControlResume
	case runControlAutofeedOn:
		mapped.Type = awrun.ControlAutofeedOn
	case runControlAutofeedOff:
		mapped.Type = awrun.ControlAutofeedOff
	case runControlInterrupt:
		mapped.Type = awrun.ControlInterrupt
	case runControlExitPrompt:
		mapped.Type = awrun.ControlExitPrompt
	case runControlExitConfirm:
		mapped.Type = awrun.ControlExitConfirm
	case runControlExitCancel:
		mapped.Type = awrun.ControlExitCancel
	default:
		return awrun.ControlEvent{}, false
	}
	return mapped, true
}

type awRunDispatcherAdapter struct {
	inner runDispatcher
}

func (a *awRunDispatcherAdapter) Next(ctx context.Context, autofeed bool) (awrun.DispatchDecision, error) {
	decision, err := a.inner.Next(ctx, autofeed)
	if err != nil {
		return awrun.DispatchDecision{}, err
	}
	return awrun.DispatchDecision{
		Prompt:      decision.Prompt,
		WaitSeconds: decision.WaitSeconds,
		Skip:        decision.Skip,
	}, nil
}

type awRunServiceSupervisorAdapter struct {
	inner runServiceSupervisor
}

func (a *awRunServiceSupervisorAdapter) Start(ctx context.Context, services []awrun.ServiceConfig, dir string) error {
	if a == nil || a.inner == nil {
		return nil
	}
	return a.inner.Start(ctx, fromAWRunServices(services), dir)
}

func (a *awRunServiceSupervisorAdapter) Stop() error {
	if a == nil || a.inner == nil {
		return nil
	}
	return a.inner.Stop()
}
