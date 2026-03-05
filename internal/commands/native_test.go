package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	aweb "github.com/awebai/aw"
	"github.com/beadhub/bdh/internal/beads"
	"github.com/beadhub/bdh/internal/config"
)

func TestParseNativeCommand(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantCmd string
		wantErr bool
	}{
		{name: "create command", args: []string{"create", "--title", "Test"}, wantCmd: "create"},
		{name: "list command", args: []string{"list", "--status=open"}, wantCmd: "list"},
		{name: "show command", args: []string{"show", "bdh-42"}, wantCmd: "show"},
		{name: "update command", args: []string{"update", "bdh-42", "--status", "in_progress"}, wantCmd: "update"},
		{name: "close command", args: []string{"close", "bdh-42"}, wantCmd: "close"},
		{name: "ready command", args: []string{"ready"}, wantCmd: "ready"},
		{name: "blocked command", args: []string{"blocked"}, wantCmd: "blocked"},
		{name: "dep command", args: []string{"dep", "add", "bdh-42", "bdh-43"}, wantCmd: "dep"},
		{name: "sync command", args: []string{"sync"}, wantCmd: "sync"},
		{name: "stats command", args: []string{"stats"}, wantCmd: "stats"},
		{name: "reopen command", args: []string{"reopen", "bdh-42"}, wantCmd: "reopen"},
		{name: "unsupported command", args: []string{"search", "foo"}, wantErr: true},
		{name: "empty args", args: []string{}, wantErr: true},
		{name: "help flag passthrough", args: []string{"--help"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, _, err := parseNativeCommand(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseNativeCommand(%v) error = nil, want error", tt.args)
				}
				return
			}
			if err != nil {
				t.Errorf("parseNativeCommand(%v) error = %v, want nil", tt.args, err)
				return
			}
			if cmd != tt.wantCmd {
				t.Errorf("parseNativeCommand(%v) cmd = %q, want %q", tt.args, cmd, tt.wantCmd)
			}
		})
	}
}

func TestParseNativeCommand_WithGlobalFlags(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantCmd       string
		wantRemaining []string
	}{
		{
			name:          "db flag before command",
			args:          []string{"--db", ".beads/beads.db", "create", "--title", "Test"},
			wantCmd:       "create",
			wantRemaining: []string{"--title", "Test"},
		},
		{
			name:          "actor flag with value matching command name",
			args:          []string{"--actor", "list", "list", "--status=open"},
			wantCmd:       "list",
			wantRemaining: []string{"--status=open"},
		},
		{
			name:          "no remaining args",
			args:          []string{"ready"},
			wantCmd:       "ready",
			wantRemaining: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, remaining, err := parseNativeCommand(tt.args)
			if err != nil {
				t.Fatalf("parseNativeCommand(%v) error = %v", tt.args, err)
			}
			if cmd != tt.wantCmd {
				t.Errorf("cmd = %q, want %q", cmd, tt.wantCmd)
			}
			if len(remaining) != len(tt.wantRemaining) {
				t.Fatalf("remaining = %v, want %v", remaining, tt.wantRemaining)
			}
			for i := range remaining {
				if remaining[i] != tt.wantRemaining[i] {
					t.Errorf("remaining[%d] = %q, want %q", i, remaining[i], tt.wantRemaining[i])
				}
			}
		})
	}
}

func TestNativeCommandsAreListed(t *testing.T) {
	supported := []string{"create", "list", "show", "update", "close", "ready", "blocked", "dep", "sync", "stats", "reopen"}
	for _, cmd := range supported {
		if !isNativeCommand(cmd) {
			t.Errorf("isNativeCommand(%q) = false, want true", cmd)
		}
	}

	unsupported := []string{"search", "export", "import", "init", "edit", "epic", "swarm"}
	for _, cmd := range unsupported {
		if isNativeCommand(cmd) {
			t.Errorf("isNativeCommand(%q) = true, want false", cmd)
		}
	}
}

// --- Mock server helper ---

// nativeMockServer creates a test server that handles /v1/tasks endpoints
// and the bdh coordination endpoints. Returns the server and an aw client.
func nativeMockServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *aweb.Client) {
	t.Helper()

	mux := http.NewServeMux()

	// bdh coordination endpoints (for passthrough tests)
	mux.HandleFunc("/v1/bdh/command", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"approved": true,
			"context":  map[string]any{"messages_waiting": 0, "beads_in_progress": []any{}},
		})
	})
	mux.HandleFunc("/v1/chat/pending", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"pending": []any{}, "messages_waiting": 0})
	})

	// Task endpoints
	mux.HandleFunc("/", handler)

	server := httptest.NewServer(mux)
	client, err := aweb.NewWithAPIKey(server.URL, "test-key")
	if err != nil {
		t.Fatalf("creating aw client: %v", err)
	}

	return server, client
}

// --- Native command handler tests ---

func TestNativeCreate(t *testing.T) {
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/tasks" && r.Method == "POST" {
			var req map[string]any
			json.NewDecoder(r.Body).Decode(&req)
			json.NewEncoder(w).Encode(aweb.Task{
				TaskRef:  "bdh-001",
				Title:    req["title"].(string),
				TaskType: "task",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"create", "--title", "My new task", "--type", "task", "--priority", "2"})
	if err != nil {
		t.Fatalf("runNative create: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr: %s", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "bdh-001") {
		t.Errorf("stdout = %q, want to contain bdh-001", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "My new task") {
		t.Errorf("stdout = %q, want to contain task title", result.Stdout)
	}
}

func TestNativeCreate_RequiresTitle(t *testing.T) {
	_, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {})

	result, err := runNative(client, []string{"create"})
	if err != nil {
		t.Fatalf("runNative create: %v", err)
	}
	if result.ExitCode != 1 {
		t.Errorf("exit code = %d, want 1", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "--title") {
		t.Errorf("stderr = %q, want to mention --title", result.Stderr)
	}
}

func TestNativeList(t *testing.T) {
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/tasks" && r.Method == "GET" {
			json.NewEncoder(w).Encode(aweb.TaskListResponse{
				Tasks: []aweb.TaskSummary{
					{TaskRef: "bdh-001", Title: "First task", Priority: 1, TaskType: "task", Status: "open"},
					{TaskRef: "bdh-002", Title: "Second task", Priority: 2, TaskType: "bug", Status: "open"},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"list", "--status=open"})
	if err != nil {
		t.Fatalf("runNative list: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr: %s", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "bdh-001") || !strings.Contains(result.Stdout, "bdh-002") {
		t.Errorf("stdout = %q, want both task refs", result.Stdout)
	}
}

func TestNativeList_Empty(t *testing.T) {
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/tasks" && r.Method == "GET" {
			json.NewEncoder(w).Encode(aweb.TaskListResponse{Tasks: []aweb.TaskSummary{}})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"list"})
	if err != nil {
		t.Fatalf("runNative list: %v", err)
	}
	if !strings.Contains(result.Stdout, "No tasks found") {
		t.Errorf("stdout = %q, want 'No tasks found'", result.Stdout)
	}
}

func TestNativeShow(t *testing.T) {
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/tasks/bdh-001" && r.Method == "GET" {
			json.NewEncoder(w).Encode(aweb.Task{
				TaskRef:     "bdh-001",
				Title:       "Test task",
				Description: "A detailed description",
				Status:      "open",
				Priority:    2,
				TaskType:    "feature",
				CreatedAt:   "2026-03-05T10:00:00Z",
				UpdatedAt:   "2026-03-05T10:00:00Z",
				BlockedBy: []aweb.TaskDepView{
					{TaskRef: "bdh-000", Title: "Prerequisite", Status: "open"},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"show", "bdh-001"})
	if err != nil {
		t.Fatalf("runNative show: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr: %s", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "bdh-001") {
		t.Errorf("stdout missing task ref")
	}
	if !strings.Contains(result.Stdout, "Test task") {
		t.Errorf("stdout missing title")
	}
	if !strings.Contains(result.Stdout, "A detailed description") {
		t.Errorf("stdout missing description")
	}
	if !strings.Contains(result.Stdout, "BLOCKED BY") {
		t.Errorf("stdout missing BLOCKED BY section")
	}
	if !strings.Contains(result.Stdout, "bdh-000") {
		t.Errorf("stdout missing blocker ref")
	}
}

func TestNativeClose_Multiple(t *testing.T) {
	closedRefs := map[string]bool{}
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/tasks/") && r.Method == "PATCH" {
			ref := strings.TrimPrefix(r.URL.Path, "/v1/tasks/")
			closedRefs[ref] = true
			json.NewEncoder(w).Encode(aweb.TaskUpdateResponse{
				Task: aweb.Task{TaskRef: ref, Title: "Task " + ref},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"close", "bdh-001", "bdh-002"})
	if err != nil {
		t.Fatalf("runNative close: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr: %s", result.ExitCode, result.Stderr)
	}
	if !closedRefs["bdh-001"] || !closedRefs["bdh-002"] {
		t.Errorf("expected both refs closed, got: %v", closedRefs)
	}
	if !strings.Contains(result.Stdout, "Closed bdh-001") || !strings.Contains(result.Stdout, "Closed bdh-002") {
		t.Errorf("stdout = %q, want both close confirmations", result.Stdout)
	}
}

func TestNativeReady(t *testing.T) {
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/tasks/ready" && r.Method == "GET" {
			json.NewEncoder(w).Encode(aweb.TaskListResponse{
				Tasks: []aweb.TaskSummary{
					{TaskRef: "bdh-010", Title: "Ready task", Priority: 1, TaskType: "task"},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"ready"})
	if err != nil {
		t.Fatalf("runNative ready: %v", err)
	}
	if !strings.Contains(result.Stdout, "Ready work") {
		t.Errorf("stdout = %q, want 'Ready work' header", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "bdh-010") {
		t.Errorf("stdout = %q, want task ref", result.Stdout)
	}
}

func TestNativeSync_NoOp(t *testing.T) {
	_, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {})

	result, err := runNative(client, []string{"sync"})
	if err != nil {
		t.Fatalf("runNative sync: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("sync exit code = %d, want 0", result.ExitCode)
	}
}

func TestNativeDep_Add(t *testing.T) {
	var gotRef, gotDep string
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/deps") && r.Method == "POST" {
			gotRef = strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/tasks/"), "/deps")
			var req map[string]string
			json.NewDecoder(r.Body).Decode(&req)
			gotDep = req["depends_on"]
			w.WriteHeader(http.StatusCreated)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"dep", "add", "bdh-002", "bdh-001"})
	if err != nil {
		t.Fatalf("runNative dep add: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr: %s", result.ExitCode, result.Stderr)
	}
	if gotRef != "bdh-002" {
		t.Errorf("dep ref = %q, want bdh-002", gotRef)
	}
	if gotDep != "bdh-001" {
		t.Errorf("depends_on = %q, want bdh-001", gotDep)
	}
}

func TestNativeUpdate(t *testing.T) {
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/tasks/") && r.Method == "PATCH" {
			ref := strings.TrimPrefix(r.URL.Path, "/v1/tasks/")
			json.NewEncoder(w).Encode(aweb.TaskUpdateResponse{
				Task: aweb.Task{TaskRef: ref, Title: "Updated task"},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"update", "bdh-001", "--status", "in_progress"})
	if err != nil {
		t.Fatalf("runNative update: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr: %s", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "Updated bdh-001") {
		t.Errorf("stdout = %q, want 'Updated bdh-001'", result.Stdout)
	}
}

func TestNativeUpdate_NoFields(t *testing.T) {
	_, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {})

	result, err := runNative(client, []string{"update", "bdh-001"})
	if err != nil {
		t.Fatalf("runNative update: %v", err)
	}
	if result.ExitCode != 1 {
		t.Errorf("exit code = %d, want 1 (no fields to update)", result.ExitCode)
	}
}

func TestNativeReopen(t *testing.T) {
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/tasks/") && r.Method == "PATCH" {
			ref := strings.TrimPrefix(r.URL.Path, "/v1/tasks/")
			json.NewEncoder(w).Encode(aweb.TaskUpdateResponse{
				Task: aweb.Task{TaskRef: ref, Title: "Reopened task"},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"reopen", "bdh-001"})
	if err != nil {
		t.Fatalf("runNative reopen: %v", err)
	}
	if !strings.Contains(result.Stdout, "Reopened bdh-001") {
		t.Errorf("stdout = %q, want 'Reopened bdh-001'", result.Stdout)
	}
}

func TestNativeStats(t *testing.T) {
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/tasks" && r.Method == "GET" {
			json.NewEncoder(w).Encode(aweb.TaskListResponse{
				Tasks: []aweb.TaskSummary{
					{TaskRef: "bdh-001", Status: "open"},
					{TaskRef: "bdh-002", Status: "open"},
					{TaskRef: "bdh-003", Status: "in_progress"},
					{TaskRef: "bdh-004", Status: "closed"},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"stats"})
	if err != nil {
		t.Fatalf("runNative stats: %v", err)
	}
	if !strings.Contains(result.Stdout, "Total: 4") {
		t.Errorf("stdout = %q, want 'Total: 4'", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "Open: 2") {
		t.Errorf("stdout = %q, want 'Open: 2'", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "In progress: 1") {
		t.Errorf("stdout = %q, want 'In progress: 1'", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "Closed: 1") {
		t.Errorf("stdout = %q, want 'Closed: 1'", result.Stdout)
	}
}

func TestNativeBlocked(t *testing.T) {
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/tasks" && r.Method == "GET" {
			json.NewEncoder(w).Encode(aweb.TaskListResponse{
				Tasks: []aweb.TaskSummary{
					{TaskRef: "bdh-001", Title: "Blocked task", Priority: 1, TaskType: "task", Status: "open"},
					{TaskRef: "bdh-002", Title: "Free task", Priority: 2, TaskType: "task", Status: "open"},
				},
			})
			return
		}
		if r.URL.Path == "/v1/tasks/bdh-001" && r.Method == "GET" {
			json.NewEncoder(w).Encode(aweb.Task{
				TaskRef:  "bdh-001",
				Title:    "Blocked task",
				Priority: 1,
				TaskType: "task",
				Status:   "open",
				BlockedBy: []aweb.TaskDepView{
					{TaskRef: "bdh-000", Title: "Prerequisite", Status: "open"},
				},
			})
			return
		}
		if r.URL.Path == "/v1/tasks/bdh-002" && r.Method == "GET" {
			json.NewEncoder(w).Encode(aweb.Task{
				TaskRef:  "bdh-002",
				Title:    "Free task",
				Priority: 2,
				TaskType: "task",
				Status:   "open",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"blocked"})
	if err != nil {
		t.Fatalf("runNative blocked: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr: %s", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "bdh-001") {
		t.Errorf("stdout missing blocked task bdh-001")
	}
	if strings.Contains(result.Stdout, "bdh-002") {
		t.Errorf("stdout should not contain unblocked task bdh-002")
	}
	if !strings.Contains(result.Stdout, "bdh-000") {
		t.Errorf("stdout missing blocker ref bdh-000")
	}
}

func TestNativeUpdate_HeldError(t *testing.T) {
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/tasks/") && r.Method == "PATCH" {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]any{
				"detail":            "task is held by another agent",
				"holder_agent_id":   "agent-abc",
				"assignee_agent_id": "agent-abc",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"update", "bdh-001", "--status", "in_progress"})
	if err != nil {
		t.Fatalf("runNative update: %v", err)
	}
	// Should return exit code 1 with the held error in stderr
	if result.ExitCode != 1 {
		t.Errorf("exit code = %d, want 1", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "held") {
		t.Errorf("stderr = %q, want to mention 'held'", result.Stderr)
	}
}

// --- Passthrough integration tests ---

func TestPassthrough_NativeModeListWorks(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	// NO .beads directory — native mode
	beads.ResetCache()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/bdh/command":
			json.NewEncoder(w).Encode(map[string]any{
				"approved": true,
				"context":  map[string]any{"messages_waiting": 0, "beads_in_progress": []any{}},
			})
		case "/v1/chat/pending":
			json.NewEncoder(w).Encode(map[string]any{"pending": []any{}, "messages_waiting": 0})
		case "/v1/tasks":
			json.NewEncoder(w).Encode(aweb.TaskListResponse{
				Tasks: []aweb.TaskSummary{
					{TaskRef: "test-001", Title: "Native list works", Priority: 2, TaskType: "task", Status: "open"},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		WorkspaceID:     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
		BeadhubURL:      server.URL,
		ProjectSlug:     "test-project",
		RepoID:          "c3d4e5f6-7890-12cd-ef01-345678901234",
		RepoOrigin:      "git@github.com:test/repo.git",
		CanonicalOrigin: "github.com/test/repo",
		Alias:           "test-agent",
		HumanName:       "Test Human",
	}
	cfg.Save()

	result, err := runPassthrough([]string{"list", "--status=open"})
	if err != nil {
		t.Fatalf("runPassthrough native list: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr: %s", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "test-001") {
		t.Errorf("stdout = %q, want to contain test-001", result.Stdout)
	}
}

func TestPassthrough_ErrorsWhenNoConfigAndNoBeads(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	beads.ResetCache()

	_, err := runPassthrough([]string{"list"})

	if err == nil {
		t.Fatal("expected error when no config and no beads, got nil")
	}
	if !strings.Contains(err.Error(), "bdh :init") {
		t.Errorf("expected error to suggest 'bdh :init', got: %v", err)
	}
}

func TestPassthrough_NativeModeUnsupportedCommand(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	beads.ResetCache()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/bdh/command":
			json.NewEncoder(w).Encode(map[string]any{
				"approved": true,
				"context":  map[string]any{"messages_waiting": 0, "beads_in_progress": []any{}},
			})
		case "/v1/chat/pending":
			json.NewEncoder(w).Encode(map[string]any{"pending": []any{}, "messages_waiting": 0})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		WorkspaceID:     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
		BeadhubURL:      server.URL,
		ProjectSlug:     "test-project",
		RepoID:          "c3d4e5f6-7890-12cd-ef01-345678901234",
		RepoOrigin:      "git@github.com:test/repo.git",
		CanonicalOrigin: "github.com/test/repo",
		Alias:           "test-agent",
		HumanName:       "Test Human",
	}
	cfg.Save()

	_, err := runPassthrough([]string{"search", "foo"})

	if err == nil {
		t.Fatal("expected native mode error for unsupported command, got nil")
	}
	if !strings.Contains(err.Error(), "not available in native mode") {
		t.Errorf("expected 'not available in native mode' in error, got: %v", err)
	}
}
