package commands

import (
	"context"
	"errors"
	"testing"
)

type fakeAWAdapterDispatcher struct {
	decision runDispatchDecision
	err      error
}

func (f *fakeAWAdapterDispatcher) Next(_ context.Context, _ bool) (runDispatchDecision, error) {
	if f.err != nil {
		return runDispatchDecision{}, f.err
	}
	return f.decision, nil
}

func TestAwRunDispatcherAdapterMapsDecision(t *testing.T) {
	adapter := &awRunDispatcherAdapter{
		inner: &fakeAWAdapterDispatcher{
			decision: runDispatchDecision{
				Prompt:      "handle pending mail",
				WaitSeconds: 17,
				Skip:        true,
			},
		},
	}

	decision, err := adapter.Next(context.Background(), true)
	if err != nil {
		t.Fatalf("Next returned error: %v", err)
	}
	if decision.Prompt != "handle pending mail" {
		t.Fatalf("prompt=%q", decision.Prompt)
	}
	if decision.WaitSeconds != 17 {
		t.Fatalf("wait_seconds=%d", decision.WaitSeconds)
	}
	if !decision.Skip {
		t.Fatal("expected skip=true")
	}
}

func TestAwRunDispatcherAdapterPropagatesError(t *testing.T) {
	expected := errors.New("dispatch unavailable")
	adapter := &awRunDispatcherAdapter{
		inner: &fakeAWAdapterDispatcher{err: expected},
	}

	_, err := adapter.Next(context.Background(), false)
	if !errors.Is(err, expected) {
		t.Fatalf("expected %v, got %v", expected, err)
	}
}
