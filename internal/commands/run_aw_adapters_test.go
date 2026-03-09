package commands

import (
	"context"
	"sync"
	"testing"
	"time"

	aweb "github.com/awebai/aw"
)

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
