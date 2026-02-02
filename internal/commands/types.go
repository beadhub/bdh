package commands

// GoneWorkspace represents a workspace that was cleaned up because its worktree path no longer exists.
type GoneWorkspace struct {
	WorkspaceID   string `json:"workspace_id"`
	Alias         string `json:"alias"`
	WorkspacePath string `json:"workspace_path"`
}

// PendingConversation is a minimal view of a chat session with unread messages.
type PendingConversation struct {
	SessionID     string   `json:"session_id"`
	Participants  []string `json:"participants"`
	LastMessage   string   `json:"last_message"`
	LastFrom      string   `json:"last_from"`
	UnreadCount   int      `json:"unread_count"`
	LastActivity  string   `json:"last_activity"`
	SenderWaiting bool     `json:"sender_waiting"`
}
