package commands

import "testing"

func TestClaudeProviderBuildCommand(t *testing.T) {
	provider := claudeProvider{}

	command, err := provider.BuildCommand("fix the bug", runBuildOptions{
		SessionID:    "sess-1",
		AllowedTools: "exec_command,apply_patch",
		Model:        "claude-sonnet-4",
	})
	if err != nil {
		t.Fatalf("BuildCommand returned error: %v", err)
	}

	joined := joinArgs(command)
	if !containsText(joined, "claude -p fix the bug") {
		t.Fatalf("expected base command, got: %q", joined)
	}
	if !containsText(joined, "--dangerously-skip-permissions") {
		t.Fatalf("expected skip permissions flag, got: %q", joined)
	}
	if !containsText(joined, "--resume sess-1") {
		t.Fatalf("expected resume flag, got: %q", joined)
	}
	if !containsText(joined, "--allowedTools exec_command,apply_patch") {
		t.Fatalf("expected allowedTools flag, got: %q", joined)
	}
	if !containsText(joined, "--model claude-sonnet-4") {
		t.Fatalf("expected model flag, got: %q", joined)
	}
}

func TestClaudeProviderParseOutput(t *testing.T) {
	provider := claudeProvider{}

	textEvent, err := provider.ParseOutput(`{"type":"stream_event","event":{"delta":{"type":"text_delta","text":"hello"}}}`)
	if err != nil {
		t.Fatalf("ParseOutput text returned error: %v", err)
	}
	if textEvent.Type != runEventText || textEvent.Text != "hello" {
		t.Fatalf("unexpected text event: %#v", textEvent)
	}

	toolEvent, err := provider.ParseOutput(`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"exec_command","input":{"cmd":"pwd"}}]}}`)
	if err != nil {
		t.Fatalf("ParseOutput tool returned error: %v", err)
	}
	if toolEvent.Type != runEventToolCall || len(toolEvent.ToolCalls) != 1 || toolEvent.ToolCalls[0].Name != "exec_command" {
		t.Fatalf("unexpected tool event: %#v", toolEvent)
	}

	resultEvent, err := provider.ParseOutput(`{"type":"result","duration_ms":2500,"cost_usd":0.0042,"session_id":"s1"}`)
	if err != nil {
		t.Fatalf("ParseOutput result returned error: %v", err)
	}
	if resultEvent.Type != runEventDone || resultEvent.DurationMS != 2500 || provider.SessionID(resultEvent) != "s1" {
		t.Fatalf("unexpected result event: %#v", resultEvent)
	}
	if resultEvent.CostUSD == nil || *resultEvent.CostUSD != 0.0042 {
		t.Fatalf("unexpected result cost: %#v", resultEvent.CostUSD)
	}

	systemEvent, err := provider.ParseOutput(`{"type":"system","subtype":"init","session_id":"abc123456789","cwd":"/tmp/project","model":"claude-opus"}`)
	if err != nil {
		t.Fatalf("ParseOutput system returned error: %v", err)
	}
	if systemEvent.Type != runEventSystem {
		t.Fatalf("expected system event, got %#v", systemEvent)
	}
	if !containsText(systemEvent.Text, "session") || !containsText(systemEvent.Text, "cwd=") {
		t.Fatalf("expected summarized system message, got %q", systemEvent.Text)
	}
}

func TestClaudeProviderParseNestedSystemMessage(t *testing.T) {
	provider := claudeProvider{}

	systemEvent, err := provider.ParseOutput(`{"type":"system","message":{"session_id":"abc123456789","cwd":"/tmp/project","model":"claude-opus"}}`)
	if err != nil {
		t.Fatalf("ParseOutput nested system returned error: %v", err)
	}
	if systemEvent.Type != runEventSystem {
		t.Fatalf("expected system event, got %#v", systemEvent)
	}
	if !containsText(systemEvent.Text, "session") || !containsText(systemEvent.Text, "cwd=") {
		t.Fatalf("expected summarized nested system message, got %q", systemEvent.Text)
	}
}
