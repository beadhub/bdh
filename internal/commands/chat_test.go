// ABOUTME: Tests for chat formatting/presentation functions.
// ABOUTME: Validates text and JSON output for all chat result types.

package commands

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/awebai/aw/chat"
)

func TestFormatChatOutput_Replied(t *testing.T) {
	result := &chat.SendResult{
		SessionID:   "s1",
		Status:      "replied",
		TargetAgent: "bob",
		Reply:       "Sure, I can help!",
		Events: []chat.Event{
			{Type: "message", FromAgent: "bob", Body: "Sure, I can help!", Timestamp: "2025-06-15T10:30:00Z"},
		},
	}

	out := formatChatOutput(result, false)
	if !strings.Contains(out, "Chat from: bob") {
		t.Errorf("expected 'Chat from: bob', got: %q", out)
	}
	if !strings.Contains(out, "Body: Sure, I can help!") {
		t.Errorf("expected reply body, got: %q", out)
	}
}

func TestFormatChatOutput_SenderLeft(t *testing.T) {
	result := &chat.SendResult{
		SessionID:   "s1",
		Status:      "sender_left",
		TargetAgent: "bob",
		Reply:       "Goodbye!",
		Events: []chat.Event{
			{Type: "message", FromAgent: "bob", Body: "Goodbye!"},
		},
	}

	out := formatChatOutput(result, false)
	if !strings.Contains(out, "Body: Goodbye!") {
		t.Errorf("expected reply body, got: %q", out)
	}
	if !strings.Contains(out, "bob has left the exchange") {
		t.Errorf("expected left note, got: %q", out)
	}
}

func TestFormatChatOutput_Sent(t *testing.T) {
	result := &chat.SendResult{
		SessionID:   "s1",
		Status:      "sent",
		TargetAgent: "bob",
	}

	out := formatChatOutput(result, false)
	if !strings.Contains(out, "Message sent to bob") {
		t.Errorf("expected sent message, got: %q", out)
	}
}

func TestFormatChatOutput_SentNotConnected(t *testing.T) {
	result := &chat.SendResult{
		SessionID:          "s1",
		Status:             "sent",
		TargetAgent:        "bob",
		TargetNotConnected: true,
		WaitedSeconds:      30,
	}

	out := formatChatOutput(result, false)
	if !strings.Contains(out, "bob was not connected") {
		t.Errorf("expected not-connected note, got: %q", out)
	}
	if !strings.Contains(out, "Waited 30s") {
		t.Errorf("expected wait info, got: %q", out)
	}
}

func TestFormatChatOutput_TargetsLeft(t *testing.T) {
	result := &chat.SendResult{
		SessionID:   "s1",
		Status:      "targets_left",
		TargetAgent: "bob",
	}

	out := formatChatOutput(result, false)
	if !strings.Contains(out, "previously left") {
		t.Errorf("expected left note, got: %q", out)
	}
	if !strings.Contains(out, "--start-conversation") {
		t.Errorf("expected restart hint, got: %q", out)
	}
}

func TestFormatChatOutput_Pending_FromTarget(t *testing.T) {
	result := &chat.SendResult{
		SessionID:     "s1",
		Status:        "pending",
		TargetAgent:   "bob",
		Reply:         "Hello?",
		SenderWaiting: true,
		Events: []chat.Event{
			{Type: "message", FromAgent: "bob", Body: "Hello?"},
		},
	}

	out := formatChatOutput(result, false)
	if !strings.Contains(out, "Chat from: bob") {
		t.Errorf("expected 'Chat from: bob', got: %q", out)
	}
	if !strings.Contains(out, "WAITING for your reply") {
		t.Errorf("expected waiting status, got: %q", out)
	}
	if !strings.Contains(out, "chat send bob") {
		t.Errorf("expected send hint, got: %q", out)
	}
}

func TestFormatChatOutput_Pending_FromSelf(t *testing.T) {
	result := &chat.SendResult{
		SessionID:   "s1",
		Status:      "pending",
		TargetAgent: "bob",
		Reply:       "My question",
		Events: []chat.Event{
			{Type: "message", FromAgent: "alice", Body: "My question"},
		},
	}

	out := formatChatOutput(result, false)
	if !strings.Contains(out, "Chat to: bob") {
		t.Errorf("expected 'Chat to: bob', got: %q", out)
	}
	if !strings.Contains(out, "Awaiting reply from bob") {
		t.Errorf("expected awaiting note, got: %q", out)
	}
}

func TestFormatChatOutput_JSON(t *testing.T) {
	result := &chat.SendResult{
		SessionID:   "s1",
		Status:      "replied",
		TargetAgent: "bob",
		Reply:       "OK",
		Events:      []chat.Event{},
	}

	out := formatChatOutput(result, true)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed["status"] != "replied" {
		t.Errorf("expected status=replied, got: %v", parsed["status"])
	}
	if parsed["session_id"] != "s1" {
		t.Errorf("expected session_id=s1, got: %v", parsed["session_id"])
	}
}

func TestFormatChatOutput_NoEvents(t *testing.T) {
	result := &chat.SendResult{
		SessionID:   "s1",
		Status:      "unknown",
		TargetAgent: "bob",
		Events:      []chat.Event{},
	}

	out := formatChatOutput(result, false)
	if !strings.Contains(out, "No chat events") {
		t.Errorf("expected fallback message, got: %q", out)
	}
}

func TestFormatPendingOutput_WithWaiting(t *testing.T) {
	timeLeft := 45
	result := &chat.PendingResult{
		Pending: []chat.PendingConversation{
			{
				SessionID:            "s1",
				Participants:         []string{"me", "alice"},
				LastFrom:             "alice",
				UnreadCount:          3,
				SenderWaiting:        true,
				TimeRemainingSeconds: &timeLeft,
			},
		},
	}

	out := formatPendingOutput(result, "me", false)
	if !strings.Contains(out, "CHAT WAITING: alice") {
		t.Errorf("expected CHAT WAITING, got: %q", out)
	}
	if !strings.Contains(out, "(45s left)") {
		t.Errorf("expected time remaining, got: %q", out)
	}
	if !strings.Contains(out, "(unread: 3)") {
		t.Errorf("expected unread count, got: %q", out)
	}
	if !strings.Contains(out, "chat open alice") {
		t.Errorf("expected open hint, got: %q", out)
	}
}

func TestFormatPendingOutput_NoWaiting(t *testing.T) {
	result := &chat.PendingResult{
		Pending: []chat.PendingConversation{
			{
				SessionID:    "s1",
				Participants: []string{"me", "bob"},
				LastFrom:     "bob",
				UnreadCount:  1,
			},
		},
	}

	out := formatPendingOutput(result, "me", false)
	if !strings.Contains(out, "CHAT: bob") {
		t.Errorf("expected CHAT line, got: %q", out)
	}
	if strings.Contains(out, "WAITING") {
		t.Errorf("should not contain WAITING, got: %q", out)
	}
}

func TestFormatPendingOutput_TimeRemainingNotUrgent(t *testing.T) {
	timeLeft := 120
	result := &chat.PendingResult{
		Pending: []chat.PendingConversation{
			{
				SessionID:            "s1",
				Participants:         []string{"me", "alice"},
				LastFrom:             "alice",
				UnreadCount:          1,
				SenderWaiting:        true,
				TimeRemainingSeconds: &timeLeft,
			},
		},
	}

	out := formatPendingOutput(result, "me", false)
	// Time remaining >= 60s should NOT be shown
	if strings.Contains(out, "120s left") {
		t.Errorf("should not show non-urgent time remaining, got: %q", out)
	}
}

func TestFormatPendingOutput_Empty(t *testing.T) {
	result := &chat.PendingResult{
		Pending: []chat.PendingConversation{},
	}

	out := formatPendingOutput(result, "me", false)
	if !strings.Contains(out, "No pending conversations") {
		t.Errorf("expected empty message, got: %q", out)
	}
}

func TestFormatPendingOutput_JSON(t *testing.T) {
	result := &chat.PendingResult{
		Pending: []chat.PendingConversation{
			{
				SessionID:    "s1",
				Participants: []string{"me", "alice"},
				LastMessage:  "secret message body",
				LastFrom:     "alice",
				UnreadCount:  1,
			},
		},
	}

	out := formatPendingOutput(result, "me", true)

	// JSON output should NOT include message bodies (discovery only)
	if strings.Contains(out, "secret message body") {
		t.Errorf("JSON pending output should not include message bodies, got: %q", out)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	pending := parsed["pending"].([]any)
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}
	item := pending[0].(map[string]any)
	if item["last_from"] != "alice" {
		t.Errorf("expected last_from=alice, got: %v", item["last_from"])
	}
}

func TestFormatHistoryOutput_Text(t *testing.T) {
	result := &chat.HistoryResult{
		SessionID: "s1",
		Messages: []chat.Event{
			{Type: "message", FromAgent: "alice", Body: "Hello", Timestamp: "2025-06-15T10:30:00Z"},
			{Type: "message", FromAgent: "bob", Body: "Hi there", Timestamp: "2025-06-15T10:31:00Z"},
		},
	}

	out := formatHistoryOutput(result, false)
	if !strings.Contains(out, "Conversation history (2 messages)") {
		t.Errorf("expected header, got: %q", out)
	}
	if !strings.Contains(out, "[10:30:00] alice: Hello") {
		t.Errorf("expected first message, got: %q", out)
	}
	if !strings.Contains(out, "[10:31:00] bob: Hi there") {
		t.Errorf("expected second message, got: %q", out)
	}
}

func TestFormatHistoryOutput_Empty(t *testing.T) {
	result := &chat.HistoryResult{
		SessionID: "s1",
		Messages:  []chat.Event{},
	}

	out := formatHistoryOutput(result, false)
	if !strings.Contains(out, "No messages in conversation") {
		t.Errorf("expected empty message, got: %q", out)
	}
}

func TestFormatHistoryOutput_NoTimestamp(t *testing.T) {
	result := &chat.HistoryResult{
		SessionID: "s1",
		Messages: []chat.Event{
			{Type: "message", FromAgent: "alice", Body: "Hello"},
		},
	}

	out := formatHistoryOutput(result, false)
	// Should show message without timestamp brackets
	if !strings.Contains(out, "alice: Hello") {
		t.Errorf("expected message without timestamp, got: %q", out)
	}
	if strings.Contains(out, "[") {
		t.Errorf("should not have timestamp brackets, got: %q", out)
	}
}

func TestFormatChatOpenOutput_WithMessages(t *testing.T) {
	result := &chat.OpenResult{
		SessionID:     "s1",
		TargetAgent:   "alice",
		MarkedRead:    2,
		SenderWaiting: true,
		Messages: []chat.Event{
			{Type: "message", FromAgent: "alice", Body: "Can you help?", Timestamp: "2025-06-15T10:30:00Z"},
			{Type: "message", FromAgent: "alice", Body: "It's urgent", Timestamp: "2025-06-15T10:31:00Z"},
		},
	}

	out := formatChatOpenOutput(result, false)
	if !strings.Contains(out, "2 marked as read") {
		t.Errorf("expected marked read count, got: %q", out)
	}
	if !strings.Contains(out, "alice is WAITING for your reply") {
		t.Errorf("expected waiting status, got: %q", out)
	}
	if !strings.Contains(out, "Can you help?") {
		t.Errorf("expected first message, got: %q", out)
	}
	if !strings.Contains(out, "---") {
		t.Errorf("expected separator between messages, got: %q", out)
	}
	if !strings.Contains(out, "chat send alice") {
		t.Errorf("expected send hint, got: %q", out)
	}
	if !strings.Contains(out, "chat hang-on") {
		t.Errorf("expected hang-on hint for waiting sender, got: %q", out)
	}
}

func TestFormatChatOpenOutput_Empty(t *testing.T) {
	result := &chat.OpenResult{
		SessionID:      "s1",
		TargetAgent:    "alice",
		UnreadWasEmpty: true,
		Messages:       []chat.Event{},
	}

	out := formatChatOpenOutput(result, false)
	if !strings.Contains(out, "No unread chat messages for alice") {
		t.Errorf("expected empty message, got: %q", out)
	}
}

func TestFormatChatOpenOutput_NotWaiting(t *testing.T) {
	result := &chat.OpenResult{
		SessionID:     "s1",
		TargetAgent:   "bob",
		MarkedRead:    1,
		SenderWaiting: false,
		Messages: []chat.Event{
			{Type: "message", FromAgent: "bob", Body: "FYI"},
		},
	}

	out := formatChatOpenOutput(result, false)
	if strings.Contains(out, "WAITING") {
		t.Errorf("should not show WAITING when sender is not waiting, got: %q", out)
	}
	if strings.Contains(out, "chat hang-on") {
		t.Errorf("should not suggest hang-on when not waiting, got: %q", out)
	}
}

func TestFormatHangOnOutput(t *testing.T) {
	result := &chat.HangOnResult{
		SessionID:          "s1",
		TargetAgent:        "bob",
		Message:            "Working on it...",
		ExtendsWaitSeconds: 300,
	}

	out := formatHangOnOutput(result, false)
	if !strings.Contains(out, "Sent hang-on to bob") {
		t.Errorf("expected hang-on header, got: %q", out)
	}
	if !strings.Contains(out, "Message: Working on it...") {
		t.Errorf("expected message, got: %q", out)
	}
	if !strings.Contains(out, "wait extended by 5 min") {
		t.Errorf("expected extension note, got: %q", out)
	}
}

func TestFormatHangOnOutput_NoExtension(t *testing.T) {
	result := &chat.HangOnResult{
		SessionID:          "s1",
		TargetAgent:        "bob",
		Message:            "Thinking...",
		ExtendsWaitSeconds: 0,
	}

	out := formatHangOnOutput(result, false)
	if strings.Contains(out, "extended") {
		t.Errorf("should not show extension when 0, got: %q", out)
	}
}

func TestFormatHangOnOutput_JSON(t *testing.T) {
	result := &chat.HangOnResult{
		SessionID:          "s1",
		TargetAgent:        "bob",
		Message:            "Hold on",
		ExtendsWaitSeconds: 300,
	}

	out := formatHangOnOutput(result, true)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed["target_agent"] != "bob" {
		t.Errorf("expected target_agent=bob, got: %v", parsed["target_agent"])
	}
	if int(parsed["extends_wait_seconds"].(float64)) != 300 {
		t.Errorf("expected extends_wait_seconds=300, got: %v", parsed["extends_wait_seconds"])
	}
}

func TestFormatPendingOutput_FiltersSelf(t *testing.T) {
	result := &chat.PendingResult{
		Pending: []chat.PendingConversation{
			{
				SessionID:    "s1",
				Participants: []string{"me", "alice"},
				LastFrom:     "alice",
				UnreadCount:  1,
			},
		},
	}

	out := formatPendingOutput(result, "me", false)
	// The open hint should point to alice, not "me"
	if !strings.Contains(out, "chat open alice") {
		t.Errorf("expected open hint for alice, got: %q", out)
	}
}
