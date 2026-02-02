package commands

import (
	"strings"
	"testing"
)

func TestFormatNotifications_ShowsWaiting(t *testing.T) {
	ctx := &NotificationContext{
		PendingConversations: []PendingConversation{
			{LastFrom: "alice", SenderWaiting: true},
		},
	}

	out := FormatNotifications(ctx, "")
	if !strings.Contains(out, "**URGENT**: alice is waiting for your response") {
		t.Errorf("expected URGENT notification, got: %q", out)
	}
	if !strings.Contains(out, "→ Respond now: `bdh :aweb chat --reply alice \"your reply\"`") {
		t.Errorf("expected response hint, got: %q", out)
	}
}

func TestFormatNotifications_ShowsChats(t *testing.T) {
	ctx := &NotificationContext{
		PendingConversations: []PendingConversation{
			{LastFrom: "alice", SenderWaiting: false},
			{LastFrom: "bob", SenderWaiting: false},
		},
	}

	out := FormatNotifications(ctx, "")
	if !strings.Contains(out, "**CHAT**: You have 2 unread conversations") {
		t.Errorf("expected CHAT notification, got: %q", out)
	}
}

func TestFormatNotifications_ShowsMail(t *testing.T) {
	ctx := &NotificationContext{
		MessagesWaiting: 3,
		CurrentAlias:    "bob-coordinator",
	}

	out := FormatNotifications(ctx, "")
	if !strings.Contains(out, "**MAIL**: You have 3 unread messages") {
		t.Errorf("expected MAIL notification, got: %q", out)
	}
}

func TestFormatNotifications_MailSingular(t *testing.T) {
	ctx := &NotificationContext{
		MessagesWaiting: 1,
	}

	out := FormatNotifications(ctx, "")
	if !strings.Contains(out, "**MAIL**: You have 1 unread message") {
		t.Errorf("expected singular MAIL, got: %q", out)
	}
}

func TestFormatNotifications_ExcludesAlias(t *testing.T) {
	ctx := &NotificationContext{
		PendingConversations: []PendingConversation{
			{LastFrom: "alice", SenderWaiting: true, Participants: []string{"me", "alice"}},
			{LastFrom: "bob", SenderWaiting: true, Participants: []string{"me", "bob"}},
		},
	}

	out := FormatNotifications(ctx, "alice")
	if strings.Contains(out, "alice") {
		t.Errorf("expected alice to be excluded, got: %q", out)
	}
	if !strings.Contains(out, "bob") {
		t.Errorf("expected bob to be included, got: %q", out)
	}
}

func TestFormatNotifications_Empty(t *testing.T) {
	ctx := &NotificationContext{}

	out := FormatNotifications(ctx, "")
	if out != "" {
		t.Errorf("expected empty output, got: %q", out)
	}
}

func TestFormatGoneWorkspaces(t *testing.T) {
	gone := []GoneWorkspace{
		{Alias: "old-agent", WorkspacePath: "/tmp/old"},
	}

	out := FormatGoneWorkspaces(gone)
	if !strings.Contains(out, "Cleaned up gone workspaces:") {
		t.Errorf("expected header, got: %q", out)
	}
	if !strings.Contains(out, "old-agent (/tmp/old)") {
		t.Errorf("expected workspace entry, got: %q", out)
	}
}

func TestFormatGoneWorkspaces_Empty(t *testing.T) {
	out := FormatGoneWorkspaces(nil)
	if out != "" {
		t.Errorf("expected empty output, got: %q", out)
	}
}

func TestSetExcludeChatAlias(t *testing.T) {
	// Reset state
	notificationsMu.Lock()
	excludeChatAlias = ""
	notificationsMu.Unlock()

	SetExcludeChatAlias("alice")

	notificationsMu.Lock()
	got := excludeChatAlias
	notificationsMu.Unlock()

	if got != "alice" {
		t.Errorf("expected excludeChatAlias to be 'alice', got: %q", got)
	}
}
