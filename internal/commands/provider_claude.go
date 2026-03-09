package commands

import (
	"fmt"
	"strings"

	awrun "github.com/awebai/aw/run"
)

type runUsageStats struct {
	InputTokens              int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
	OutputTokens             int
	ContextWindowSize        int
}

func (s runUsageStats) TotalInput() int {
	return s.InputTokens + s.CacheCreationInputTokens + s.CacheReadInputTokens
}

func (s runUsageStats) ContextPct() float64 {
	if s.ContextWindowSize <= 0 {
		return 0
	}
	return float64(s.TotalInput()) / float64(s.ContextWindowSize) * 100
}

type runBuildOptions struct {
	SessionID       string
	ContinueSession bool
	AllowedTools    string
	Model           string
}

type runProvider interface {
	Name() string
	BuildCommand(prompt string, opts runBuildOptions) ([]string, error)
	ParseOutput(line string) (*runEvent, error)
	SessionID(event *runEvent) string
}

type runEventType string

const (
	runEventText       runEventType = "text"
	runEventToolCall   runEventType = "tool_call"
	runEventToolResult runEventType = "tool_result"
	runEventDone       runEventType = "done"
	runEventSystem     runEventType = "system"
)

type runToolCall struct {
	Name  string
	Input map[string]any
}

type runEvent struct {
	Type       runEventType
	Text       string
	ToolCalls  []runToolCall
	DurationMS int
	CostUSD    *float64
	Session    string
	IsError    bool
	Usage      *runUsageStats
}

type claudeProvider struct{}

func newRunProvider(name string) (runProvider, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "claude":
		return claudeProvider{}, nil
	case "codex":
		return codexProvider{}, nil
	default:
		return nil, fmt.Errorf("unsupported provider %q", name)
	}
}

func (claudeProvider) Name() string {
	return "claude"
}

func (claudeProvider) BuildCommand(prompt string, opts runBuildOptions) ([]string, error) {
	provider, err := awrun.NewProvider("claude")
	if err != nil {
		return nil, err
	}
	return provider.BuildCommand(prompt, toAWRunBuildOptions(opts))
}

func (claudeProvider) ParseOutput(line string) (*runEvent, error) {
	provider, err := awrun.NewProvider("claude")
	if err != nil {
		return nil, err
	}
	event, err := provider.ParseOutput(line)
	if err != nil {
		return nil, err
	}
	return fromAWRunEvent(event), nil
}

func (claudeProvider) SessionID(event *runEvent) string {
	if event == nil {
		return ""
	}
	return event.Session
}

func toAWRunBuildOptions(opts runBuildOptions) awrun.BuildOptions {
	return awrun.BuildOptions{
		SessionID:       opts.SessionID,
		ContinueSession: opts.ContinueSession,
		AllowedTools:    opts.AllowedTools,
		Model:           opts.Model,
	}
}

func fromAWRunEvent(event *awrun.Event) *runEvent {
	if event == nil {
		return nil
	}

	mapped := &runEvent{
		Type:       runEventType(event.Type),
		Text:       event.Text,
		DurationMS: event.DurationMS,
		CostUSD:    event.CostUSD,
		Session:    event.Session,
		IsError:    event.IsError,
	}
	if len(event.ToolCalls) > 0 {
		mapped.ToolCalls = make([]runToolCall, 0, len(event.ToolCalls))
		for _, call := range event.ToolCalls {
			mapped.ToolCalls = append(mapped.ToolCalls, runToolCall{
				Name:  call.Name,
				Input: call.Input,
			})
		}
	}
	if event.Usage != nil {
		mapped.Usage = &runUsageStats{
			InputTokens:              event.Usage.InputTokens,
			CacheCreationInputTokens: event.Usage.CacheCreationInputTokens,
			CacheReadInputTokens:     event.Usage.CacheReadInputTokens,
			OutputTokens:             event.Usage.OutputTokens,
			ContextWindowSize:        event.Usage.ContextWindowSize,
		}
	}

	return mapped
}
