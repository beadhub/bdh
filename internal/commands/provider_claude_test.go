package commands

import "testing"

func TestClaudeProviderBuildCommand(t *testing.T) {
	provider := claudeProvider{}

	command, err := provider.BuildCommand("fix the bug", runBuildOptions{
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
	if containsText(joined, "--continue") {
		t.Fatalf("did not expect continue flag by default, got: %q", joined)
	}
	if !containsText(joined, "--allowedTools exec_command,apply_patch") {
		t.Fatalf("expected allowedTools flag, got: %q", joined)
	}
	if !containsText(joined, "--model claude-sonnet-4") {
		t.Fatalf("expected model flag, got: %q", joined)
	}
}

func TestClaudeProviderBuildCommandWithContinue(t *testing.T) {
	provider := claudeProvider{}

	command, err := provider.BuildCommand("fix the bug", runBuildOptions{
		ContinueSession: true,
	})
	if err != nil {
		t.Fatalf("BuildCommand returned error: %v", err)
	}

	joined := joinArgs(command)
	if !containsText(joined, "--continue") {
		t.Fatalf("expected continue flag, got: %q", joined)
	}
}

func TestClaudeProviderBuildCommandWithSessionID(t *testing.T) {
	provider := claudeProvider{}

	command, err := provider.BuildCommand("fix the bug", runBuildOptions{
		SessionID: "sess-1",
	})
	if err != nil {
		t.Fatalf("BuildCommand returned error: %v", err)
	}

	joined := joinArgs(command)
	if !containsText(joined, "--resume sess-1") {
		t.Fatalf("expected explicit resume flag, got: %q", joined)
	}
	if containsText(joined, "--continue") {
		t.Fatalf("did not expect continue flag when session id is set, got: %q", joined)
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
	if toolEvent.Usage != nil {
		t.Fatalf("expected no usage stats on bare tool event, got %#v", toolEvent.Usage)
	}

	usageEvent, err := provider.ParseOutput(`{"type":"assistant","message":{"model":"claude-opus-4-6","usage":{"input_tokens":12000,"cache_creation_input_tokens":5000,"cache_read_input_tokens":18000,"output_tokens":800},"content":[]}}`)
	if err != nil {
		t.Fatalf("ParseOutput usage assistant returned error: %v", err)
	}
	if usageEvent.Usage == nil {
		t.Fatal("expected usage stats on assistant event")
	}
	if usageEvent.Usage.TotalInput() != 35000 {
		t.Fatalf("expected total input 35000, got %d", usageEvent.Usage.TotalInput())
	}
	if usageEvent.Usage.ContextWindowSize != 200000 {
		t.Fatalf("expected 200000 context window, got %d", usageEvent.Usage.ContextWindowSize)
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

	errorResultEvent, err := provider.ParseOutput(`{"type":"result","duration_ms":320,"total_cost_usd":0,"session_id":"s1","is_error":true,"result":"You've hit your limit"}`)
	if err != nil {
		t.Fatalf("ParseOutput error result returned error: %v", err)
	}
	if errorResultEvent.Type != runEventDone || !errorResultEvent.IsError {
		t.Fatalf("expected error result event, got %#v", errorResultEvent)
	}
	if errorResultEvent.Text != "You've hit your limit" {
		t.Fatalf("unexpected error result text: %#v", errorResultEvent.Text)
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

func TestRunUsageStatsContextPct(t *testing.T) {
	stats := runUsageStats{
		InputTokens:              12000,
		CacheCreationInputTokens: 5000,
		CacheReadInputTokens:     18000,
		ContextWindowSize:        200000,
	}

	if got := stats.TotalInput(); got != 35000 {
		t.Fatalf("expected total input 35000, got %d", got)
	}
	if got := stats.ContextPct(); got != 17.5 {
		t.Fatalf("expected context pct 17.5, got %v", got)
	}
}
