package commands

import "testing"

func TestCodexProviderBuildCommand(t *testing.T) {
	provider := codexProvider{}

	command, err := provider.BuildCommand("fix the bug", runBuildOptions{
		Model: "gpt-5-codex",
	})
	if err != nil {
		t.Fatalf("BuildCommand returned error: %v", err)
	}

	joined := joinArgs(command)
	if !containsText(joined, "codex exec --json -m gpt-5-codex fix the bug") {
		t.Fatalf("unexpected codex command: %q", joined)
	}
	if containsText(joined, "resume --last") {
		t.Fatalf("did not expect resume mode by default: %q", joined)
	}
}

func TestCodexProviderBuildCommandWithContinue(t *testing.T) {
	provider := codexProvider{}

	command, err := provider.BuildCommand("continue working", runBuildOptions{
		ContinueSession: true,
	})
	if err != nil {
		t.Fatalf("BuildCommand returned error: %v", err)
	}

	joined := joinArgs(command)
	if !containsText(joined, "codex exec resume --last --json continue working") {
		t.Fatalf("expected resume --last codex command, got: %q", joined)
	}
}

func TestCodexProviderBuildCommandWithSessionID(t *testing.T) {
	provider := codexProvider{}

	command, err := provider.BuildCommand("continue working", runBuildOptions{
		SessionID: "019ccab4-4844-7ff3-80f2-b2d3b0c25e79",
	})
	if err != nil {
		t.Fatalf("BuildCommand returned error: %v", err)
	}

	joined := joinArgs(command)
	if !containsText(joined, "codex exec resume 019ccab4-4844-7ff3-80f2-b2d3b0c25e79 --json continue working") {
		t.Fatalf("expected explicit codex resume id, got: %q", joined)
	}
	if containsText(joined, "--last") {
		t.Fatalf("did not expect --last when an exact session id is set, got: %q", joined)
	}
}

func TestCodexProviderBuildCommandWithContinueAndSessionIDPrefersExactResume(t *testing.T) {
	provider := codexProvider{}

	command, err := provider.BuildCommand("continue working", runBuildOptions{
		SessionID:       "019ccab4-4844-7ff3-80f2-b2d3b0c25e79",
		ContinueSession: true,
	})
	if err != nil {
		t.Fatalf("BuildCommand returned error: %v", err)
	}

	joined := joinArgs(command)
	if !containsText(joined, "codex exec resume 019ccab4-4844-7ff3-80f2-b2d3b0c25e79 --json continue working") {
		t.Fatalf("expected explicit codex resume id, got: %q", joined)
	}
	if containsText(joined, "--last") {
		t.Fatalf("did not expect --last when an exact session id is set, got: %q", joined)
	}
}

func TestCodexProviderRejectsAllowedTools(t *testing.T) {
	provider := codexProvider{}

	_, err := provider.BuildCommand("continue working", runBuildOptions{
		AllowedTools: "Bash",
	})
	if err == nil {
		t.Fatal("expected allowed-tools error for codex")
	}
}

func TestCodexProviderParseOutput(t *testing.T) {
	provider := codexProvider{}

	systemEvent, err := provider.ParseOutput(`{"type":"thread.started","thread_id":"019cca9b-364c-7c81-ae75-4fb21c9c5a4d"}`)
	if err != nil {
		t.Fatalf("ParseOutput thread.started returned error: %v", err)
	}
	if systemEvent.Type != runEventSystem || provider.SessionID(systemEvent) == "" {
		t.Fatalf("unexpected thread.started event: %#v", systemEvent)
	}

	toolCallEvent, err := provider.ParseOutput(`{"type":"item.started","item":{"id":"item_0","type":"command_execution","command":"/bin/zsh -lc pwd","aggregated_output":"","exit_code":null,"status":"in_progress"}}`)
	if err != nil {
		t.Fatalf("ParseOutput item.started returned error: %v", err)
	}
	if toolCallEvent.Type != runEventToolCall || len(toolCallEvent.ToolCalls) != 1 {
		t.Fatalf("unexpected tool call event: %#v", toolCallEvent)
	}
	if toolCallEvent.ToolCalls[0].Name != "Bash" {
		t.Fatalf("expected Bash tool call, got %#v", toolCallEvent.ToolCalls[0])
	}
	if got := toolCallEvent.ToolCalls[0].Input["command"]; got != "pwd" {
		t.Fatalf("expected shell wrapper to be stripped, got %#v", got)
	}

	toolResultEvent, err := provider.ParseOutput(`{"type":"item.completed","item":{"id":"item_0","type":"command_execution","command":"/bin/zsh -lc pwd","aggregated_output":"/tmp/work\n","exit_code":0,"status":"completed"}}`)
	if err != nil {
		t.Fatalf("ParseOutput command_execution completion returned error: %v", err)
	}
	if toolResultEvent.Type != runEventToolResult || toolResultEvent.Text != "/tmp/work" {
		t.Fatalf("unexpected tool result event: %#v", toolResultEvent)
	}

	textEvent, err := provider.ParseOutput(`{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"hello"}}`)
	if err != nil {
		t.Fatalf("ParseOutput agent_message returned error: %v", err)
	}
	if textEvent.Type != runEventText || textEvent.Text != "hello" {
		t.Fatalf("unexpected agent message event: %#v", textEvent)
	}

	doneEvent, err := provider.ParseOutput(`{"type":"turn.completed","usage":{"input_tokens":10663,"cached_input_tokens":8832,"output_tokens":32}}`)
	if err != nil {
		t.Fatalf("ParseOutput turn.completed returned error: %v", err)
	}
	if doneEvent.Type != runEventDone {
		t.Fatalf("unexpected done event: %#v", doneEvent)
	}
	if doneEvent.Usage != nil {
		t.Fatalf("did not expect usage from codex stdout turn.completed event, got %#v", doneEvent.Usage)
	}
}
