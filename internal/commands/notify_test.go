package commands

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/awebai/aw/chat"
)

func TestFormatNotifyOutput_NoPending(t *testing.T) {
	result := &chat.PendingResult{
		Pending: nil,
	}

	output := formatNotifyOutput(result, "my-alias")

	if output != "" {
		t.Errorf("Expected empty output for no pending, got: %s", output)
	}
}

func TestFormatNotifyOutput_UrgentChat(t *testing.T) {
	result := &chat.PendingResult{
		Pending: []chat.PendingConversation{
			{
				SessionID:     "sess_123",
				Participants:  []string{"my-alias", "bob"},
				LastFrom:      "bob",
				UnreadCount:   2,
				SenderWaiting: true,
			},
		},
	}

	output := formatNotifyOutput(result, "my-alias")

	if !strings.Contains(output, "URGENT") {
		t.Errorf("Expected URGENT in output, got: %s", output)
	}
	if !strings.Contains(output, "bob") {
		t.Errorf("Expected bob in output, got: %s", output)
	}
	if !strings.Contains(output, "WAITING") {
		t.Errorf("Expected WAITING in output, got: %s", output)
	}
}

func TestFormatNotifyOutput_RegularChat(t *testing.T) {
	result := &chat.PendingResult{
		Pending: []chat.PendingConversation{
			{
				SessionID:     "sess_456",
				Participants:  []string{"my-alias", "alice"},
				LastFrom:      "alice",
				UnreadCount:   1,
				SenderWaiting: false,
			},
		},
	}

	output := formatNotifyOutput(result, "my-alias")

	if !strings.Contains(output, "Unread message") {
		t.Errorf("Expected 'Unread message' in output, got: %s", output)
	}
	if !strings.Contains(output, "alice") {
		t.Errorf("Expected alice in output, got: %s", output)
	}
	if strings.Contains(output, "URGENT") {
		t.Errorf("Did not expect URGENT for non-waiting chat, got: %s", output)
	}
}

func TestFormatNotifyOutput_MixedChats(t *testing.T) {
	result := &chat.PendingResult{
		Pending: []chat.PendingConversation{
			{
				SessionID:     "sess_123",
				Participants:  []string{"my-alias", "bob"},
				LastFrom:      "bob",
				UnreadCount:   2,
				SenderWaiting: true,
			},
			{
				SessionID:     "sess_456",
				Participants:  []string{"my-alias", "alice"},
				LastFrom:      "alice",
				UnreadCount:   1,
				SenderWaiting: false,
			},
		},
	}

	output := formatNotifyOutput(result, "my-alias")

	if !strings.Contains(output, "URGENT") {
		t.Errorf("Expected URGENT in output, got: %s", output)
	}
	if !strings.Contains(output, "bob") {
		t.Errorf("Expected bob in output, got: %s", output)
	}
	if !strings.Contains(output, "alice") {
		t.Errorf("Expected alice in output, got: %s", output)
	}
	if !strings.Contains(output, "bdh :aweb chat pending") {
		t.Errorf("Expected help command in output, got: %s", output)
	}
}

func TestFormatNotifyOutput_FallsBackToParticipant(t *testing.T) {
	result := &chat.PendingResult{
		Pending: []chat.PendingConversation{
			{
				SessionID:     "sess_789",
				Participants:  []string{"my-alias", "charlie"},
				LastFrom:      "", // Empty LastFrom
				UnreadCount:   1,
				SenderWaiting: false,
			},
		},
	}

	output := formatNotifyOutput(result, "my-alias")

	if !strings.Contains(output, "charlie") {
		t.Errorf("Expected charlie (from participants) in output, got: %s", output)
	}
}

func TestFormatHookOutput_ValidJSON(t *testing.T) {
	content := "Test notification content"
	output := formatHookOutput(content)

	// Verify it's valid JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("Output is not valid JSON: %v", err)
	}

	// Verify structure
	hookSpecific, ok := parsed["hookSpecificOutput"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected hookSpecificOutput in output")
	}

	if hookSpecific["hookEventName"] != "PostToolUse" {
		t.Errorf("Expected hookEventName to be PostToolUse, got: %v", hookSpecific["hookEventName"])
	}

	if hookSpecific["additionalContext"] != content {
		t.Errorf("Expected additionalContext to be %q, got: %v", content, hookSpecific["additionalContext"])
	}
}

func TestFormatHookOutput_PreservesContent(t *testing.T) {
	// Test that the notification content is preserved in the JSON
	result := &chat.PendingResult{
		Pending: []chat.PendingConversation{
			{
				SessionID:     "sess_123",
				Participants:  []string{"my-alias", "bob"},
				LastFrom:      "bob",
				UnreadCount:   1,
				SenderWaiting: true,
			},
		},
	}

	notifyContent := formatNotifyOutput(result, "my-alias")
	hookOutput := formatHookOutput(notifyContent)

	// Parse and verify content is preserved
	var parsed map[string]interface{}
	json.Unmarshal([]byte(hookOutput), &parsed)

	hookSpecific := parsed["hookSpecificOutput"].(map[string]interface{})
	additionalContext := hookSpecific["additionalContext"].(string)

	if !strings.Contains(additionalContext, "URGENT") {
		t.Error("Expected URGENT to be preserved in additionalContext")
	}
	if !strings.Contains(additionalContext, "bob") {
		t.Error("Expected bob to be preserved in additionalContext")
	}
}
