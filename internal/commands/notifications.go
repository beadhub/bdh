package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	aweb "github.com/awebai/aw"
	"github.com/beadhub/bdh/internal/client"
	"github.com/beadhub/bdh/internal/config"
)

// NotificationContext contains all notification data fetched from BeadHub.
type NotificationContext struct {
	PendingConversations []PendingConversation
	MessagesWaiting      int
	GoneWorkspaces       []GoneWorkspace
	CurrentAlias         string
	Warning              string
}

// Coordination header state
//
// The coordination header "# Coordination Info for [alias] (you, the agent)"
// should appear exactly once before any coordination sections (Team Status,
// Your Claims, Notifications, etc).
//
// Lifecycle:
//  1. Command calls SetCoordinationHeaderAlias(alias) early in execution
//  2. Formatting functions call FormatCoordinationHeader() before their first section
//     - First call returns the header, subsequent calls return ""
//  3. PrintNotifications (called at end of every command) handles two cases:
//     - If header was already printed by command: just print notifications
//     - If no header set (command had no coordination output): set it up and print
//  4. ResetCoordinationHeader() clears state for next command
var (
	notificationsMu           sync.Mutex
	excludeChatAlias          string
	coordinationAlias         string // Agent alias for the header (empty = not set)
	coordinationHeaderPrinted bool   // True after header has been output
)

// SetExcludeChatAlias sets an alias to exclude from chat notifications.
// Used by :chat command to avoid showing "you have a chat with X" when already chatting with X.
func SetExcludeChatAlias(alias string) {
	notificationsMu.Lock()
	excludeChatAlias = alias
	notificationsMu.Unlock()
}

// SetCoordinationHeaderAlias enables the coordination header for this command.
// Call this early in command execution, before any coordination output.
func SetCoordinationHeaderAlias(alias string) {
	notificationsMu.Lock()
	coordinationAlias = alias
	coordinationHeaderPrinted = false
	notificationsMu.Unlock()
}

// FormatCoordinationHeader returns the header on first call, empty string thereafter.
// Safe to call multiple times - only the first call produces output.
func FormatCoordinationHeader() string {
	notificationsMu.Lock()
	defer notificationsMu.Unlock()

	if coordinationHeaderPrinted || coordinationAlias == "" {
		return ""
	}

	header := fmt.Sprintf("\n# Coordination Info for %s (you, the agent)\n", coordinationAlias)
	coordinationHeaderPrinted = true
	return header
}

// ResetCoordinationHeader clears state for next command.
func ResetCoordinationHeader() {
	notificationsMu.Lock()
	coordinationAlias = ""
	coordinationHeaderPrinted = false
	notificationsMu.Unlock()
}

// FetchNotifications retrieves all notification data from BeadHub.
func FetchNotifications(cfg *config.Config) *NotificationContext {
	ctx := &NotificationContext{
		CurrentAlias: cfg.Alias,
	}

	c := newBeadHubClient(cfg.BeadhubURL)
	aw, _ := newAwebClient(cfg.BeadhubURL)

	// Refresh presence
	refreshPresenceHeartbeat(cfg)

	// Fetch pending chats (best-effort)
	if aw != nil {
		pendingCtx, pendingCancel := context.WithTimeout(context.Background(), apiTimeout)
		pendingResp, err := aw.ChatPending(pendingCtx)
		pendingCancel()
		if err != nil {
			ctx.Warning = fmt.Sprintf("Could not check chat notifications: %v", err)
		} else {
			ctx.PendingConversations = make([]PendingConversation, 0, len(pendingResp.Pending))
			for _, p := range pendingResp.Pending {
				ctx.PendingConversations = append(ctx.PendingConversations, PendingConversation{
					SessionID:     p.SessionID,
					Participants:  p.Participants,
					LastMessage:   p.LastMessage,
					LastFrom:      p.LastFrom,
					UnreadCount:   p.UnreadCount,
					LastActivity:  p.LastActivity,
					SenderWaiting: p.SenderWaiting,
				})
			}
		}

		// Fetch unread mail count (best-effort).
		mailCtx, mailCancel := context.WithTimeout(context.Background(), apiTimeout)
		inboxResp, mailErr := aw.Inbox(mailCtx, aweb.InboxParams{
			UnreadOnly: true,
			Limit:      500,
		})
		mailCancel()
		if mailErr == nil && inboxResp != nil {
			ctx.MessagesWaiting = len(inboxResp.Messages)
		}
	}

	// Detect and clean gone workspaces
	ctx.GoneWorkspaces = detectGoneWorkspaces(cfg, c)

	return ctx
}

// detectGoneWorkspaces checks for workspaces on this hostname whose paths no longer exist.
func detectGoneWorkspaces(cfg *config.Config, c *client.Client) []GoneWorkspace {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		return nil
	}

	listCtx, listCancel := context.WithTimeout(context.Background(), apiTimeout)
	defer listCancel()

	includePresence := false
	resp, err := c.Workspaces(listCtx, &client.WorkspacesRequest{
		Hostname:        hostname,
		IncludePresence: &includePresence,
	})
	if err != nil {
		return nil
	}

	var gone []GoneWorkspace
	for _, ws := range resp.Workspaces {
		if ws.WorkspacePath == "" || ws.WorkspaceID == cfg.WorkspaceID {
			continue
		}
		if _, err := os.Stat(ws.WorkspacePath); os.IsNotExist(err) {
			deleteCtx, deleteCancel := context.WithTimeout(context.Background(), apiTimeout)
			_, deleteErr := c.DeleteWorkspace(deleteCtx, ws.WorkspaceID)
			deleteCancel()
			if deleteErr == nil {
				gone = append(gone, GoneWorkspace{
					WorkspaceID:   ws.WorkspaceID,
					Alias:         ws.Alias,
					WorkspacePath: ws.WorkspacePath,
				})
			}
		}
	}
	return gone
}

// FormatNotifications formats the notifications section for the agent.
// Uses second-person language to make it explicit these are for the agent to act on.
func FormatNotifications(ctx *NotificationContext, excludeAlias string) string {
	exclude := make(map[string]struct{})
	if excludeAlias != "" {
		exclude[excludeAlias] = struct{}{}
	}

	involvesExcluded := func(conv PendingConversation) bool {
		if len(exclude) == 0 {
			return false
		}
		if _, ok := exclude[conv.LastFrom]; ok {
			return true
		}
		for _, p := range conv.Participants {
			if _, ok := exclude[p]; ok {
				return true
			}
		}
		return false
	}

	var lines []string

	// URGENT: sender actively waiting for response
	seen := make(map[string]struct{})
	for _, conv := range ctx.PendingConversations {
		if !conv.SenderWaiting || involvesExcluded(conv) {
			continue
		}
		alias := strings.TrimSpace(conv.LastFrom)
		if alias == "" {
			continue
		}
		if _, ok := seen[alias]; ok {
			continue
		}
		seen[alias] = struct{}{}
		lines = append(lines, fmt.Sprintf("- **URGENT**: %s is waiting for your response\n  → Respond now: `bdh :aweb chat send %s \"your reply\"`", alias, alias))
	}

	// CHATS: non-waiting with unread
	nonWaiting := 0
	for _, conv := range ctx.PendingConversations {
		if !conv.SenderWaiting && !involvesExcluded(conv) {
			nonWaiting++
		}
	}
	if nonWaiting == 1 {
		lines = append(lines, "- **CHAT**: You have 1 unread conversation\n  → Check: `bdh :aweb chat pending`")
	} else if nonWaiting > 1 {
		lines = append(lines, fmt.Sprintf("- **CHAT**: You have %d unread conversations\n  → Check: `bdh :aweb chat pending`", nonWaiting))
	}

	// MAIL
	if ctx.MessagesWaiting > 0 {
		if ctx.MessagesWaiting == 1 {
			lines = append(lines, "- **MAIL**: You have 1 unread message\n  → Check: `bdh :aweb mail list`")
		} else {
			lines = append(lines, fmt.Sprintf("- **MAIL**: You have %d unread messages\n  → Check: `bdh :aweb mail list`", ctx.MessagesWaiting))
		}
	}

	if len(lines) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n## Your Notifications\n")
	for _, line := range lines {
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	return sb.String()
}

// FormatGoneWorkspaces formats the gone workspaces message.
func FormatGoneWorkspaces(gone []GoneWorkspace) string {
	if len(gone) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("Cleaned up gone workspaces:\n")
	for _, gw := range gone {
		sb.WriteString(fmt.Sprintf("  %s (%s)\n", gw.Alias, gw.WorkspacePath))
	}
	sb.WriteString("\n")
	return sb.String()
}

// PrintNotifications fetches and prints notifications.
// This is the single entry point called by main.go at the end of every command.
func PrintNotifications(w io.Writer) {
	notificationsMu.Lock()
	exclude := excludeChatAlias
	excludeChatAlias = ""
	notificationsMu.Unlock()

	// Load config - if not initialized, silently skip
	cfg, err := config.Load()
	if err != nil {
		ResetCoordinationHeader()
		return
	}
	if cfg.Validate() != nil {
		ResetCoordinationHeader()
		return
	}

	ctx := FetchNotifications(cfg)

	// Print gone workspaces
	if gone := FormatGoneWorkspaces(ctx.GoneWorkspaces); gone != "" {
		_, _ = io.WriteString(w, gone)
	}

	// Print notifications with coordination header
	if out := FormatNotifications(ctx, exclude); out != "" {
		// Commands without coordination sections don't set up the header.
		// Set it up now so notifications print under the coordination header.
		notificationsMu.Lock()
		headerNotSetUp := coordinationAlias == ""
		notificationsMu.Unlock()
		if headerNotSetUp {
			SetCoordinationHeaderAlias(cfg.Alias)
		}

		// Print header (no-op if already printed by command)
		if header := FormatCoordinationHeader(); header != "" {
			_, _ = io.WriteString(w, header)
		}
		_, _ = io.WriteString(w, out)
	}

	ResetCoordinationHeader()
}
