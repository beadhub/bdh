package commands

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	awrun "github.com/awebai/aw/run"
)

type integrationAWProvider struct {
	prompts []string
}

func (p *integrationAWProvider) Name() string { return "integration-aw-provider" }

func (p *integrationAWProvider) BuildCommand(prompt string, _ awrun.BuildOptions) ([]string, error) {
	p.prompts = append(p.prompts, prompt)
	return []string{"fake-run", prompt}, nil
}

func (p *integrationAWProvider) ParseOutput(string) (*awrun.Event, error) { return nil, nil }

func (p *integrationAWProvider) SessionID(event *awrun.Event) string {
	if event == nil {
		return ""
	}
	return event.Session
}

type integrationRunDispatcher struct {
	decisions      []runDispatchDecision
	autofeedStates []bool
}

func (d *integrationRunDispatcher) Next(_ context.Context, autofeed bool) (runDispatchDecision, error) {
	d.autofeedStates = append(d.autofeedStates, autofeed)
	if len(d.decisions) == 0 {
		return runDispatchDecision{}, nil
	}
	decision := d.decisions[0]
	d.decisions = d.decisions[1:]
	return decision, nil
}

func TestAwRunDispatcherAdapterIntegratesWithAwLoop(t *testing.T) {
	provider := &integrationAWProvider{}
	dispatcher := &integrationRunDispatcher{
		decisions: []runDispatchDecision{
			{Skip: true, WaitSeconds: 0},
			{Prompt: "Respond to unread mail from mia.", WaitSeconds: 0},
		},
	}

	runnerCalls := 0
	loop := awrun.NewLoop(provider, io.Discard)
	loop.Dispatch = &awRunDispatcherAdapter{inner: dispatcher}
	loop.Runner = func(_ context.Context, _ string, _ []string, _ func(string), _ any) error {
		runnerCalls++
		return nil
	}
	loop.Sleep = func(context.Context, time.Duration) error { return nil }

	err := loop.Run(context.Background(), awrun.LoopOptions{
		Prompt:          "Preserve dispatch ordering and policy behavior.",
		WaitSeconds:     0,
		IdleWaitSeconds: 0,
		MaxRuns:         1,
		Autofeed:        true,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if runnerCalls != 1 {
		t.Fatalf("expected exactly one provider run after initial skip, got %d", runnerCalls)
	}
	if len(provider.prompts) != 1 {
		t.Fatalf("expected one built prompt, got %d", len(provider.prompts))
	}
	gotPrompt := provider.prompts[0]
	if !strings.Contains(gotPrompt, "Primary mission:\nPreserve dispatch ordering and policy behavior.") {
		t.Fatalf("expected base mission in composed prompt, got %q", gotPrompt)
	}
	if !strings.Contains(gotPrompt, "Current cycle:\nRespond to unread mail from mia.") {
		t.Fatalf("expected dispatch cycle prompt in composed prompt, got %q", gotPrompt)
	}
	if len(dispatcher.autofeedStates) != 2 {
		t.Fatalf("expected two dispatch calls, got %d", len(dispatcher.autofeedStates))
	}
	if !dispatcher.autofeedStates[0] || !dispatcher.autofeedStates[1] {
		t.Fatalf("expected dispatch calls with autofeed=true, got %#v", dispatcher.autofeedStates)
	}
}
