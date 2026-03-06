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
		{name: "comment command", args: []string{"comment", "add", "bdh-42", "hello"}, wantCmd: "comment"},
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
	supported := []string{"create", "list", "show", "update", "close", "ready", "blocked", "dep", "comment", "sync", "stats", "reopen"}
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
		if r.URL.Path == "/v1/tasks/blocked" && r.Method == "GET" {
			json.NewEncoder(w).Encode(aweb.TaskListResponse{
				Tasks: []aweb.TaskSummary{
					{TaskRef: "bdh-001", Title: "Blocked task", Priority: 1, TaskType: "task", Status: "open"},
				},
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
}

func TestNativeBlocked_Empty(t *testing.T) {
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/tasks/blocked" && r.Method == "GET" {
			json.NewEncoder(w).Encode(aweb.TaskListResponse{Tasks: []aweb.TaskSummary{}})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"blocked"})
	if err != nil {
		t.Fatalf("runNative blocked: %v", err)
	}
	if !strings.Contains(result.Stdout, "No blocked tasks") {
		t.Errorf("stdout = %q, want 'No blocked tasks'", result.Stdout)
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

// --- Bug fix tests ---

func TestPositionalArg_BooleanFlagBeforeRef(t *testing.T) {
	// MAJ-4: Boolean flags like --json don't take values. positionalArg should
	// not consume the next argument as a flag value.
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"--json", "bdh-001"}, "bdh-001"},
		{[]string{"bdh-001", "--json"}, "bdh-001"},
		{[]string{"--json"}, ""},
		{[]string{"--verbose", "bdh-001"}, "bdh-001"},
		// Flag with value should still skip the value
		{[]string{"--status", "open", "bdh-001"}, "bdh-001"},
		{[]string{"--status=open", "bdh-001"}, "bdh-001"},
	}
	for _, tt := range tests {
		got := positionalArg(tt.args)
		if got != tt.want {
			t.Errorf("positionalArg(%v) = %q, want %q", tt.args, got, tt.want)
		}
	}
}

func TestAllPositionalArgs_BooleanFlags(t *testing.T) {
	// Same bug as positionalArg: boolean flags should not consume next arg
	tests := []struct {
		args []string
		want []string
	}{
		{[]string{"--json", "bdh-001", "bdh-002"}, []string{"bdh-001", "bdh-002"}},
		{[]string{"bdh-001", "--reason", "done", "bdh-002"}, []string{"bdh-001", "bdh-002"}},
	}
	for _, tt := range tests {
		got := allPositionalArgs(tt.args)
		if len(got) != len(tt.want) {
			t.Errorf("allPositionalArgs(%v) = %v, want %v", tt.args, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("allPositionalArgs(%v)[%d] = %q, want %q", tt.args, i, got[i], tt.want[i])
			}
		}
	}
}

func TestNativeClose_AllFail_ExitCode(t *testing.T) {
	// MAJ-3: When all close operations fail, exit code should be non-zero
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/tasks/") && r.Method == "PATCH" {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"detail": "not found"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"close", "bdh-999"})
	if err != nil {
		t.Fatalf("runNative close: %v", err)
	}
	if result.ExitCode == 0 {
		t.Errorf("exit code = 0, want non-zero when close fails; stdout: %s", result.Stdout)
	}
}

func TestNativeReopen_HeldError(t *testing.T) {
	// SUG-4: nativeReopen should handle TaskHeldError with a specific message
	// like nativeUpdate does, not a generic "reopening task" wrapper.
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

	result, err := runNative(client, []string{"reopen", "bdh-001"})
	if err != nil {
		t.Fatalf("runNative reopen: %v", err)
	}
	if result.ExitCode != 1 {
		t.Errorf("exit code = %d, want 1", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "held by another agent") {
		t.Errorf("stderr = %q, want specific held-by-another-agent message", result.Stderr)
	}
}

func TestNativeSync_Message(t *testing.T) {
	// SUG-3: sync in native mode should output an explanatory message
	_, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {})

	result, err := runNative(client, []string{"sync"})
	if err != nil {
		t.Fatalf("runNative sync: %v", err)
	}
	if result.Stdout == "" {
		t.Errorf("sync should produce an explanatory message, got empty output")
	}
}

func TestParseNativeCommand_ErrorMessageSuggestsBdhInit(t *testing.T) {
	// SUG-2: Error for unsupported commands should suggest 'bdh :init', not 'bd init'
	_, _, err := parseNativeCommand([]string{"search", "foo"})
	if err == nil {
		t.Fatal("expected error for unsupported command")
	}
	if strings.Contains(err.Error(), "'bd init'") {
		t.Errorf("error should not suggest 'bd init', got: %v", err)
	}
	if !strings.Contains(err.Error(), "bdh :init") {
		t.Errorf("error should suggest 'bdh :init', got: %v", err)
	}
}

func TestFormatTaskLine_LeadingIconMatchesPriority(t *testing.T) {
	// MAJ-5: Leading icon should reflect priority, not always be a circle
	highPri := formatTaskLine(aweb.TaskSummary{TaskRef: "bdh-001", Priority: 1, TaskType: "task", Title: "High"})
	if !strings.HasPrefix(highPri, "●") {
		t.Errorf("P1 task should start with filled circle, got: %q", highPri)
	}

	lowPri := formatTaskLine(aweb.TaskSummary{TaskRef: "bdh-002", Priority: 4, TaskType: "task", Title: "Low"})
	if !strings.HasPrefix(lowPri, "○") {
		t.Errorf("P4 task should start with open circle, got: %q", lowPri)
	}
}

// --- JSON output tests ---

func TestNativeList_JSON(t *testing.T) {
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/tasks" && r.Method == "GET" {
			json.NewEncoder(w).Encode(aweb.TaskListResponse{
				Tasks: []aweb.TaskSummary{
					{TaskRef: "bdh-001", Title: "First", Priority: 1, TaskType: "task", Status: "open"},
					{TaskRef: "bdh-002", Title: "Second", Priority: 2, TaskType: "bug", Status: "open"},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"list", "--json"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr: %s", result.ExitCode, result.Stderr)
	}

	var resp aweb.TaskListResponse
	if err := json.Unmarshal([]byte(result.Stdout), &resp); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout: %s", err, result.Stdout)
	}
	if len(resp.Tasks) != 2 {
		t.Errorf("got %d tasks, want 2", len(resp.Tasks))
	}
	if resp.Tasks[0].TaskRef != "bdh-001" {
		t.Errorf("task[0].TaskRef = %q, want bdh-001", resp.Tasks[0].TaskRef)
	}
}

func TestNativeShow_JSON(t *testing.T) {
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/tasks/bdh-001" && r.Method == "GET" {
			json.NewEncoder(w).Encode(aweb.Task{
				TaskRef:     "bdh-001",
				Title:       "Test task",
				Description: "A description",
				Status:      "open",
				Priority:    2,
				TaskType:    "feature",
				CreatedAt:   "2026-03-05T10:00:00Z",
				UpdatedAt:   "2026-03-05T10:00:00Z",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"show", "bdh-001", "--json"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr: %s", result.ExitCode, result.Stderr)
	}

	var task aweb.Task
	if err := json.Unmarshal([]byte(result.Stdout), &task); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout: %s", err, result.Stdout)
	}
	if task.TaskRef != "bdh-001" {
		t.Errorf("task_ref = %q, want bdh-001", task.TaskRef)
	}
	if task.Title != "Test task" {
		t.Errorf("title = %q, want 'Test task'", task.Title)
	}
}

func TestNativeCreate_JSON(t *testing.T) {
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/tasks" && r.Method == "POST" {
			json.NewEncoder(w).Encode(aweb.Task{
				TaskRef:  "bdh-042",
				Title:    "New task",
				TaskType: "task",
				Status:   "open",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"create", "--title", "New task", "--json"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr: %s", result.ExitCode, result.Stderr)
	}

	var task aweb.Task
	if err := json.Unmarshal([]byte(result.Stdout), &task); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout: %s", err, result.Stdout)
	}
	if task.TaskRef != "bdh-042" {
		t.Errorf("task_ref = %q, want bdh-042", task.TaskRef)
	}
}

func TestNativeReady_JSON(t *testing.T) {
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

	result, err := runNative(client, []string{"ready", "--json"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr: %s", result.ExitCode, result.Stderr)
	}

	var resp aweb.TaskListResponse
	if err := json.Unmarshal([]byte(result.Stdout), &resp); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout: %s", err, result.Stdout)
	}
	if len(resp.Tasks) != 1 || resp.Tasks[0].TaskRef != "bdh-010" {
		t.Errorf("unexpected tasks: %+v", resp.Tasks)
	}
}

func TestNativeUpdate_JSON(t *testing.T) {
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/tasks/") && r.Method == "PATCH" {
			json.NewEncoder(w).Encode(aweb.TaskUpdateResponse{
				Task: aweb.Task{TaskRef: "bdh-001", Title: "Updated", Status: "in_progress"},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"update", "bdh-001", "--status", "in_progress", "--json"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr: %s", result.ExitCode, result.Stderr)
	}

	var resp aweb.TaskUpdateResponse
	if err := json.Unmarshal([]byte(result.Stdout), &resp); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout: %s", err, result.Stdout)
	}
	if resp.TaskRef != "bdh-001" {
		t.Errorf("task_ref = %q, want bdh-001", resp.TaskRef)
	}
}

func TestNativeClose_JSON(t *testing.T) {
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/tasks/") && r.Method == "PATCH" {
			ref := strings.TrimPrefix(r.URL.Path, "/v1/tasks/")
			json.NewEncoder(w).Encode(aweb.TaskUpdateResponse{
				Task: aweb.Task{TaskRef: ref, Title: "Task " + ref, Status: "closed"},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"close", "bdh-001", "bdh-002", "--json"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr: %s", result.ExitCode, result.Stderr)
	}

	var results []aweb.TaskUpdateResponse
	if err := json.Unmarshal([]byte(result.Stdout), &results); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout: %s", err, result.Stdout)
	}
	if len(results) != 2 {
		t.Errorf("got %d results, want 2", len(results))
	}
}

func TestNativeBlocked_JSON(t *testing.T) {
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/tasks/blocked" && r.Method == "GET" {
			json.NewEncoder(w).Encode(aweb.TaskListResponse{
				Tasks: []aweb.TaskSummary{
					{TaskRef: "bdh-001", Title: "Blocked", Priority: 1, TaskType: "task", Status: "open"},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"blocked", "--json"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr: %s", result.ExitCode, result.Stderr)
	}

	var resp aweb.TaskListResponse
	if err := json.Unmarshal([]byte(result.Stdout), &resp); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout: %s", err, result.Stdout)
	}
	if len(resp.Tasks) != 1 {
		t.Errorf("got %d tasks, want 1", len(resp.Tasks))
	}
}

func TestNativeStats_JSON(t *testing.T) {
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

	result, err := runNative(client, []string{"stats", "--json"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr: %s", result.ExitCode, result.Stderr)
	}

	var stats map[string]int
	if err := json.Unmarshal([]byte(result.Stdout), &stats); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout: %s", err, result.Stdout)
	}
	if stats["total"] != 4 {
		t.Errorf("total = %d, want 4", stats["total"])
	}
	if stats["open"] != 2 {
		t.Errorf("open = %d, want 2", stats["open"])
	}
}

func TestNativeReopen_JSON(t *testing.T) {
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/tasks/") && r.Method == "PATCH" {
			ref := strings.TrimPrefix(r.URL.Path, "/v1/tasks/")
			json.NewEncoder(w).Encode(aweb.TaskUpdateResponse{
				Task: aweb.Task{TaskRef: ref, Title: "Reopened", Status: "open"},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"reopen", "bdh-001", "--json"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr: %s", result.ExitCode, result.Stderr)
	}

	var resp aweb.TaskUpdateResponse
	if err := json.Unmarshal([]byte(result.Stdout), &resp); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout: %s", err, result.Stdout)
	}
	if resp.TaskRef != "bdh-001" {
		t.Errorf("task_ref = %q, want bdh-001", resp.TaskRef)
	}
}

func TestNativeList_JSON_Empty(t *testing.T) {
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/tasks" && r.Method == "GET" {
			json.NewEncoder(w).Encode(aweb.TaskListResponse{Tasks: []aweb.TaskSummary{}})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"list", "--json"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr: %s", result.ExitCode, result.Stderr)
	}

	// Even with no tasks, JSON mode should return valid JSON (not "No tasks found")
	var resp aweb.TaskListResponse
	if err := json.Unmarshal([]byte(result.Stdout), &resp); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout: %s", err, result.Stdout)
	}
	if len(resp.Tasks) != 0 {
		t.Errorf("got %d tasks, want 0", len(resp.Tasks))
	}
}

// --- Labels filter test ---

func TestNativeList_LabelsFilter(t *testing.T) {
	var gotLabels string
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/tasks" && r.Method == "GET" {
			gotLabels = r.URL.Query().Get("labels")
			json.NewEncoder(w).Encode(aweb.TaskListResponse{
				Tasks: []aweb.TaskSummary{
					{TaskRef: "bdh-001", Title: "Labeled task", Priority: 2, TaskType: "task", Status: "open",
						Labels: []string{"frontend", "urgent"}},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"list", "--labels", "frontend,urgent"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr: %s", result.ExitCode, result.Stderr)
	}
	if gotLabels != "frontend,urgent" {
		t.Errorf("labels query param = %q, want 'frontend,urgent'", gotLabels)
	}
	if !strings.Contains(result.Stdout, "bdh-001") {
		t.Errorf("stdout missing bdh-001")
	}
}

// --- Dep list tests ---

func TestNativeDep_List(t *testing.T) {
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/tasks/bdh-001" && r.Method == "GET" {
			json.NewEncoder(w).Encode(aweb.Task{
				TaskRef:  "bdh-001",
				Title:    "My task",
				Status:   "open",
				Priority: 2,
				TaskType: "task",
				BlockedBy: []aweb.TaskDepView{
					{TaskRef: "bdh-000", Title: "Prerequisite", Status: "open"},
				},
				Blocks: []aweb.TaskDepView{
					{TaskRef: "bdh-002", Title: "Downstream", Status: "open"},
				},
				CreatedAt: "2026-03-05T10:00:00Z",
				UpdatedAt: "2026-03-05T10:00:00Z",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"dep", "list", "bdh-001"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr: %s", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "bdh-000") {
		t.Errorf("stdout missing blocker bdh-000")
	}
	if !strings.Contains(result.Stdout, "bdh-002") {
		t.Errorf("stdout missing downstream bdh-002")
	}
}

func TestNativeDep_List_NoDeps(t *testing.T) {
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/tasks/bdh-001" && r.Method == "GET" {
			json.NewEncoder(w).Encode(aweb.Task{
				TaskRef: "bdh-001", Title: "No deps", Status: "open",
				Priority: 2, TaskType: "task",
				CreatedAt: "2026-03-05T10:00:00Z", UpdatedAt: "2026-03-05T10:00:00Z",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"dep", "list", "bdh-001"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(result.Stdout, "No dependencies") {
		t.Errorf("stdout = %q, want 'No dependencies'", result.Stdout)
	}
}

func TestNativeDep_List_JSON(t *testing.T) {
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/tasks/bdh-001" && r.Method == "GET" {
			json.NewEncoder(w).Encode(aweb.Task{
				TaskRef: "bdh-001", Title: "My task", Status: "open",
				Priority: 2, TaskType: "task",
				BlockedBy: []aweb.TaskDepView{{TaskRef: "bdh-000", Title: "Blocker", Status: "open"}},
				CreatedAt: "2026-03-05T10:00:00Z", UpdatedAt: "2026-03-05T10:00:00Z",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"dep", "list", "bdh-001", "--json"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr: %s", result.ExitCode, result.Stderr)
	}

	var data map[string][]aweb.TaskDepView
	if err := json.Unmarshal([]byte(result.Stdout), &data); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout: %s", err, result.Stdout)
	}
	if len(data["blocked_by"]) != 1 {
		t.Errorf("blocked_by count = %d, want 1", len(data["blocked_by"]))
	}
}

// --- Comment command tests ---

func TestNativeComment_Add(t *testing.T) {
	var gotRef, gotBody string
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/comments") && r.Method == "POST" {
			gotRef = strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/tasks/"), "/comments")
			var req map[string]string
			json.NewDecoder(r.Body).Decode(&req)
			gotBody = req["body"]
			json.NewEncoder(w).Encode(aweb.TaskComment{
				CommentID: "comment-001",
				TaskID:    "task-id",
				Body:      gotBody,
				CreatedAt: "2026-03-05T10:00:00Z",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"comment", "add", "bdh-001", "This is a comment"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr: %s", result.ExitCode, result.Stderr)
	}
	if gotRef != "bdh-001" {
		t.Errorf("ref = %q, want bdh-001", gotRef)
	}
	if gotBody != "This is a comment" {
		t.Errorf("body = %q, want 'This is a comment'", gotBody)
	}
	if !strings.Contains(result.Stdout, "comment") {
		t.Errorf("stdout = %q, want confirmation", result.Stdout)
	}
}

func TestNativeComment_List(t *testing.T) {
	author := "agent-abc"
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/comments") && r.Method == "GET" {
			json.NewEncoder(w).Encode(aweb.TaskCommentListResponse{
				Comments: []aweb.TaskComment{
					{CommentID: "c1", Body: "First comment", AuthorAgentID: &author, CreatedAt: "2026-03-05T10:00:00Z"},
					{CommentID: "c2", Body: "Second comment", CreatedAt: "2026-03-05T11:00:00Z"},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"comment", "list", "bdh-001"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr: %s", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "First comment") {
		t.Errorf("stdout missing 'First comment'")
	}
	if !strings.Contains(result.Stdout, "Second comment") {
		t.Errorf("stdout missing 'Second comment'")
	}
}

func TestNativeComment_List_Empty(t *testing.T) {
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/comments") && r.Method == "GET" {
			json.NewEncoder(w).Encode(aweb.TaskCommentListResponse{Comments: []aweb.TaskComment{}})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"comment", "list", "bdh-001"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(result.Stdout, "No comments") {
		t.Errorf("stdout = %q, want 'No comments'", result.Stdout)
	}
}

func TestNativeComment_List_JSON(t *testing.T) {
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/comments") && r.Method == "GET" {
			json.NewEncoder(w).Encode(aweb.TaskCommentListResponse{
				Comments: []aweb.TaskComment{
					{CommentID: "c1", Body: "A comment", CreatedAt: "2026-03-05T10:00:00Z"},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"comment", "list", "bdh-001", "--json"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr: %s", result.ExitCode, result.Stderr)
	}

	var resp aweb.TaskCommentListResponse
	if err := json.Unmarshal([]byte(result.Stdout), &resp); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout: %s", err, result.Stdout)
	}
	if len(resp.Comments) != 1 {
		t.Errorf("got %d comments, want 1", len(resp.Comments))
	}
}

func TestNativeComment_Add_JSON(t *testing.T) {
	server, client := nativeMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/comments") && r.Method == "POST" {
			json.NewEncoder(w).Encode(aweb.TaskComment{
				CommentID: "comment-001",
				Body:      "My comment",
				CreatedAt: "2026-03-05T10:00:00Z",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer server.Close()

	result, err := runNative(client, []string{"comment", "add", "bdh-001", "My comment", "--json"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr: %s", result.ExitCode, result.Stderr)
	}

	var comment aweb.TaskComment
	if err := json.Unmarshal([]byte(result.Stdout), &comment); err != nil {
		t.Fatalf("invalid JSON: %v\nstdout: %s", err, result.Stdout)
	}
	if comment.CommentID != "comment-001" {
		t.Errorf("comment_id = %q, want comment-001", comment.CommentID)
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
