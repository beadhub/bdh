package commands

import awrun "github.com/awebai/aw/run"

type codexProvider struct{}

func (codexProvider) Name() string {
	return "codex"
}

func (codexProvider) BuildCommand(prompt string, opts runBuildOptions) ([]string, error) {
	provider, err := awrun.NewProvider("codex")
	if err != nil {
		return nil, err
	}
	return provider.BuildCommand(prompt, toAWRunBuildOptions(opts))
}

func (codexProvider) ParseOutput(line string) (*runEvent, error) {
	provider, err := awrun.NewProvider("codex")
	if err != nil {
		return nil, err
	}
	event, err := provider.ParseOutput(line)
	if err != nil {
		return nil, err
	}
	return fromAWRunEvent(event), nil
}

func (codexProvider) SessionID(event *runEvent) string {
	if event == nil {
		return ""
	}
	return event.Session
}
