package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCommand_Approved(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/bdh/command" {
			t.Errorf("Expected /v1/bdh/command, got %s", r.URL.Path)
		}

		// Verify Authorization header is sent
		if got := r.Header.Get("Authorization"); got != "Bearer aw_sk_test123" {
			t.Errorf("Expected Authorization 'Bearer aw_sk_test123', got '%s'", got)
		}

		var req CommandRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		if req.WorkspaceID != "ws-123" {
			t.Errorf("Expected workspace_id ws-123, got %s", req.WorkspaceID)
		}

		resp := CommandResponse{
			Approved: true,
			Context: &CommandContext{
				MessagesWaiting: 2,
				BeadsInProgress: []BeadInProgress{
					{BeadID: "bd-42", Alias: "other-agent"},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := NewWithAPIKey(server.URL, "aw_sk_test123")
	resp, err := c.Command(context.Background(), &CommandRequest{
		WorkspaceID: "ws-123",
		Alias:       "claude-code",
		HumanName:   "Juan",
		CommandLine: "update bd-42 --status in_progress",
	})

	if err != nil {
		t.Fatalf("Command() error: %v", err)
	}
	if !resp.Approved {
		t.Error("Expected approved=true")
	}
	if resp.Context.MessagesWaiting != 2 {
		t.Errorf("Expected 2 messages waiting, got %d", resp.Context.MessagesWaiting)
	}
}

func TestCommand_Rejected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := CommandResponse{
			Approved: false,
			Reason:   "bd-42 is being worked on by other-agent (Maria)",
			Context: &CommandContext{
				BeadsInProgress: []BeadInProgress{
					{BeadID: "bd-42", Alias: "other-agent", HumanName: "Maria"},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := New(server.URL)
	resp, err := c.Command(context.Background(), &CommandRequest{
		WorkspaceID: "ws-123",
		CommandLine: "update bd-42 --status in_progress",
	})

	if err != nil {
		t.Fatalf("Command() error: %v", err)
	}
	if resp.Approved {
		t.Error("Expected approved=false")
	}
	if resp.Reason == "" {
		t.Error("Expected rejection reason")
	}
}

func TestCommand_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal server error"}`))
	}))
	defer server.Close()

	c := New(server.URL)
	_, err := c.Command(context.Background(), &CommandRequest{
		WorkspaceID: "ws-123",
	})

	if err == nil {
		t.Error("Expected error for 500 response")
	}
}

func TestCommand_Unreachable(t *testing.T) {
	c := New("http://localhost:99999") // Invalid port
	_, err := c.Command(context.Background(), &CommandRequest{
		WorkspaceID: "ws-123",
	})

	if err == nil {
		t.Error("Expected error for unreachable server")
	}
}

func TestSync(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/bdh/sync" {
			t.Errorf("Expected /v1/bdh/sync, got %s", r.URL.Path)
		}

		// Verify Authorization header is sent
		if got := r.Header.Get("Authorization"); got != "Bearer aw_sk_test123" {
			t.Errorf("Expected Authorization 'Bearer aw_sk_test123', got '%s'", got)
		}

		var req SyncRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		if req.IssuesJSONL == "" {
			t.Error("Expected issues_jsonl to be non-empty")
		}

		resp := SyncResponse{
			Synced:      true,
			IssuesCount: 5,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := NewWithAPIKey(server.URL, "aw_sk_test123")
	resp, err := c.Sync(context.Background(), &SyncRequest{
		WorkspaceID: "ws-123",
		Alias:       "claude-code",
		HumanName:   "Juan",
		IssuesJSONL: `{"id":"bd-1","title":"Test"}`,
	})

	if err != nil {
		t.Fatalf("Sync() error: %v", err)
	}
	if !resp.Synced {
		t.Error("Expected synced=true")
	}
	if resp.IssuesCount != 5 {
		t.Errorf("Expected 5 issues, got %d", resp.IssuesCount)
	}
}

func TestEnsureProject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/projects/ensure" {
			t.Errorf("Expected /v1/projects/ensure, got %s", r.URL.Path)
		}

		var req EnsureProjectRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		if req.Slug != "beadhub" {
			t.Errorf("Expected slug beadhub, got %s", req.Slug)
		}

		resp := EnsureProjectResponse{
			ProjectID: "a1b2c3d4-5678-90ab-cdef-1234567890ab",
			Slug:      "beadhub",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := New(server.URL)
	resp, err := c.EnsureProject(context.Background(), &EnsureProjectRequest{
		Slug: "beadhub",
	})

	if err != nil {
		t.Fatalf("EnsureProject() error: %v", err)
	}
	if resp.ProjectID == "" {
		t.Error("Expected project_id to be non-empty")
	}
	if resp.Slug != "beadhub" {
		t.Errorf("Expected slug beadhub, got %s", resp.Slug)
	}
}

func TestActivePolicy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/policies/active" {
			t.Errorf("Expected /v1/policies/active, got %s", r.URL.Path)
		}

		// Verify Authorization header is sent
		if got := r.Header.Get("Authorization"); got != "Bearer aw_sk_test123" {
			t.Errorf("Expected Authorization 'Bearer aw_sk_test123', got '%s'", got)
		}

		if r.URL.Query().Get("role") != "coordinator" {
			t.Errorf("Expected role=coordinator, got %q", r.URL.Query().Get("role"))
		}
		if r.URL.Query().Get("only_selected") != "true" {
			t.Errorf("Expected only_selected=true, got %q", r.URL.Query().Get("only_selected"))
		}

		resp := ActivePolicyResponse{
			PolicyID:  "pol-123",
			ProjectID: "proj-456",
			Version:   3,
			UpdatedAt: "2026-01-02T12:00:00Z",
			Invariants: []PolicyInvariant{
				{ID: "tracking.bdh-only", Title: "Use bdh for tracking", BodyMD: "…"},
			},
			Roles: map[string]PolicyRolePlaybook{
				"coordinator": {Title: "Coordinator", PlaybookMD: "…"},
			},
			SelectedRole: &SelectedPolicyRole{
				Role:       "coordinator",
				Title:      "Coordinator",
				PlaybookMD: "…",
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := NewWithAPIKey(server.URL, "aw_sk_test123")
	resp, err := c.ActivePolicy(context.Background(), &ActivePolicyRequest{
		Role:         "coordinator",
		OnlySelected: true,
	})
	if err != nil {
		t.Fatalf("ActivePolicy() error: %v", err)
	}
	if resp.PolicyID != "pol-123" {
		t.Errorf("Expected policy_id pol-123, got %s", resp.PolicyID)
	}
	if resp.SelectedRole == nil || resp.SelectedRole.Role != "coordinator" {
		t.Fatalf("Expected selected_role coordinator, got %#v", resp.SelectedRole)
	}
}

func TestInbox_UnreadOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/messages/inbox" {
			t.Errorf("Expected /v1/messages/inbox, got %s", r.URL.Path)
		}

		// Check query parameters
		q := r.URL.Query()
		if q.Get("workspace_id") != "ws-123" {
			t.Errorf("Expected workspace_id ws-123, got %s", q.Get("workspace_id"))
		}
		if q.Get("unread_only") != "true" {
			t.Errorf("Expected unread_only true, got %s", q.Get("unread_only"))
		}

		resp := InboxResponse{
			Messages: []Message{
				{
					MessageID:     "msg_abc123",
					FromWorkspace: "x7y8z9w0-1234-5678-90ab-cdef12345678",
					FromAlias:     "backend-bot",
					Subject:       "Need bd-42",
					Body:          "I'm blocked on the dashboard.",
					Priority:      "normal",
					Read:          false,
					CreatedAt:     "2025-12-08T14:02:00Z",
				},
			},
			Count:   1,
			HasMore: false,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := New(server.URL)
	resp, err := c.Inbox(context.Background(), &InboxRequest{
		WorkspaceID: "ws-123",
		UnreadOnly:  true,
	})

	if err != nil {
		t.Fatalf("Inbox() error: %v", err)
	}
	if resp.Count != 1 {
		t.Errorf("Expected 1 message, got %d", resp.Count)
	}
	if len(resp.Messages) != 1 {
		t.Fatalf("Expected 1 message in slice, got %d", len(resp.Messages))
	}
	if resp.Messages[0].MessageID != "msg_abc123" {
		t.Errorf("Expected message_id msg_abc123, got %s", resp.Messages[0].MessageID)
	}
	if resp.Messages[0].FromAlias != "backend-bot" {
		t.Errorf("Expected from_alias backend-bot, got %s", resp.Messages[0].FromAlias)
	}
}

func TestInbox_AllMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("unread_only") != "false" {
			t.Errorf("Expected unread_only false, got %s", q.Get("unread_only"))
		}

		resp := InboxResponse{
			Messages: []Message{
				{MessageID: "msg_1", Read: false},
				{MessageID: "msg_2", Read: true},
			},
			Count:   2,
			HasMore: false,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := New(server.URL)
	resp, err := c.Inbox(context.Background(), &InboxRequest{
		WorkspaceID: "ws-123",
		UnreadOnly:  false,
	})

	if err != nil {
		t.Fatalf("Inbox() error: %v", err)
	}
	if resp.Count != 2 {
		t.Errorf("Expected 2 messages, got %d", resp.Count)
	}
}

func TestInbox_WithLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("limit") != "5" {
			t.Errorf("Expected limit 5, got %s", q.Get("limit"))
		}

		resp := InboxResponse{
			Messages: []Message{},
			Count:    0,
			HasMore:  false,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := New(server.URL)
	_, err := c.Inbox(context.Background(), &InboxRequest{
		WorkspaceID: "ws-123",
		Limit:       5,
	})

	if err != nil {
		t.Fatalf("Inbox() error: %v", err)
	}
}

func TestInbox_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal server error"}`))
	}))
	defer server.Close()

	c := New(server.URL)
	_, err := c.Inbox(context.Background(), &InboxRequest{
		WorkspaceID: "ws-123",
	})

	if err == nil {
		t.Error("Expected error for 500 response")
	}
}

func TestAck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/messages/msg_abc123/ack" {
			t.Errorf("Expected /v1/messages/msg_abc123/ack, got %s", r.URL.Path)
		}

		var req AckRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}
		if req.WorkspaceID != "ws-123" {
			t.Errorf("Expected workspace_id ws-123, got %s", req.WorkspaceID)
		}

		resp := AckResponse{
			MessageID:      "msg_abc123",
			AcknowledgedAt: "2025-12-08T14:03:00Z",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := New(server.URL)
	resp, err := c.Ack(context.Background(), "msg_abc123", &AckRequest{
		WorkspaceID: "ws-123",
	})

	if err != nil {
		t.Fatalf("Ack() error: %v", err)
	}
	if resp.MessageID != "msg_abc123" {
		t.Errorf("Expected message_id msg_abc123, got %s", resp.MessageID)
	}
	if resp.AcknowledgedAt == "" {
		t.Error("Expected acknowledged_at to be non-empty")
	}
}

func TestAck_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error": "message not found"}`))
	}))
	defer server.Close()

	c := New(server.URL)
	_, err := c.Ack(context.Background(), "msg_invalid", &AckRequest{
		WorkspaceID: "ws-123",
	})

	if err == nil {
		t.Error("Expected error for 404 response")
	}
}

func TestPost_ResponseSizeLimiting(t *testing.T) {
	tests := []struct {
		name        string
		responseLen int64
		wantErr     bool
	}{
		{
			name:        "exact max size is accepted",
			responseLen: maxResponseSize,
			wantErr:     false,
		},
		{
			name:        "over max size is rejected",
			responseLen: maxResponseSize + 1,
			wantErr:     true,
		},
		{
			name:        "far over max size is rejected",
			responseLen: maxResponseSize + 1000,
			wantErr:     true,
		},
		{
			name:        "under max size is accepted",
			responseLen: 1000,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Create JSON response with exact byte size using printable chars
				// JSON: {"workspace_id":"XXXX...","alias":"","project_id":"","repo_id":"","created":false}
				base := `{"workspace_id":"","alias":"","project_id":"","repo_id":"","created":false}`
				baseLen := int64(len(base))
				padding := tt.responseLen - baseLen
				if padding < 0 {
					padding = 0
				}

				// Build response manually to control exact size
				// Use 'a' chars which don't get escaped in JSON
				paddingStr := make([]byte, padding)
				for i := range paddingStr {
					paddingStr[i] = 'a'
				}
				resp := `{"workspace_id":"` + string(paddingStr) + `","alias":"","project_id":"","repo_id":"","created":false}`
				w.Write([]byte(resp))
			}))
			defer server.Close()

			c := New(server.URL)
			_, err := c.RegisterWorkspace(context.Background(), &RegisterWorkspaceRequest{
				RepoOrigin: "git@github.com:test/repo.git",
			})

			if tt.wantErr && err == nil {
				t.Error("Expected error for oversized response")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestGet_ResponseSizeLimiting(t *testing.T) {
	tests := []struct {
		name        string
		responseLen int64
		wantErr     bool
	}{
		{
			name:        "exact max size is accepted",
			responseLen: maxResponseSize,
			wantErr:     false,
		},
		{
			name:        "over max size is rejected",
			responseLen: maxResponseSize + 1,
			wantErr:     true,
		},
		{
			name:        "far over max size is rejected",
			responseLen: maxResponseSize + 1000,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Create JSON response with exact byte size
				// {"workspaces":[{"workspace_id":"XXX...","alias":"","human_name":"","project_slug":"","status":"","last_seen":""}],"count":1}
				base := `{"workspaces":[{"workspace_id":"","alias":"","human_name":"","project_slug":"","status":"","last_seen":""}],"count":1}`
				baseLen := int64(len(base))
				padding := tt.responseLen - baseLen
				if padding < 0 {
					padding = 0
				}

				paddingStr := make([]byte, padding)
				for i := range paddingStr {
					paddingStr[i] = 'a'
				}
				resp := `{"workspaces":[{"workspace_id":"` + string(paddingStr) + `","alias":"","human_name":"","project_slug":"","status":"","last_seen":""}],"count":1}`
				w.Write([]byte(resp))
			}))
			defer server.Close()

			c := New(server.URL)
			_, err := c.Workspaces(context.Background(), &WorkspacesRequest{})

			if tt.wantErr && err == nil {
				t.Error("Expected error for oversized response")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestNewWithAPIKey_SendsAuthorizationHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Authorization header is sent
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer aw_sk_test123" {
			t.Errorf("Expected Authorization 'Bearer aw_sk_test123', got '%s'", authHeader)
		}

		resp := CommandResponse{Approved: true}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := NewWithAPIKey(server.URL, "aw_sk_test123")
	_, err := c.Command(context.Background(), &CommandRequest{
		WorkspaceID: "ws-123",
		Alias:       "test-agent",
		CommandLine: "bd ready",
	})
	if err != nil {
		t.Fatalf("Command() error: %v", err)
	}
}

func TestNewWithAPIKey_GETSendsAuthorizationHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Authorization header is sent
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer aw_sk_test456" {
			t.Errorf("Expected Authorization 'Bearer aw_sk_test456', got '%s'", authHeader)
		}

		resp := ActivePolicyResponse{PolicyID: "pol-123"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := NewWithAPIKey(server.URL, "aw_sk_test456")
	_, err := c.ActivePolicy(context.Background(), &ActivePolicyRequest{
		Role: "agent",
	})
	if err != nil {
		t.Fatalf("ActivePolicy() error: %v", err)
	}
}
