package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beadhub/bdh/internal/config"
)

func TestEscalate(t *testing.T) {
	t.Setenv("BEADHUB_API_KEY", "aw_sk_test123")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/escalations" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)

		if req["subject"] != "Blocked on bd-42" {
			t.Errorf("unexpected subject: %v", req["subject"])
		}
		if req["situation"] != "Agent not responding" {
			t.Errorf("unexpected situation: %v", req["situation"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"escalation_id": "esc_12345",
			"status":        "pending",
			"created_at":    "2025-12-11T12:00:00Z",
		})
	}))
	defer server.Close()

	cfg := &config.Config{
		WorkspaceID: "my-workspace-id",
		BeadhubURL:  server.URL,
		ProjectSlug: "test",
		RepoOrigin:  "git@github.com:test/repo.git",
		Alias:       "test-agent",
		HumanName:   "Test Human",
	}

	result, err := createEscalationWithConfig(cfg, "Blocked on bd-42", "Agent not responding")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.EscalationID != "esc_12345" {
		t.Errorf("unexpected escalation_id: %s", result.EscalationID)
	}
	if result.Status != "pending" {
		t.Errorf("unexpected status: %s", result.Status)
	}
}

func TestEscalate_EmptySubject(t *testing.T) {
	cfg := &config.Config{
		WorkspaceID: "my-workspace-id",
		BeadhubURL:  "http://localhost:8000",
		ProjectSlug: "test",
		RepoOrigin:  "git@github.com:test/repo.git",
		Alias:       "test-agent",
		HumanName:   "Test Human",
	}

	_, err := createEscalationWithConfig(cfg, "", "Some situation")
	if err == nil {
		t.Error("expected error for empty subject")
	}
	if !strings.Contains(err.Error(), "subject cannot be empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEscalate_EmptySituation(t *testing.T) {
	cfg := &config.Config{
		WorkspaceID: "my-workspace-id",
		BeadhubURL:  "http://localhost:8000",
		ProjectSlug: "test",
		RepoOrigin:  "git@github.com:test/repo.git",
		Alias:       "test-agent",
		HumanName:   "Test Human",
	}

	_, err := createEscalationWithConfig(cfg, "Some subject", "")
	if err == nil {
		t.Error("expected error for empty situation")
	}
	if !strings.Contains(err.Error(), "situation cannot be empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEscalate_ServerError(t *testing.T) {
	t.Setenv("BEADHUB_API_KEY", "aw_sk_test123")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("database error"))
	}))
	defer server.Close()

	cfg := &config.Config{
		WorkspaceID: "my-workspace-id",
		BeadhubURL:  server.URL,
		ProjectSlug: "test",
		RepoOrigin:  "git@github.com:test/repo.git",
		Alias:       "test-agent",
		HumanName:   "Test Human",
	}

	_, err := createEscalationWithConfig(cfg, "Subject", "Situation")
	if err == nil {
		t.Error("expected error for server error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected 500 in error, got: %v", err)
	}
}

func TestFormatEscalateOutput_Plain(t *testing.T) {
	result := &EscalateResult{
		EscalationID: "esc_12345",
		Status:       "pending",
		CreatedAt:    "2025-12-11T12:00:00Z",
	}

	output := formatEscalateOutput(result, false)
	if !strings.Contains(output, "esc_12345") {
		t.Errorf("output missing escalation ID: %s", output)
	}
	if !strings.Contains(output, "human will review") {
		t.Errorf("output missing review message: %s", output)
	}
}

func TestFormatEscalateOutput_JSON(t *testing.T) {
	result := &EscalateResult{
		EscalationID: "esc_12345",
		Status:       "pending",
		CreatedAt:    "2025-12-11T12:00:00Z",
	}

	output := formatEscalateOutput(result, true)

	var parsed map[string]any
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if parsed["escalation_id"] != "esc_12345" {
		t.Errorf("unexpected escalation_id: %v", parsed["escalation_id"])
	}
}
