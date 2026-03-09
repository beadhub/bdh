package commands

import (
	"context"
	"sync"
	"testing"
	"time"

	aweb "github.com/awebai/aw"
)

type fakeRunScreenSurface struct {
	events            chan runControlEvent
	pending           bool
	appended          []string
	lastInputLine     string
	lastStatusLine    string
	clearStatusCalls  int
	clearInputCalls   int
	exitConfirmation  bool
	activeProgramMode bool
}

func (f *fakeRunScreenSurface) Start() error { return nil }
func (f *fakeRunScreenSurface) Stop() error  { return nil }
func (f *fakeRunScreenSurface) Events() <-chan runControlEvent {
	return f.events
}
func (f *fakeRunScreenSurface) HasPendingInput() bool { return f.pending }
func (f *fakeRunScreenSurface) AppendText(text string) {
	f.appended = append(f.appended, text)
}
func (f *fakeRunScreenSurface) AppendLine(text string) {
	f.appended = append(f.appended, text)
}
func (f *fakeRunScreenSurface) SetInputLine(line string)  { f.lastInputLine = line }
func (f *fakeRunScreenSurface) SetStatusLine(line string) { f.lastStatusLine = line }
func (f *fakeRunScreenSurface) ClearStatusLine()          { f.clearStatusCalls++ }
func (f *fakeRunScreenSurface) ClearInputLine()           { f.clearInputCalls++ }
func (f *fakeRunScreenSurface) SetExitConfirmation(active bool) {
	f.exitConfirmation = active
}
func (f *fakeRunScreenSurface) hasActiveProgram() bool { return f.activeProgramMode }

func TestAwRunInputControllerAdapterForwardsScreenMethods(t *testing.T) {
	inner := &fakeRunScreenSurface{
		events:            make(chan runControlEvent, 1),
		activeProgramMode: true,
	}
	adapter := &awRunInputControllerAdapter{inner: inner}

	adapter.AppendText("hello")
	adapter.AppendLine("world")
	adapter.SetInputLine("input>")
	adapter.SetStatusLine("waiting")
	adapter.ClearStatusLine()
	adapter.ClearInputLine()
	adapter.SetExitConfirmation(true)

	if len(inner.appended) != 2 {
		t.Fatalf("expected 2 appended lines, got %d", len(inner.appended))
	}
	if inner.lastInputLine != "input>" {
		t.Fatalf("input line=%q", inner.lastInputLine)
	}
	if inner.lastStatusLine != "waiting" {
		t.Fatalf("status line=%q", inner.lastStatusLine)
	}
	if inner.clearStatusCalls != 1 {
		t.Fatalf("clear status calls=%d", inner.clearStatusCalls)
	}
	if inner.clearInputCalls != 1 {
		t.Fatalf("clear input calls=%d", inner.clearInputCalls)
	}
	if !inner.exitConfirmation {
		t.Fatal("expected exit confirmation to be forwarded")
	}
	if !adapter.hasActiveProgram() {
		t.Fatal("expected active program state to be forwarded")
	}
}

type scriptedRunWakeStream struct {
	mu    sync.Mutex
	calls int
}

func (s *scriptedRunWakeStream) Stream(ctx context.Context, _ time.Time) (<-chan runWakeEvent, <-chan error) {
	s.mu.Lock()
	s.calls++
	call := s.calls
	s.mu.Unlock()

	events := make(chan runWakeEvent, 1)
	errs := make(chan error, 1)

	switch call {
	case 1:
		close(events)
		close(errs)
	case 2:
		events <- runWakeEvent{Type: runWakeEventMailMessage, MessageID: "m1", FromAlias: "yara"}
		close(events)
		close(errs)
	default:
		go func() {
			<-ctx.Done()
			close(events)
			close(errs)
		}()
	}

	return events, errs
}

func TestAwRunWakeStreamAdapterReconnectsAfterEarlyCleanClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	now := time.Now()
	inner := &scriptedRunWakeStream{}
	adapter := &awRunWakeStreamAdapter{
		inner: inner,
		now:   func() time.Time { return now },
		sleep: func(_ context.Context, d time.Duration) error {
			now = now.Add(d)
			return nil
		},
	}

	events, errs := adapter.Stream(ctx, now.Add(5*time.Second))

	select {
	case evt, ok := <-events:
		if !ok {
			t.Fatal("wake events channel closed before receiving mapped event")
		}
		if evt.Type != aweb.AgentEventMailMessage {
			t.Fatalf("event type=%s", evt.Type)
		}
		cancel()
	case err, ok := <-errs:
		if ok && err != nil {
			t.Fatalf("unexpected wake stream error: %v", err)
		}
		t.Fatal("wake stream ended before delivering mapped event")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for mapped wake event")
	}

	inner.mu.Lock()
	calls := inner.calls
	inner.mu.Unlock()
	if calls < 2 {
		t.Fatalf("expected reconnect after early clean close, calls=%d", calls)
	}
}
