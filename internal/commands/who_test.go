package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/beadhub/bdh/internal/config"
)

func TestWho(t *testing.T) {
	tests := []struct {
		name           string
		mockWorkspaces []map[string]any
		wantContains   []string
		wantErr        bool
	}{
		{
			name: "shows multiple workspaces",
			mockWorkspaces: []map[string]any{
				{
					"workspace_id": "uuid-1",
					"alias":        "claude-main",
					"human_name":   "Juan",
					"status":       "active",
					"last_seen":    "2025-12-11T12:00:00Z",
				},
				{
					"workspace_id": "uuid-2",
					"alias":        "other-agent",
					"human_name":   "Maria",
					"status":       "active",
					"last_seen":    "2025-12-11T11:30:00Z",
				},
			},
			wantContains: []string{"claude-main", "Juan", "other-agent", "Maria"},
		},
		{
			name:           "shows no workspaces",
			mockWorkspaces: []map[string]any{},
			wantContains:   []string{"No active workspaces"},
		},
		{
			name: "shows workspace apex title for epic",
			mockWorkspaces: []map[string]any{
				{
					"workspace_id": "uuid-1",
					"alias":        "claude-main",
					"human_name":   "Juan",
					"apex_id":      "beadhub-npuh",
					"apex_title":   "Apex bead tracking",
					"apex_type":    "epic",
					"status":       "active",
					"last_seen":    "2025-12-11T12:00:00Z",
				},
			},
			wantContains: []string{"claude-main", "Juan", "Working on epic: beadhub-npuh Apex bead tracking"},
		},
		{
			name: "shows workspace apex id only",
			mockWorkspaces: []map[string]any{
				{
					"workspace_id": "uuid-1",
					"alias":        "claude-main",
					"human_name":   "Juan",
					"apex_id":      "beadhub-npuh",
					"status":       "active",
					"last_seen":    "2025-12-11T12:00:00Z",
				},
			},
			wantContains: []string{"claude-main", "Juan", "Working on: beadhub-npuh"},
		},
		{
			name: "shows workspace with non-epic apex",
			mockWorkspaces: []map[string]any{
				{
					"workspace_id": "uuid-1",
					"alias":        "claude-main",
					"human_name":   "Juan",
					"role":         "Backend expert",
					"apex_id":      "beadhub-abc",
					"apex_title":   "Fix something",
					"apex_type":    "task",
					"status":       "active",
					"last_seen":    "2025-12-11T12:00:00Z",
				},
			},
			wantContains: []string{"claude-main", "Working on: beadhub-abc Fix something"},
		},
		{
			name: "shows recent focus when no claims",
			mockWorkspaces: []map[string]any{
				{
					"workspace_id":     "uuid-1",
					"alias":            "claude-main",
					"human_name":       "Juan",
					"focus_apex_id":    "beadhub-xyz",
					"focus_apex_title": "Last Epic",
					"status":           "active",
					"last_seen":        "2025-12-11T12:00:00Z",
				},
			},
			wantContains: []string{"claude-main", "Recent focus: beadhub-xyz Last Epic"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/workspaces/team":
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(map[string]any{
						"workspaces": tt.mockWorkspaces,
						"count":      len(tt.mockWorkspaces),
					})
				case "/v1/reservations":
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(map[string]any{
						"reservations": []any{},
						"count":        0,
					})
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()

			cfg := &config.Config{
				WorkspaceID: "my-workspace-id",
				BeadhubURL:  server.URL,
				RepoOrigin:  "git@github.com:test/repo.git",
				Alias:       "test-agent",
				HumanName:   "Test Human",
			}

			result, err := fetchWhoWithConfig(cfg, WhoOptions{Limit: defaultWhoLimit})

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			output := formatWhoOutput(result, false)
			for _, want := range tt.wantContains {
				if !strings.Contains(output, want) {
					t.Errorf("output missing %q:\n%s", want, output)
				}
			}
		})
	}
}

func TestWho_ShowsLocks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/workspaces/team":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"workspaces": []map[string]any{
					{
						"workspace_id": "uuid-1",
						"alias":        "claude-main",
						"human_name":   "Juan",
						"status":       "active",
						"last_seen":    "2025-12-11T12:00:00Z",
					},
				},
				"count": 1,
			})
		case "/v1/reservations":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"reservations": []map[string]any{
					{
						"lock_id":               "lk-1",
						"path":                  "src/api.py",
						"alias":                 "claude-main",
						"workspace_id":          "uuid-1",
						"project_id":            "proj-1",
						"exclusive":             true,
						"acquired_at":           "2025-12-11T11:50:00Z",
						"expires_at":            "2025-12-11T12:05:00Z",
						"ttl_remaining_seconds": 180,
					},
				},
				"count": 1,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		WorkspaceID: "my-workspace-id",
		BeadhubURL:  server.URL,
		RepoOrigin:  "git@github.com:test/repo.git",
		Alias:       "test-agent",
		HumanName:   "Test Human",
	}

	result, err := fetchWhoWithConfig(cfg, WhoOptions{Limit: defaultWhoLimit})
	if err != nil {
		t.Fatalf("fetchWhoWithConfig error: %v", err)
	}

	output := formatWhoOutput(result, false)
	if !strings.Contains(output, "Reservations:") {
		t.Fatalf("expected reservations section, got:\n%s", output)
	}
	if !strings.Contains(output, "src/api.py (expires in 3m)") {
		t.Fatalf("expected reservation entry, got:\n%s", output)
	}
}

func TestWho_UsesBoundedTeamQuery(t *testing.T) {
	t.Setenv("BEADHUB_API_KEY", "bh_sk_test123")

	var gotQuery url.Values
	var gotAuthHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/workspaces/team":
			gotQuery = r.URL.Query()
			gotAuthHeader = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"workspaces": []map[string]any{},
				"count":      0,
			})
		case "/v1/reservations":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"reservations": []any{},
				"count":        0,
			})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		WorkspaceID: "my-workspace-id",
		BeadhubURL:  server.URL,
		ProjectSlug: "test-project",
		RepoOrigin:  "git@github.com:test/repo.git",
		Alias:       "test-agent",
		HumanName:   "Test Human",
	}

	_, err := fetchWhoWithConfig(cfg, WhoOptions{Limit: defaultWhoLimit})
	if err != nil {
		t.Fatalf("fetchWhoWithConfig error: %v", err)
	}

	if gotAuthHeader != "Bearer bh_sk_test123" {
		t.Errorf("Authorization = %q, want %q", gotAuthHeader, "Bearer bh_sk_test123")
	}
	if gotQuery.Get("include_claims") != "true" {
		t.Errorf("include_claims = %q, want true", gotQuery.Get("include_claims"))
	}
	if gotQuery.Get("include_presence") != "true" {
		t.Errorf("include_presence = %q, want true", gotQuery.Get("include_presence"))
	}
	if gotQuery.Get("only_with_claims") != "false" {
		t.Errorf("only_with_claims = %q, want false", gotQuery.Get("only_with_claims"))
	}
	if gotQuery.Get("limit") != "50" {
		t.Errorf("limit = %q, want 50", gotQuery.Get("limit"))
	}
}

func TestWho_RequestsLocksWithWorkspaceID(t *testing.T) {
	var gotWorkspaceID string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/workspaces/team":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"workspaces": []map[string]any{},
				"count":      0,
			})
		case "/v1/reservations":
			gotWorkspaceID = r.URL.Query().Get("workspace_id")
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"reservations": []any{},
				"count":        0,
			})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		WorkspaceID: "my-workspace-id",
		BeadhubURL:  server.URL,
		ProjectSlug: "test-project",
		RepoOrigin:  "git@github.com:test/repo.git",
		Alias:       "test-agent",
		HumanName:   "Test Human",
	}

	_, err := fetchWhoWithConfig(cfg, WhoOptions{Limit: defaultWhoLimit})
	if err != nil {
		t.Fatalf("fetchWhoWithConfig error: %v", err)
	}

	if gotWorkspaceID != "my-workspace-id" {
		t.Errorf("workspace_id = %q, want %q", gotWorkspaceID, "my-workspace-id")
	}
}

func TestWho_RespectsCustomLimitAndFilters(t *testing.T) {
	var gotQuery url.Values

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/workspaces/team":
			gotQuery = r.URL.Query()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"workspaces": []map[string]any{},
				"count":      0,
			})
		case "/v1/reservations":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"reservations": []any{},
				"count":        0,
			})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		WorkspaceID: "my-workspace-id",
		BeadhubURL:  server.URL,
		ProjectSlug: "test-project",
		RepoOrigin:  "git@github.com:test/repo.git",
		Alias:       "test-agent",
		HumanName:   "Test Human",
	}

	_, err := fetchWhoWithConfig(cfg, WhoOptions{
		Limit:          25,
		OnlyWithClaims: true,
	})
	if err != nil {
		t.Fatalf("fetchWhoWithConfig error: %v", err)
	}

	if gotQuery.Get("only_with_claims") != "true" {
		t.Errorf("only_with_claims = %q, want true", gotQuery.Get("only_with_claims"))
	}
	if gotQuery.Get("limit") != "25" {
		t.Errorf("limit = %q, want 25", gotQuery.Get("limit"))
	}
}

func TestWho_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("database error"))
	}))
	defer server.Close()

	cfg := &config.Config{
		WorkspaceID: "my-workspace-id",
		BeadhubURL:  server.URL,
		RepoOrigin:  "git@github.com:test/repo.git",
		Alias:       "test-agent",
		HumanName:   "Test Human",
	}

	_, err := fetchWhoWithConfig(cfg, WhoOptions{Limit: defaultWhoLimit})
	if err == nil {
		t.Error("expected error for server error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected 500 in error, got: %v", err)
	}
}

func TestFormatWhoOutput_Plain(t *testing.T) {
	result := &WhoResult{
		Workspaces: []WorkspaceInfo{
			{
				WorkspaceID: "uuid-1",
				Alias:       "claude-main",
				HumanName:   "Juan",
				Status:      "active",
				LastSeen:    "2025-12-11T12:00:00Z",
			},
		},
	}

	output := formatWhoOutput(result, false)
	if !strings.Contains(output, "claude-main") {
		t.Errorf("output missing agent name: %s", output)
	}
	if !strings.Contains(output, "Juan") {
		t.Errorf("output missing human name: %s", output)
	}
}

func TestFormatWhoOutput_JSON(t *testing.T) {
	result := &WhoResult{
		Workspaces: []WorkspaceInfo{
			{
				WorkspaceID: "uuid-1",
				Alias:       "claude-main",
				HumanName:   "Juan",
				Status:      "active",
				LastSeen:    "2025-12-11T12:00:00Z",
			},
		},
	}

	output := formatWhoOutput(result, true)

	var parsed map[string]any
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	workspaces, ok := parsed["workspaces"].([]any)
	if !ok {
		t.Fatal("expected workspaces array")
	}
	if len(workspaces) != 1 {
		t.Errorf("expected 1 workspace, got %d", len(workspaces))
	}
}

func TestWho_ScopesToProject(t *testing.T) {
	t.Setenv("BEADHUB_API_KEY", "bh_sk_test123")

	var capturedAuthHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/workspaces/team":
			capturedAuthHeader = r.Header.Get("Authorization")

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"workspaces": []map[string]any{
					{
						"workspace_id": "uuid-1",
						"alias":        "claude-main",
						"human_name":   "Juan",
						"status":       "active",
						"last_seen":    "2025-12-11T12:00:00Z",
					},
				},
				"count": 1,
			})
		case "/v1/reservations":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"reservations": []any{},
				"count":        0,
			})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		WorkspaceID: "my-workspace-id",
		ProjectSlug: "my-project",
		BeadhubURL:  server.URL,
		RepoOrigin:  "git@github.com:test/repo.git",
		Alias:       "test-agent",
		HumanName:   "Test Human",
	}

	_, err := fetchWhoWithConfig(cfg, WhoOptions{Limit: defaultWhoLimit})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedAuthHeader != "Bearer bh_sk_test123" {
		t.Errorf("expected Authorization %q, got %q", "Bearer bh_sk_test123", capturedAuthHeader)
	}
}
