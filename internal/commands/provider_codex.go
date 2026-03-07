package commands

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type codexProvider struct{}

type codexEnvelope struct {
	Type     string          `json:"type"`
	ThreadID string          `json:"thread_id"`
	Item     json.RawMessage `json:"item"`
}

type codexItem struct {
	ID               string `json:"id"`
	Type             string `json:"type"`
	Text             string `json:"text"`
	Command          string `json:"command"`
	AggregatedOutput string `json:"aggregated_output"`
	ExitCode         *int   `json:"exit_code"`
	Status           string `json:"status"`
}

func (codexProvider) Name() string {
	return "codex"
}

func (codexProvider) BuildCommand(prompt string, opts runBuildOptions) ([]string, error) {
	if strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("prompt cannot be empty")
	}
	if strings.TrimSpace(opts.AllowedTools) != "" {
		return nil, fmt.Errorf("provider codex does not support --allowed-tools")
	}

	command := []string{
		"codex",
		"exec",
	}
	if strings.TrimSpace(opts.SessionID) != "" {
		command = append(command, "resume", opts.SessionID)
	} else if opts.ContinueSession {
		command = append(command, "resume", "--last")
	}
	command = append(command, "--json")
	if strings.TrimSpace(opts.Model) != "" {
		command = append(command, "-m", opts.Model)
	}
	command = append(command, prompt)
	return command, nil
}

func (codexProvider) ParseOutput(line string) (*runEvent, error) {
	var envelope codexEnvelope
	if err := json.Unmarshal([]byte(line), &envelope); err != nil {
		return nil, err
	}

	switch envelope.Type {
	case "thread.started":
		session := strings.TrimSpace(envelope.ThreadID)
		if session == "" {
			return &runEvent{}, nil
		}
		return &runEvent{
			Type:    runEventSystem,
			Text:    fmt.Sprintf("session %s", truncateRunText(session, 12)),
			Session: session,
		}, nil
	case "item.started":
		item, err := codexParseItem(envelope.Item)
		if err != nil {
			return nil, err
		}
		if item.Type == "command_execution" {
			return &runEvent{
				Type: runEventToolCall,
				ToolCalls: []runToolCall{{
					Name: "Bash",
					Input: map[string]any{
						"command": codexDisplayCommand(item.Command),
					},
				}},
			}, nil
		}
	case "item.completed":
		item, err := codexParseItem(envelope.Item)
		if err != nil {
			return nil, err
		}
		switch item.Type {
		case "agent_message":
			return &runEvent{Type: runEventText, Text: item.Text}, nil
		case "command_execution":
			return &runEvent{Type: runEventToolResult, Text: codexToolResultText(item)}, nil
		}
	case "turn.completed":
		return &runEvent{Type: runEventDone}, nil
	}

	return &runEvent{}, nil
}

func (codexProvider) SessionID(event *runEvent) string {
	if event == nil {
		return ""
	}
	return event.Session
}

func codexParseItem(raw json.RawMessage) (codexItem, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return codexItem{}, nil
	}
	var item codexItem
	if err := json.Unmarshal(raw, &item); err != nil {
		return codexItem{}, err
	}
	return item, nil
}

func codexDisplayCommand(command string) string {
	command = strings.TrimSpace(command)
	for _, prefix := range []string{
		"/bin/zsh -lc ",
		"/bin/bash -lc ",
		"zsh -lc ",
		"bash -lc ",
		"sh -lc ",
	} {
		if !strings.HasPrefix(command, prefix) {
			continue
		}
		remainder := strings.TrimSpace(strings.TrimPrefix(command, prefix))
		if unquoted, err := strconv.Unquote(remainder); err == nil {
			return unquoted
		}
		if len(remainder) >= 2 {
			if remainder[0] == '\'' && remainder[len(remainder)-1] == '\'' {
				return remainder[1 : len(remainder)-1]
			}
			if remainder[0] == '"' && remainder[len(remainder)-1] == '"' {
				return remainder[1 : len(remainder)-1]
			}
		}
		return remainder
	}
	return command
}

func codexToolResultText(item codexItem) string {
	text := strings.TrimSpace(item.AggregatedOutput)
	if text != "" {
		if item.ExitCode != nil && *item.ExitCode != 0 {
			return fmt.Sprintf("%s (exit %d)", text, *item.ExitCode)
		}
		return text
	}
	if item.ExitCode != nil && *item.ExitCode != 0 {
		return fmt.Sprintf("exit %d", *item.ExitCode)
	}
	return ""
}
