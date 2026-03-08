package commands

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseRunWakeEvent(t *testing.T) {
	tests := []struct {
		name      string
		eventName string
		data      string
		check     func(t *testing.T, evt runWakeEvent)
	}{
		{
			name:      "connected",
			eventName: "connected",
			data:      `{"agent_id":"a1","project_id":"p1"}`,
			check: func(t *testing.T, evt runWakeEvent) {
				if evt.Type != runWakeEventConnected || evt.AgentID != "a1" || evt.ProjectID != "p1" {
					t.Fatalf("unexpected connected event: %#v", evt)
				}
			},
		},
		{
			name:      "chat",
			eventName: "chat_message",
			data:      `{"type":"chat_message","message_id":"m1","from_alias":"mia","session_id":"s1"}`,
			check: func(t *testing.T, evt runWakeEvent) {
				if evt.Type != runWakeEventChatMessage || evt.MessageID != "m1" || evt.FromAlias != "mia" || evt.SessionID != "s1" {
					t.Fatalf("unexpected chat event: %#v", evt)
				}
			},
		},
		{
			name:      "claim removed",
			eventName: "claim_removed",
			data:      `{"type":"claim_removed","task_id":"t1"}`,
			check: func(t *testing.T, evt runWakeEvent) {
				if evt.Type != runWakeEventClaimRemoved || evt.TaskID != "t1" {
					t.Fatalf("unexpected claim_removed event: %#v", evt)
				}
			},
		},
		{
			name:      "control interrupt",
			eventName: "control_interrupt",
			data:      `{"type":"control_interrupt","signal_id":"sig1"}`,
			check: func(t *testing.T, evt runWakeEvent) {
				if evt.Type != runWakeEventControlInterrupt || evt.SignalID != "sig1" {
					t.Fatalf("unexpected control event: %#v", evt)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evt, ok, err := parseRunWakeEvent(tt.eventName, tt.data)
			if err != nil {
				t.Fatalf("parseRunWakeEvent returned error: %v", err)
			}
			if !ok {
				t.Fatal("expected parsed event")
			}
			tt.check(t, evt)
		})
	}
}

func TestParseRunWakeEventUnknown(t *testing.T) {
	_, ok, err := parseRunWakeEvent("unknown_type", `{"x":1}`)
	if err != nil {
		t.Fatalf("parseRunWakeEvent returned error: %v", err)
	}
	if ok {
		t.Fatal("expected unknown event to be ignored")
	}
}

func TestRunEventStreamClientStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token-123" {
			http.Error(w, "missing auth", http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get("Accept"); !strings.Contains(got, "text/event-stream") {
			http.Error(w, "missing accept", http.StatusBadRequest)
			return
		}
		if got := r.URL.Query().Get("deadline"); got == "" {
			http.Error(w, "missing deadline", http.StatusUnprocessableEntity)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, ": keepalive\n\n")
		_, _ = fmt.Fprint(w, "event: connected\n")
		_, _ = fmt.Fprint(w, "data: {\"agent_id\":\"a1\",\"project_id\":\"p1\"}\n\n")
		_, _ = fmt.Fprint(w, "event: work_available\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"work_available\",\"task_id\":\"t1\",\"title\":\"Fix it\"}\n\n")
	}))
	defer server.Close()

	client := &runEventStreamClient{
		baseURL:    server.URL,
		apiKey:     "token-123",
		httpClient: server.Client(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	events, errs := client.Stream(ctx, time.Now().Add(5*time.Second))

	var got []runWakeEvent
	for evt := range events {
		got = append(got, evt)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 events, got %d (%#v)", len(got), got)
	}
	if got[0].Type != runWakeEventConnected {
		t.Fatalf("expected first event to be connected, got %#v", got[0])
	}
	if got[1].Type != runWakeEventWorkAvailable || got[1].TaskID != "t1" {
		t.Fatalf("unexpected second event: %#v", got[1])
	}

	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected stream error: %v", err)
		}
	}
}

func TestRunEventStreamClientStreamHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"detail":"nope"}`, http.StatusUnauthorized)
	}))
	defer server.Close()

	client := &runEventStreamClient{
		baseURL:    server.URL,
		apiKey:     "token-123",
		httpClient: server.Client(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	events, errs := client.Stream(ctx, time.Now().Add(5*time.Second))
	for range events {
		t.Fatal("did not expect any events")
	}
	var gotErr error
	for err := range errs {
		gotErr = err
	}
	if gotErr == nil {
		t.Fatal("expected stream error")
	}
	if !strings.Contains(gotErr.Error(), "401") || !strings.Contains(gotErr.Error(), "nope") {
		t.Fatalf("unexpected error: %v", gotErr)
	}
}
