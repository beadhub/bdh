package commands

import (
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

	if !strings.Contains(output, "Unread chat") {
		t.Errorf("Expected 'Unread chat' in output, got: %s", output)
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
