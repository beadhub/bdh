package commands

import (
	"context"
	"errors"
	"time"

	aweb "github.com/awebai/aw"
	awrun "github.com/awebai/aw/run"
)

type awRunInputControllerAdapter struct {
	inner runInputController
}

type runScreenSurface interface {
	AppendText(string)
	AppendLine(string)
	SetInputLine(string)
	SetStatusLine(string)
	ClearStatusLine()
	ClearInputLine()
	SetExitConfirmation(bool)
	hasActiveProgram() bool
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

func (a *awRunInputControllerAdapter) AppendText(text string) {
	if screen := a.screen(); screen != nil {
		screen.AppendText(text)
	}
}

func (a *awRunInputControllerAdapter) AppendLine(text string) {
	if screen := a.screen(); screen != nil {
		screen.AppendLine(text)
	}
}

func (a *awRunInputControllerAdapter) SetInputLine(line string) {
	if screen := a.screen(); screen != nil {
		screen.SetInputLine(line)
	}
}

func (a *awRunInputControllerAdapter) SetStatusLine(line string) {
	if screen := a.screen(); screen != nil {
		screen.SetStatusLine(line)
	}
}

func (a *awRunInputControllerAdapter) ClearStatusLine() {
	if screen := a.screen(); screen != nil {
		screen.ClearStatusLine()
	}
}

func (a *awRunInputControllerAdapter) ClearInputLine() {
	if screen := a.screen(); screen != nil {
		screen.ClearInputLine()
	}
}

func (a *awRunInputControllerAdapter) SetExitConfirmation(active bool) {
	if screen := a.screen(); screen != nil {
		screen.SetExitConfirmation(active)
	}
}

func (a *awRunInputControllerAdapter) hasActiveProgram() bool {
	if screen := a.screen(); screen != nil {
		return screen.hasActiveProgram()
	}
	return false
}

func (a *awRunInputControllerAdapter) screen() runScreenSurface {
	if a == nil || a.inner == nil {
		return nil
	}
	screen, _ := a.inner.(runScreenSurface)
	return screen
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

type awRunWakeStreamAdapter struct {
	inner runWakeStream
	now   func() time.Time
	sleep runSleepFunc
}

func (a *awRunWakeStreamAdapter) Stream(ctx context.Context, deadline time.Time) (<-chan aweb.AgentEvent, <-chan error) {
	events := make(chan aweb.AgentEvent, 32)
	errs := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errs)
		if err := a.stream(ctx, deadline, events); err != nil && ctx.Err() == nil {
			errs <- err
		}
	}()

	return events, errs
}

func (a *awRunWakeStreamAdapter) stream(ctx context.Context, deadline time.Time, out chan<- aweb.AgentEvent) error {
	if a == nil || a.inner == nil {
		return nil
	}
	now := a.now
	if now == nil {
		now = time.Now
	}
	sleep := a.sleep
	if sleep == nil {
		sleep = sleepWithContext
	}

	for {
		if !now().Before(deadline) {
			return nil
		}
		events, errs := a.inner.Stream(ctx, deadline)
		reconnect := false

	streamLoop:
		for {
			select {
			case evt, ok := <-events:
				if !ok {
					events = nil
					if errs == nil {
						reconnect = now().Before(deadline)
						break streamLoop
					}
					continue
				}
				mapped, ok := mapRunWakeEvent(evt)
				if !ok {
					continue
				}
				select {
				case <-ctx.Done():
					return nil
				case out <- mapped:
				}
			case err, ok := <-errs:
				if !ok {
					errs = nil
					if events == nil {
						reconnect = now().Before(deadline)
						break streamLoop
					}
					continue
				}
				if err == nil {
					reconnect = now().Before(deadline)
					break streamLoop
				}
				return err
			case <-ctx.Done():
				return nil
			}
		}

		if !reconnect {
			return nil
		}
		backoff := time.Second
		remaining := deadline.Sub(now())
		if remaining <= 0 {
			return nil
		}
		if remaining < backoff {
			backoff = remaining
		}
		if err := sleep(ctx, backoff); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			return err
		}
	}
}

func mapRunWakeEvent(evt runWakeEvent) (aweb.AgentEvent, bool) {
	mapped := aweb.AgentEvent{
		AgentID:   evt.AgentID,
		ProjectID: evt.ProjectID,
		MessageID: evt.MessageID,
		FromAlias: evt.FromAlias,
		SessionID: evt.SessionID,
		Subject:   evt.Subject,
		TaskID:    evt.TaskID,
		Title:     evt.Title,
		Status:    evt.Status,
		SignalID:  evt.SignalID,
		Text:      evt.Text,
	}
	switch evt.Type {
	case runWakeEventConnected:
		mapped.Type = aweb.AgentEventConnected
	case runWakeEventMailMessage:
		mapped.Type = aweb.AgentEventMailMessage
	case runWakeEventChatMessage:
		mapped.Type = aweb.AgentEventChatMessage
	case runWakeEventWorkAvailable:
		mapped.Type = aweb.AgentEventWorkAvailable
	case runWakeEventClaimUpdate:
		mapped.Type = aweb.AgentEventClaimUpdate
	case runWakeEventClaimRemoved:
		mapped.Type = aweb.AgentEventClaimRemoved
	case runWakeEventControlPause:
		mapped.Type = aweb.AgentEventControlPause
	case runWakeEventControlResume:
		mapped.Type = aweb.AgentEventControlResume
	case runWakeEventControlInterrupt:
		mapped.Type = aweb.AgentEventControlInterrupt
	case runWakeEventError:
		mapped.Type = aweb.AgentEventError
	default:
		return aweb.AgentEvent{}, false
	}
	return mapped, true
}
