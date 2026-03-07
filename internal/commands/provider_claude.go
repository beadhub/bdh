package commands

import (
	"encoding/json"
	"fmt"
	"strings"
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

type claudeEnvelope struct {
	Type       string          `json:"type"`
	Subtype    string          `json:"subtype"`
	Event      claudeEvent     `json:"event"`
	Message    json.RawMessage `json:"message"`
	Content    any             `json:"content"`
	DurationMS int             `json:"duration_ms"`
	Stats      struct {
		DurationMS int `json:"duration_ms"`
	} `json:"stats"`
	CostUSD      *float64 `json:"cost_usd"`
	TotalCostUSD *float64 `json:"total_cost_usd"`
	Session      string   `json:"session_id"`
	CWD          string   `json:"cwd"`
	Model        string   `json:"model"`
	Result       string   `json:"result"`
	IsError      bool     `json:"is_error"`
}

type claudeEvent struct {
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
}

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
	if strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("prompt cannot be empty")
	}

	command := []string{
		"claude",
		"-p",
		prompt,
		"--dangerously-skip-permissions",
		"--output-format",
		"stream-json",
		"--verbose",
		"--include-partial-messages",
	}

	if strings.TrimSpace(opts.SessionID) != "" {
		command = append(command, "--resume", opts.SessionID)
	} else if opts.ContinueSession {
		command = append(command, "--continue")
	}
	if strings.TrimSpace(opts.AllowedTools) != "" {
		command = append(command, "--allowedTools", opts.AllowedTools)
	}
	if strings.TrimSpace(opts.Model) != "" {
		command = append(command, "--model", opts.Model)
	}

	return command, nil
}

func (claudeProvider) ParseOutput(line string) (*runEvent, error) {
	var envelope claudeEnvelope
	if err := json.Unmarshal([]byte(line), &envelope); err != nil {
		return nil, err
	}

	switch envelope.Type {
	case "stream_event":
		if envelope.Event.Delta.Type == "text_delta" {
			return &runEvent{Type: runEventText, Text: envelope.Event.Delta.Text}, nil
		}
	case "assistant":
		var message struct {
			Content []struct {
				Type  string         `json:"type"`
				Name  string         `json:"name"`
				Input map[string]any `json:"input"`
			} `json:"content"`
			Usage struct {
				InputTokens              int `json:"input_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens"`
				OutputTokens             int `json:"output_tokens"`
			} `json:"usage"`
			Model string `json:"model"`
		}
		if err := json.Unmarshal(envelope.Message, &message); err != nil {
			return nil, err
		}
		calls := make([]runToolCall, 0, len(message.Content))
		for _, block := range message.Content {
			if block.Type != "tool_use" {
				continue
			}
			calls = append(calls, runToolCall{Name: block.Name, Input: block.Input})
		}
		var usage *runUsageStats
		if message.Usage.InputTokens > 0 || message.Usage.CacheCreationInputTokens > 0 || message.Usage.CacheReadInputTokens > 0 || message.Usage.OutputTokens > 0 {
			usage = &runUsageStats{
				InputTokens:              message.Usage.InputTokens,
				CacheCreationInputTokens: message.Usage.CacheCreationInputTokens,
				CacheReadInputTokens:     message.Usage.CacheReadInputTokens,
				OutputTokens:             message.Usage.OutputTokens,
				ContextWindowSize:        claudeContextWindowSize(message.Model),
			}
			if usage.ContextWindowSize == 0 {
				usage.ContextWindowSize = claudeContextWindowSize(envelope.Model)
			}
		}
		if len(calls) > 0 {
			return &runEvent{Type: runEventToolCall, ToolCalls: calls, Usage: usage}, nil
		}
		if usage != nil {
			return &runEvent{Usage: usage}, nil
		}
	case "tool_result":
		return &runEvent{Type: runEventToolResult, Text: claudeToolResultText(envelope.Content)}, nil
	case "result":
		duration := envelope.DurationMS
		if duration == 0 {
			duration = envelope.Stats.DurationMS
		}
		costUSD := envelope.CostUSD
		if costUSD == nil {
			costUSD = envelope.TotalCostUSD
		}
		return &runEvent{
			Type:       runEventDone,
			Text:       strings.TrimSpace(envelope.Result),
			DurationMS: duration,
			CostUSD:    costUSD,
			Session:    envelope.Session,
			IsError:    envelope.IsError,
		}, nil
	case "system":
		text, err := claudeSystemEventText(envelope)
		if err != nil {
			return nil, err
		}
		return &runEvent{Type: runEventSystem, Text: text}, nil
	}

	return &runEvent{}, nil
}

func claudeContextWindowSize(model string) int {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return 200000
	}
	return 200000
}

func (claudeProvider) SessionID(event *runEvent) string {
	if event == nil {
		return ""
	}
	return event.Session
}

func claudeToolResultText(content any) string {
	switch typed := content.(type) {
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}
			blockType, _ := block["type"].(string)
			if blockType != "text" {
				continue
			}
			text, _ := block["text"].(string)
			if strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, " ")
	default:
		return fmt.Sprintf("%v", content)
	}
}

func claudeSystemMessageText(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err == nil {
		parts := make([]string, 0, 3)
		if sessionID, _ := payload["session_id"].(string); strings.TrimSpace(sessionID) != "" {
			parts = append(parts, fmt.Sprintf("session %s", truncateRunText(sessionID, 12)))
		}
		if cwd, _ := payload["cwd"].(string); strings.TrimSpace(cwd) != "" {
			parts = append(parts, fmt.Sprintf("cwd=%s", truncateRunText(cwd, 40)))
		}
		if model, _ := payload["model"].(string); strings.TrimSpace(model) != "" {
			parts = append(parts, fmt.Sprintf("model=%s", model))
		}
		if len(parts) == 0 {
			return "session event", nil
		}
		return strings.Join(parts, "  "), nil
	}

	return "", nil
}

func claudeSystemEventText(envelope claudeEnvelope) (string, error) {
	if len(envelope.Message) > 0 && string(envelope.Message) != "null" {
		text, err := claudeSystemMessageText(envelope.Message)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(text) != "" {
			return text, nil
		}
	}

	parts := make([]string, 0, 4)
	if subtype := strings.TrimSpace(envelope.Subtype); subtype != "" && subtype != "init" {
		parts = append(parts, subtype)
	}
	if sessionID := strings.TrimSpace(envelope.Session); sessionID != "" {
		parts = append(parts, fmt.Sprintf("session %s", truncateRunText(sessionID, 12)))
	}
	if cwd := strings.TrimSpace(envelope.CWD); cwd != "" {
		parts = append(parts, fmt.Sprintf("cwd=%s", truncateRunText(cwd, 40)))
	}
	if model := strings.TrimSpace(envelope.Model); model != "" {
		parts = append(parts, fmt.Sprintf("model=%s", model))
	}
	if len(parts) == 0 {
		if subtype := strings.TrimSpace(envelope.Subtype); subtype != "" {
			return subtype, nil
		}
		return "", nil
	}

	return strings.Join(parts, "  "), nil
}
