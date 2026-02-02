package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCreateChatSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/chat/sessions" {
			t.Errorf("Expected path /v1/chat/sessions, got %s", r.URL.Path)
		}

		var req CreateChatSessionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("Failed to decode request: %v", err)
		}

		if req.FromWorkspace != "ws-1" {
			t.Errorf("Expected from_workspace 'ws-1', got '%s'", req.FromWorkspace)
		}
		if req.FromAlias != "agent-p1" {
			t.Errorf("Expected from_alias 'agent-p1', got '%s'", req.FromAlias)
		}
		if len(req.ToAliases) != 1 || req.ToAliases[0] != "agent-p2" {
			t.Errorf("Expected to_aliases ['agent-p2'], got %v", req.ToAliases)
		}
		if req.Message != "Hello?" {
			t.Errorf("Expected message 'Hello?', got '%s'", req.Message)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CreateChatSessionResponse{
			SessionID: "chat_123",
			MessageID: "msg_456",
			Participants: []ChatParticipant{
				{WorkspaceID: "ws-1", Alias: "agent-p1"},
				{WorkspaceID: "ws-2", Alias: "agent-p2"},
			},
			SSEURL: "/v1/chat/sessions/chat_123/stream",
		})
	}))
	defer server.Close()

	client := New(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.CreateChatSession(ctx, &CreateChatSessionRequest{
		FromWorkspace: "ws-1",
		FromAlias:     "agent-p1",
		ToAliases:     []string{"agent-p2"},
		Message:       "Hello?",
	})
	if err != nil {
		t.Fatalf("CreateChatSession failed: %v", err)
	}

	if resp.SessionID != "chat_123" {
		t.Errorf("Expected session_id 'chat_123', got '%s'", resp.SessionID)
	}
	if resp.MessageID != "msg_456" {
		t.Errorf("Expected message_id 'msg_456', got '%s'", resp.MessageID)
	}
	if len(resp.Participants) != 2 {
		t.Errorf("Expected 2 participants, got %d", len(resp.Participants))
	}
}

func TestSendChatMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/chat/sessions/chat_123/messages" {
			t.Errorf("Expected path /v1/chat/sessions/chat_123/messages, got %s", r.URL.Path)
		}

		var req SendChatMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("Failed to decode request: %v", err)
		}

		if req.Body != "Yes, it's required" {
			t.Errorf("Expected body 'Yes, it's required', got '%s'", req.Body)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(SendChatMessageResponse{
			MessageID: "msg_456",
			Delivered: true,
		})
	}))
	defer server.Close()

	client := New(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.SendChatMessage(ctx, "chat_123", &SendChatMessageRequest{
		WorkspaceID: "ws-2",
		Alias:       "agent-p2",
		Body:        "Yes, it's required",
	})
	if err != nil {
		t.Fatalf("SendChatMessage failed: %v", err)
	}

	if resp.MessageID != "msg_456" {
		t.Errorf("Expected message_id 'msg_456', got '%s'", resp.MessageID)
	}
	if !resp.Delivered {
		t.Error("Expected delivered=true")
	}
}

func TestMarkRead(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/chat/sessions/chat_123/read" {
			t.Errorf("Expected path /v1/chat/sessions/chat_123/read, got %s", r.URL.Path)
		}

		var req MarkReadRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("Failed to decode request: %v", err)
		}

		if req.WorkspaceID != "ws-1" {
			t.Errorf("Expected workspace_id 'ws-1', got '%s'", req.WorkspaceID)
		}
		if req.UpToMessageID != "msg_456" {
			t.Errorf("Expected up_to_message_id 'msg_456', got '%s'", req.UpToMessageID)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(MarkReadResponse{
			Success: true,
		})
	}))
	defer server.Close()

	client := New(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.MarkRead(ctx, "chat_123", &MarkReadRequest{
		WorkspaceID:   "ws-1",
		UpToMessageID: "msg_456",
	})
	if err != nil {
		t.Fatalf("MarkRead failed: %v", err)
	}

	if !resp.Success {
		t.Error("Expected success=true")
	}
}

func TestGetPendingChats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/chat/pending" {
			t.Errorf("Expected path /v1/chat/pending, got %s", r.URL.Path)
		}

		// Verify query params
		if r.URL.Query().Get("workspace_id") != "ws-2" {
			t.Errorf("Expected workspace_id 'ws-2', got '%s'", r.URL.Query().Get("workspace_id"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(GetPendingChatsResponse{
			Pending: []PendingConversation{
				{
					SessionID:    "chat_123",
					Participants: []string{"agent-p1", "agent-p2"},
					LastMessage:  "Is project_id nullable?",
					LastFrom:     "agent-p1",
					UnreadCount:  2,
					LastActivity: "2025-12-11T18:00:00Z",
				},
			},
			MessagesWaiting: 3,
		})
	}))
	defer server.Close()

	client := New(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.GetPendingChats(ctx, &GetPendingChatsRequest{
		WorkspaceID: "ws-2",
	})
	if err != nil {
		t.Fatalf("GetPendingChats failed: %v", err)
	}

	if len(resp.Pending) != 1 {
		t.Fatalf("Expected 1 pending conversation, got %d", len(resp.Pending))
	}
	if resp.Pending[0].LastFrom != "agent-p1" {
		t.Errorf("Expected last_from 'agent-p1', got '%s'", resp.Pending[0].LastFrom)
	}
	if resp.Pending[0].UnreadCount != 2 {
		t.Errorf("Expected unread_count 2, got %d", resp.Pending[0].UnreadCount)
	}
	if resp.MessagesWaiting != 3 {
		t.Errorf("Expected messages_waiting 3, got %d", resp.MessagesWaiting)
	}
}

func TestGetPendingChats_Empty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(GetPendingChatsResponse{
			Pending: []PendingConversation{},
		})
	}))
	defer server.Close()

	client := New(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.GetPendingChats(ctx, &GetPendingChatsRequest{
		WorkspaceID: "ws-2",
	})
	if err != nil {
		t.Fatalf("GetPendingChats failed: %v", err)
	}

	if len(resp.Pending) != 0 {
		t.Errorf("Expected 0 pending conversations, got %d", len(resp.Pending))
	}
	if resp.MessagesWaiting != 0 {
		t.Errorf("Expected messages_waiting 0, got %d", resp.MessagesWaiting)
	}
}

func TestGetMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/chat/sessions/chat_123/messages" {
			t.Errorf("Expected path /v1/chat/sessions/chat_123/messages, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(GetMessagesResponse{
			SessionID: "chat_123",
			Messages: []ChatMessage{
				{MessageID: "msg_1", FromAgent: "agent-p1", Body: "Hello?", CreatedAt: "2025-12-11T18:00:00Z"},
				{MessageID: "msg_2", FromAgent: "agent-p2", Body: "Hi!", CreatedAt: "2025-12-11T18:01:00Z"},
			},
		})
	}))
	defer server.Close()

	client := New(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.GetMessages(ctx, "chat_123", &GetMessagesRequest{
		WorkspaceID: "ws-1",
	})
	if err != nil {
		t.Fatalf("GetMessages failed: %v", err)
	}

	if resp.SessionID != "chat_123" {
		t.Errorf("Expected session_id 'chat_123', got '%s'", resp.SessionID)
	}
	if len(resp.Messages) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(resp.Messages))
	}
	if resp.Messages[0].Body != "Hello?" {
		t.Errorf("Expected first message body 'Hello?', got '%s'", resp.Messages[0].Body)
	}
}

func TestCreateChatSession_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := New(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.CreateChatSession(ctx, &CreateChatSessionRequest{
		FromWorkspace: "ws-1",
		FromAlias:     "agent-p1",
		ToAliases:     []string{"agent-p2"},
		Message:       "Hello?",
	})
	if err == nil {
		t.Fatal("Expected error for 500 response, got nil")
	}

	clientErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("Expected *Error, got %T", err)
	}
	if clientErr.StatusCode != 500 {
		t.Errorf("Expected status 500, got %d", clientErr.StatusCode)
	}
}

func TestSendChatMessage_SessionNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"detail": "Session not found"}`, http.StatusNotFound)
	}))
	defer server.Close()

	client := New(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.SendChatMessage(ctx, "nonexistent", &SendChatMessageRequest{
		WorkspaceID: "ws-2",
		Alias:       "agent-p2",
		Body:        "Hello",
	})
	if err == nil {
		t.Fatal("Expected error for 404 response, got nil")
	}

	clientErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("Expected *Error, got %T", err)
	}
	if clientErr.StatusCode != 404 {
		t.Errorf("Expected status 404, got %d", clientErr.StatusCode)
	}
}
