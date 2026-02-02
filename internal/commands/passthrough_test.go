package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	aweb "github.com/awebai/aw"
	"github.com/beadhub/bdh/internal/client"
	"github.com/beadhub/bdh/internal/config"
)

func TestPassthrough_PreservesArgsWhenInvokingBd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a sh stub for bd")
	}

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	os.MkdirAll(".beads", 0755)

	// Stub out `bd` in PATH so we can assert the argv it receives.
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	bdPath := filepath.Join(binDir, "bd")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\"\n"
	if err := os.WriteFile(bdPath, []byte(script), 0755); err != nil {
		t.Fatalf("write bd stub: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Mock server that approves and returns no pending chats.
	var gotCommandLine string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/bdh/command":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if v, ok := body["command_line"].(string); ok {
				gotCommandLine = v
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"approved": true,
				"context": map[string]any{
					"messages_waiting":  0,
					"beads_in_progress": []any{},
				},
			})
			return
		case "/v1/chat/pending":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"pending":          []any{},
				"messages_waiting": 0,
			})
			return
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

	args := []string{
		"ready",
		"--filter",
		"has spaces",
		`--flag=weird"quotes"`,
		"--:jump-in",
		"reason with spaces",
	}

	result, err := runPassthrough(args)
	if err != nil {
		t.Fatalf("runPassthrough error: %v", err)
	}

	// `--:jump-in` and its message must be stripped, but everything else must be
	// forwarded to bd exactly (including spaces/quotes inside argv elements).
	wantForwarded := []string{"ready", "--filter", "has spaces", `--flag=weird"quotes"`}
	wantStdout := strings.Join(wantForwarded, "\n") + "\n"
	if result.Stdout != wantStdout {
		t.Fatalf("bd argv mismatch:\nstdout=%q\nwant=%q", result.Stdout, wantStdout)
	}

	// The server "command_line" is informational only, but it should reflect the
	// forwarded argv (minus bdh-specific flags).
	wantCommandLine := strings.Join(wantForwarded, " ")
	if gotCommandLine != wantCommandLine {
		t.Fatalf("server command_line mismatch: got=%q want=%q", gotCommandLine, wantCommandLine)
	}
}

func TestPassthrough_ReadyUsesBoundedTeamQuery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a sh stub for bd")
	}

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	os.MkdirAll(".beads", 0755)

	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	bdPath := filepath.Join(binDir, "bd")
	script := "#!/bin/sh\necho 'ready'\n"
	if err := os.WriteFile(bdPath, []byte(script), 0755); err != nil {
		t.Fatalf("write bd stub: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var gotQuery url.Values
	var gotAuthorization string
	var gotReservationsQuery string
	var gotReservationsAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/bdh/command":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"approved": true,
				"context": map[string]any{
					"messages_waiting":  0,
					"beads_in_progress": []any{},
				},
			})
			return
		case "/v1/chat/pending":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"pending":          []any{},
				"messages_waiting": 0,
			})
			return
		case "/v1/messages/inbox":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"messages": []any{},
				"count":    0,
			})
			return
		case "/v1/workspaces/team":
			gotQuery = r.URL.Query()
			gotAuthorization = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"workspaces": []any{},
				"count":      0,
			})
			return
		case "/v1/reservations":
			gotReservationsQuery = r.URL.RawQuery
			gotReservationsAuthorization = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"reservations": []any{},
				"count":        0,
			})
			return
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

	t.Setenv("BEADHUB_API_KEY", "test-api-key")

	_, err := runPassthrough([]string{"ready"})
	if err != nil {
		t.Fatalf("runPassthrough error: %v", err)
	}

	if gotAuthorization != "Bearer test-api-key" {
		t.Errorf("Authorization header = %q, want %q", gotAuthorization, "Bearer test-api-key")
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
	if gotQuery.Get("always_include_workspace_id") != cfg.WorkspaceID {
		t.Errorf("always_include_workspace_id = %q, want %q", gotQuery.Get("always_include_workspace_id"), cfg.WorkspaceID)
	}
	if gotQuery.Get("limit") != "16" {
		t.Errorf("limit = %q, want 16", gotQuery.Get("limit"))
	}
	if gotReservationsAuthorization != "Bearer test-api-key" {
		t.Errorf("reservations Authorization header = %q, want %q", gotReservationsAuthorization, "Bearer test-api-key")
	}
	if gotReservationsQuery != "" {
		t.Errorf("reservations query = %q, want empty", gotReservationsQuery)
	}
}

func TestFormatPassthroughOutput_ShowsYourFocusWhenNoClaims(t *testing.T) {
	result := &PassthroughResult{
		IsReadyCommand:   true,
		MyFocusApexID:    "beadhub-xyz",
		MyFocusApexTitle: "Last Epic",
	}

	output := formatPassthroughOutput(result)
	if !strings.Contains(output, "## Your Focus") {
		t.Fatalf("expected Your Focus section, got:\n%s", output)
	}
	if !strings.Contains(output, "beadhub-xyz \"Last Epic\"") {
		t.Fatalf("expected focus apex details, got:\n%s", output)
	}
	if strings.Contains(output, "## Your Current Epics") {
		t.Fatalf("did not expect current epics section, got:\n%s", output)
	}
}

func TestFormatPassthroughOutput_ShowsActiveLocks(t *testing.T) {
	now := time.Now()
	result := &PassthroughResult{
		IsReadyCommand: true,
		MyAlias:        "my-agent", // Set so we can filter out own locks
		Stdout:         "Ready issues:\n",
		ReadyLocks: []aweb.ReservationView{
			{
				ResourceKey: "src/api.py",
				HolderAlias: "claude-be", // Different from MyAlias, so should show
				ExpiresAt:   now.Add(3 * time.Minute).UTC().Format(time.RFC3339Nano),
				Metadata:    map[string]any{},
			},
		},
	}

	output := formatPassthroughOutput(result)
	if !strings.Contains(output, "## File Reservations") {
		t.Fatalf("expected File Reservations section, got:\n%s", output)
	}
	if !strings.Contains(output, "`src/api.py` — claude-be (expires in 3m)") {
		t.Fatalf("expected reservation details, got:\n%s", output)
	}
}

func TestFormatPassthroughOutput_JSONModeOutputsPureJSON(t *testing.T) {
	now := time.Now()
	result := &PassthroughResult{
		JSONMode:       true,
		IsReadyCommand: true,
		Stdout:         "[{\"bead_id\":\"bd-1\"}]\n",
		Stderr:         "",
		ExitCode:       0,
		ReadyLocks: []aweb.ReservationView{
			{
				ResourceKey: "src/api.py",
				HolderAlias: "claude-be",
				ExpiresAt:   now.Add(3 * time.Minute).UTC().Format(time.RFC3339Nano),
				Metadata:    map[string]any{},
			},
		},
	}

	output := formatPassthroughOutput(result)

	var decoded map[string]any
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("expected valid JSON output, got error %v:\n%s", err, output)
	}

	if _, ok := decoded["bd_stdout"]; !ok {
		t.Fatalf("expected bd_stdout in JSON output, got:\n%s", output)
	}
	if _, ok := decoded["ready_context"]; !ok {
		t.Fatalf("expected ready_context in JSON output, got:\n%s", output)
	}
	if strings.Contains(output, "ACTIVE RESERVATIONS:") {
		t.Fatalf("did not expect human output in JSON mode, got:\n%s", output)
	}
}

func TestPassthrough_RunsBdWhenServerUnreachable(t *testing.T) {
	// Setup: create temp dir with .beadhub config pointing to unreachable server
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	// Create .beads directory
	os.MkdirAll(".beads", 0755)

	// Create .beadhub config pointing to unreachable server
	cfg := &config.Config{
		WorkspaceID:     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
		BeadhubURL:      "http://localhost:59999", // unreachable
		ProjectSlug:     "test-project",
		RepoID:          "c3d4e5f6-7890-12cd-ef01-345678901234",
		RepoOrigin:      "git@github.com:test/repo.git",
		CanonicalOrigin: "github.com/test/repo",
		Alias:           "test-agent",
		HumanName:       "Test Human",
	}
	cfg.Save()

	// Run passthrough with a simple bd command
	result, err := runPassthrough([]string{"--version"})

	// Should NOT error - bd should still run
	if err != nil {
		t.Fatalf("runPassthrough should not error when server unreachable, got: %v", err)
	}

	// bd --version should succeed
	if result.ExitCode != 0 {
		t.Errorf("bd --version should succeed, got exit code %d, stderr: %s", result.ExitCode, result.Stderr)
	}
}

func TestPassthrough_ShowsWarningWhenServerUnreachable(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	os.MkdirAll(".beads", 0755)

	cfg := &config.Config{
		WorkspaceID:     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
		BeadhubURL:      "http://localhost:59999",
		ProjectSlug:     "test-project",
		RepoID:          "c3d4e5f6-7890-12cd-ef01-345678901234",
		RepoOrigin:      "git@github.com:test/repo.git",
		CanonicalOrigin: "github.com/test/repo",
		Alias:           "test-agent",
		HumanName:       "Test Human",
	}
	cfg.Save()

	result, _ := runPassthrough([]string{"--version"})

	// Should contain a warning about server being unreachable
	if !strings.Contains(result.Warning, "BeadHub unreachable") {
		t.Errorf("expected warning about unreachable server, got: %q", result.Warning)
	}
}

func TestPassthrough_RunsBdWhenApproved(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	os.MkdirAll(".beads", 0755)

	// Mock server that approves
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/bdh/command" {
			json.NewEncoder(w).Encode(map[string]any{
				"approved": true,
				"context": map[string]any{
					"messages_waiting":  0,
					"beads_in_progress": []any{},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
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

	result, err := runPassthrough([]string{"--version"})

	if err != nil {
		t.Fatalf("runPassthrough error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("bd --version should succeed, got exit code %d", result.ExitCode)
	}
	if result.Warning != "" {
		t.Errorf("should have no warning when approved, got: %q", result.Warning)
	}
}

func TestPassthrough_RejectsClaimWithError(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	os.MkdirAll(".beads", 0755)

	// Mock server that rejects
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/bdh/command" {
			json.NewEncoder(w).Encode(map[string]any{
				"approved": false,
				"reason":   "bd-42 is being worked on by other-agent (Maria)",
				"context": map[string]any{
					"messages_waiting": 1,
					"beads_in_progress": []any{
						map[string]any{
							"bead_id":      "bd-42",
							"workspace_id": "other-ws",
							"alias":        "other-agent",
							"human_name":   "Maria",
						},
					},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
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

	// Simulate claiming a bead that's already taken
	result, err := runPassthrough([]string{"update", "bd-42", "--status", "in_progress"})

	// Should return rejection result (not a Go error, but rejection info)
	if err != nil {
		t.Fatalf("runPassthrough should not return Go error, got: %v", err)
	}

	// Should have rejection info in the result
	if !result.Rejected {
		t.Error("result.Rejected should be true")
	}
	if !strings.Contains(result.RejectionReason, "bd-42") {
		t.Errorf("rejection reason should mention bd-42, got: %q", result.RejectionReason)
	}

	// bd should NOT have been run - verify by checking that output is empty
	if result.Stdout != "" || result.Stderr != "" {
		t.Errorf("bd should not have run, but got stdout: %q, stderr: %q", result.Stdout, result.Stderr)
	}
}

func TestPassthrough_RunsBdWhenServerReturns5xx(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	os.MkdirAll(".beads", 0755)

	// Mock server that returns 500
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("database error"))
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

	result, err := runPassthrough([]string{"--version"})

	// Should NOT error - bd should still run (non-blocking design)
	if err != nil {
		t.Fatalf("runPassthrough should not error on server 5xx, got: %v", err)
	}

	// Should have a warning about the error
	if !strings.Contains(result.Warning, "500") {
		t.Errorf("expected warning about 500 error, got: %q", result.Warning)
	}

	// bd should still have run
	if result.ExitCode != 0 {
		t.Errorf("bd --version should succeed even with server error")
	}
}

func TestPassthrough_EmptyArgsReturnsError(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	os.MkdirAll(".beads", 0755)

	cfg := &config.Config{
		WorkspaceID:     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
		BeadhubURL:      "http://localhost:8000",
		ProjectSlug:     "test-project",
		RepoID:          "c3d4e5f6-7890-12cd-ef01-345678901234",
		RepoOrigin:      "git@github.com:test/repo.git",
		CanonicalOrigin: "github.com/test/repo",
		Alias:           "test-agent",
		HumanName:       "Test Human",
	}
	cfg.Save()

	_, err := runPassthrough([]string{})

	if err == nil {
		t.Fatal("runPassthrough should error on empty args")
	}
	if !strings.Contains(err.Error(), "no command") {
		t.Errorf("error should mention no command, got: %v", err)
	}
}

func TestPassthrough_SyncsAfterMutationCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a sh stub for bd")
	}

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	os.MkdirAll(".beads", 0755)

	// Stub out `bd` in PATH. `bdh` should run `bd export` before syncing so the
	// JSONL reflects the latest mutations even in daemon mode.
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	bdPath := filepath.Join(binDir, "bd")
	script := `#!/bin/sh
set -e
cmd="$1"
shift || true
case "$cmd" in
  create)
    # Simulate successful create that returns JSON.
    echo '{"id":"bd-1","title":"Test","status":"open","priority":2,"issue_type":"task"}'
    ;;
  export)
    out=""
    while [ "$#" -gt 0 ]; do
      if [ "$1" = "-o" ]; then out="$2"; shift 2; continue; fi
      shift
    done
    mkdir -p "$(dirname "$out")"
    echo '{"id":"bd-1","title":"Test","status":"open","priority":2,"issue_type":"task"}' > "$out"
    ;;
  *)
    ;;
esac
`
	if err := os.WriteFile(bdPath, []byte(script), 0755); err != nil {
		t.Fatalf("write bd stub: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var syncCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/bdh/command" {
			json.NewEncoder(w).Encode(map[string]any{
				"approved": true,
				"context":  map[string]any{},
			})
			return
		}
		if r.URL.Path == "/v1/chat/pending" {
			_ = json.NewEncoder(w).Encode(map[string]any{"pending": []any{}, "messages_waiting": 0})
			return
		}
		if r.URL.Path == "/v1/bdh/sync" {
			syncCalled = true
			json.NewEncoder(w).Encode(map[string]any{
				"synced":       true,
				"issues_count": 1,
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
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

	_, err := runPassthrough([]string{"create", "--title", "Test", "--json"})
	if err != nil {
		t.Fatalf("runPassthrough error: %v", err)
	}
	if !syncCalled {
		t.Error("expected /v1/bdh/sync to be called after create")
	}
}

func TestPassthrough_DoesNotSyncOnBdFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a sh stub for bd")
	}

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	os.MkdirAll(".beads", 0755)
	os.WriteFile(".beads/issues.jsonl", []byte(`{"id":"bd-1"}`), 0644)

	// Stub out `bd` in PATH so the create command reliably fails.
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	bdPath := filepath.Join(binDir, "bd")
	script := "#!/bin/sh\nexit 1\n"
	if err := os.WriteFile(bdPath, []byte(script), 0755); err != nil {
		t.Fatalf("write bd stub: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var syncCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/bdh/command" {
			json.NewEncoder(w).Encode(map[string]any{"approved": true, "context": map[string]any{}})
			return
		}
		if r.URL.Path == "/v1/chat/pending" {
			_ = json.NewEncoder(w).Encode(map[string]any{"pending": []any{}, "messages_waiting": 0})
			return
		}
		if r.URL.Path == "/v1/bdh/sync" {
			syncCalled = true
			json.NewEncoder(w).Encode(map[string]any{"synced": true})
			return
		}
		w.WriteHeader(http.StatusNotFound)
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

	// Run a mutation command that will fail (create with no args)
	result, _ := runPassthrough([]string{"create"})

	// bd create without args should fail
	if result.ExitCode == 0 {
		t.Fatalf("expected create to fail in stub, got exit code 0")
	}

	// Sync should NOT be called when bd fails
	if syncCalled {
		t.Error("sync should NOT be called when bd command fails")
	}
}

func TestPassthrough_SyncFailureWarnsButDoesNotError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a sh stub for bd")
	}

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	os.MkdirAll(".beads", 0755)
	os.WriteFile(".beads/issues.jsonl", []byte(`{"id":"bd-1"}`), 0644)

	// Stub out `bd` in PATH (export no-op; create succeeds).
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	bdPath := filepath.Join(binDir, "bd")
	script := `#!/bin/sh
set -e
cmd="$1"
shift || true
case "$cmd" in
  create)
    echo '{"id":"bd-1"}'
    ;;
  export)
    exit 0
    ;;
  *)
    ;;
esac
`
	if err := os.WriteFile(bdPath, []byte(script), 0755); err != nil {
		t.Fatalf("write bd stub: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/bdh/command" {
			json.NewEncoder(w).Encode(map[string]any{"approved": true, "context": map[string]any{}})
			return
		}
		if r.URL.Path == "/v1/chat/pending" {
			_ = json.NewEncoder(w).Encode(map[string]any{"pending": []any{}, "messages_waiting": 0})
			return
		}
		if r.URL.Path == "/v1/bdh/sync" {
			// Sync fails with 500
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("database error"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
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

	result, err := runPassthrough([]string{"create", "--title", "Test", "--json"})
	if err != nil {
		t.Fatalf("runPassthrough error: %v", err)
	}
	if result.SyncWarning == "" {
		t.Fatalf("expected sync warning on server 500")
	}
	if !strings.Contains(result.SyncWarning, "500") {
		t.Fatalf("expected sync warning to mention status code, got: %q", result.SyncWarning)
	}
}

func TestPassthrough_RequiresBeadhubConfig(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	os.MkdirAll(".beads", 0755)
	// No .beadhub file

	result, err := runPassthrough([]string{"--version"})
	if err != nil {
		t.Fatalf("runPassthrough should succeed without .beadhub, got: %v", err)
	}
	if result == nil {
		t.Fatal("runPassthrough returned nil result")
	}
	if !strings.Contains(result.Warning, "No .beadhub config found") {
		t.Fatalf("expected warning about missing .beadhub, got: %q", result.Warning)
	}
}

// =============================================================================
// --:local-config flag tests
// =============================================================================

func TestParseLocalConfig_ExtractsPathAndStripsFlag(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantArgs    []string
		wantPath    string
		wantHasFlag bool
	}{
		{
			name:        "no local-config flag",
			args:        []string{"update", "bd-42", "--status", "in_progress"},
			wantArgs:    []string{"update", "bd-42", "--status", "in_progress"},
			wantPath:    "",
			wantHasFlag: false,
		},
		{
			name:        "local-config with path at end",
			args:        []string{"ready", "--:local-config", "/path/to/.beadhub-dev"},
			wantArgs:    []string{"ready"},
			wantPath:    "/path/to/.beadhub-dev",
			wantHasFlag: true,
		},
		{
			name:        "local-config with path in middle",
			args:        []string{"--:local-config", "/tmp/.beadhub", "show", "bd-42"},
			wantArgs:    []string{"show", "bd-42"},
			wantPath:    "/tmp/.beadhub",
			wantHasFlag: true,
		},
		{
			name:        "local-config with equals syntax",
			args:        []string{"list", "--:local-config=/custom/.beadhub", "--status", "open"},
			wantArgs:    []string{"list", "--status", "open"},
			wantPath:    "/custom/.beadhub",
			wantHasFlag: true,
		},
		{
			name:        "local-config with relative path",
			args:        []string{"--:local-config", ".beadhub-test", "ready"},
			wantArgs:    []string{"ready"},
			wantPath:    ".beadhub-test",
			wantHasFlag: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotArgs, gotPath, gotHasFlag := parseLocalConfig(tt.args)

			if gotHasFlag != tt.wantHasFlag {
				t.Errorf("hasFlag = %v, want %v", gotHasFlag, tt.wantHasFlag)
			}
			if gotPath != tt.wantPath {
				t.Errorf("path = %q, want %q", gotPath, tt.wantPath)
			}
			if len(gotArgs) != len(tt.wantArgs) {
				t.Errorf("args length = %d, want %d", len(gotArgs), len(tt.wantArgs))
			} else {
				for i := range gotArgs {
					if gotArgs[i] != tt.wantArgs[i] {
						t.Errorf("args[%d] = %q, want %q", i, gotArgs[i], tt.wantArgs[i])
					}
				}
			}
		})
	}
}

func TestParseLocalConfig_FlagWithoutPath(t *testing.T) {
	args := []string{"ready", "--:local-config"}
	_, path, hasFlag := parseLocalConfig(args)

	if !hasFlag {
		t.Error("should detect --:local-config flag")
	}
	if path != "" {
		t.Errorf("path should be empty when no value provided, got %q", path)
	}
}

func TestPassthrough_LocalConfigMissingPath(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	// Reset config path after test
	defer config.SetPath("")

	os.MkdirAll(".beads", 0755)

	// Create default config
	cfg := &config.Config{
		WorkspaceID:     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
		BeadhubURL:      "http://localhost:59999",
		ProjectSlug:     "test-project",
		RepoID:          "c3d4e5f6-7890-12cd-ef01-345678901234",
		RepoOrigin:      "git@github.com:test/repo.git",
		CanonicalOrigin: "github.com/test/repo",
		Alias:           "test-agent",
		HumanName:       "Test Human",
	}
	cfg.Save()

	// Run with --:local-config but no path (uses default config)
	result, err := runPassthrough([]string{"--:local-config", "--version"})

	// Should still work (falls back to empty path which means default)
	// --:local-config with no value means hasFlag=true, path="" -> no SetPath called
	if err != nil {
		t.Fatalf("runPassthrough error: %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("bd --version should succeed, got exit code %d", result.ExitCode)
	}
}

func TestPassthrough_LocalConfigUsesCustomPath(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	// Reset config path after test
	defer config.SetPath("")

	os.MkdirAll(".beads", 0755)

	// Create a custom config file in a different location
	customPath := tmpDir + "/.beadhub-dev"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/bdh/command" {
			json.NewEncoder(w).Encode(map[string]any{
				"approved": true,
				"context":  map[string]any{},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Write custom config to the custom path
	customCfg := &config.Config{
		WorkspaceID:     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
		BeadhubURL:      server.URL,
		ProjectSlug:     "test-project",
		RepoID:          "c3d4e5f6-7890-12cd-ef01-345678901234",
		RepoOrigin:      "git@github.com:test/repo.git",
		CanonicalOrigin: "github.com/test/repo",
		Alias:           "custom-agent",
		HumanName:       "Custom User",
	}
	config.SetPath(customPath)
	customCfg.Save()
	config.SetPath("") // Reset for the test

	// Run passthrough with --:local-config
	result, err := runPassthrough([]string{"--:local-config", customPath, "--version"})

	if err != nil {
		t.Fatalf("runPassthrough error: %v", err)
	}

	// Should have run successfully using the custom config
	if result.ExitCode != 0 {
		t.Errorf("bd --version should succeed, got exit code %d", result.ExitCode)
	}
}

// =============================================================================
// --:jump-in flag tests
// =============================================================================

func TestParseJumpIn_ExtractsMessageAndStripsFlag(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantArgs      []string
		wantMessage   string
		wantHasJumpIn bool
	}{
		{
			name:          "no jump-in flag",
			args:          []string{"update", "bd-42", "--status", "in_progress"},
			wantArgs:      []string{"update", "bd-42", "--status", "in_progress"},
			wantMessage:   "",
			wantHasJumpIn: false,
		},
		{
			name:          "jump-in with message at end",
			args:          []string{"update", "bd-42", "--status", "in_progress", "--:jump-in", "I'll handle the tests"},
			wantArgs:      []string{"update", "bd-42", "--status", "in_progress"},
			wantMessage:   "I'll handle the tests",
			wantHasJumpIn: true,
		},
		{
			name:          "jump-in with message in middle",
			args:          []string{"update", "bd-42", "--:jump-in", "Taking over API work", "--status", "in_progress"},
			wantArgs:      []string{"update", "bd-42", "--status", "in_progress"},
			wantMessage:   "Taking over API work",
			wantHasJumpIn: true,
		},
		{
			name:          "jump-in with equals syntax",
			args:          []string{"update", "bd-42", "--status", "in_progress", "--:jump-in=Finishing the feature"},
			wantArgs:      []string{"update", "bd-42", "--status", "in_progress"},
			wantMessage:   "Finishing the feature",
			wantHasJumpIn: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotArgs, gotMessage, gotHasJumpIn := parseJumpIn(tt.args)

			if gotHasJumpIn != tt.wantHasJumpIn {
				t.Errorf("hasJumpIn = %v, want %v", gotHasJumpIn, tt.wantHasJumpIn)
			}
			if gotMessage != tt.wantMessage {
				t.Errorf("message = %q, want %q", gotMessage, tt.wantMessage)
			}
			if len(gotArgs) != len(tt.wantArgs) {
				t.Errorf("args length = %d, want %d", len(gotArgs), len(tt.wantArgs))
			} else {
				for i := range gotArgs {
					if gotArgs[i] != tt.wantArgs[i] {
						t.Errorf("args[%d] = %q, want %q", i, gotArgs[i], tt.wantArgs[i])
					}
				}
			}
		})
	}
}

func TestParseJumpIn_RequiresMessage(t *testing.T) {
	// --:jump-in without a message should return empty message
	args := []string{"update", "bd-42", "--status", "in_progress", "--:jump-in"}
	_, message, hasJumpIn := parseJumpIn(args)

	if !hasJumpIn {
		t.Error("should detect --:jump-in flag")
	}
	if message != "" {
		t.Errorf("message should be empty when no value provided, got %q", message)
	}
}

func TestPassthrough_JumpInOverridesRejection(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	os.MkdirAll(".beads", 0755)
	os.WriteFile(".beads/issues.jsonl", []byte(`{"id":"bd-42","title":"Test","status":"open"}`), 0644)

	var messageSent bool
	var sentToAgentID string
	var sentBody string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/bdh/command" {
			// Server rejects the claim
			json.NewEncoder(w).Encode(map[string]any{
				"approved": false,
				"reason":   "bd-42 is being worked on by other-agent (Maria)",
				"context": map[string]any{
					"messages_waiting": 0,
					"beads_in_progress": []any{
						map[string]any{
							"bead_id":      "bd-42",
							"workspace_id": "other-ws-id",
							"alias":        "other-agent",
							"human_name":   "Maria",
						},
					},
				},
			})
			return
		}
		if r.URL.Path == "/v1/bdh/sync" {
			json.NewEncoder(w).Encode(map[string]any{
				"synced":       true,
				"issues_count": 1,
			})
			return
		}
		if r.URL.Path == "/v1/messages" {
			messageSent = true
			var req map[string]string
			json.NewDecoder(r.Body).Decode(&req)
			sentToAgentID = req["to_agent_id"]
			sentBody = req["body"]
			json.NewEncoder(w).Encode(map[string]any{
				"message_id":   "msg_123",
				"status":       "delivered",
				"delivered_at": "2025-01-01T00:00:00Z",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
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

	// Run with --:jump-in flag
	result, err := runPassthrough([]string{"update", "bd-42", "--status", "in_progress", "--:jump-in", "I'll handle the tests"})

	if err != nil {
		t.Fatalf("runPassthrough should not error with --:jump-in, got: %v", err)
	}

	// Result should NOT be marked as rejected (--:jump-in overrides)
	if result.Rejected {
		t.Error("result.Rejected should be false when --:jump-in is used")
	}

	// Should have sent notification to other agent
	if !messageSent {
		t.Error("should have sent notification to other agent")
	}
	if sentToAgentID != "other-ws-id" {
		t.Errorf("sent to wrong agent: got %q, want 'other-ws-id'", sentToAgentID)
	}
	if !strings.Contains(sentBody, "I'll handle the tests") {
		t.Errorf("message should contain jump-in reason, got: %q", sentBody)
	}
	if !strings.Contains(sentBody, "bd-42") {
		t.Errorf("message should mention the bead, got: %q", sentBody)
	}
}

func TestPassthrough_JumpInRequiresMessage(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	os.MkdirAll(".beads", 0755)

	cfg := &config.Config{
		WorkspaceID:     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
		BeadhubURL:      "http://localhost:59999", // won't be called
		ProjectSlug:     "test-project",
		RepoID:          "c3d4e5f6-7890-12cd-ef01-345678901234",
		RepoOrigin:      "git@github.com:test/repo.git",
		CanonicalOrigin: "github.com/test/repo",
		Alias:           "test-agent",
		HumanName:       "Test Human",
	}
	cfg.Save()

	// --:jump-in without message should error
	_, err := runPassthrough([]string{"update", "bd-42", "--status", "in_progress", "--:jump-in"})

	if err == nil {
		t.Fatal("runPassthrough should error when --:jump-in has no message")
	}
	if !strings.Contains(err.Error(), "message") {
		t.Errorf("error should mention message requirement, got: %v", err)
	}
}

func TestPassthrough_JumpInWarnsWhenBeadIDNotExtracted(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	os.MkdirAll(".beads", 0755)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/bdh/command" {
			// Server rejects (simulating another agent working)
			json.NewEncoder(w).Encode(map[string]any{
				"approved": false,
				"reason":   "bead is being worked on",
				"context": map[string]any{
					"messages_waiting":  0,
					"beads_in_progress": []any{},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
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

	// Use --:jump-in with a command that doesn't have a bead ID (like "show")
	result, err := runPassthrough([]string{"show", "--:jump-in", "testing"})

	if err != nil {
		t.Fatalf("runPassthrough error: %v", err)
	}

	// Should have a warning about not extracting bead ID
	if !strings.Contains(result.Warning, "couldn't extract bead ID") {
		t.Errorf("expected warning about bead ID extraction, got: %q", result.Warning)
	}
}

func TestPassthrough_CloseRejectsWhenOthersHaveClaims(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	os.MkdirAll(".beads", 0755)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/bdh/command" {
			// Server approves the close command, but reports other claimants
			json.NewEncoder(w).Encode(map[string]any{
				"approved": true,
				"context": map[string]any{
					"messages_waiting": 0,
					"beads_in_progress": []any{
						map[string]any{
							"bead_id":      "bd-42",
							"workspace_id": "other-ws-id",
							"alias":        "other-agent",
							"human_name":   "Maria",
						},
					},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
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

	// Try to close a bead that another agent is working on
	result, err := runPassthrough([]string{"close", "bd-42", "--reason", "done"})

	if err != nil {
		t.Fatalf("runPassthrough error: %v", err)
	}

	// Should be rejected - others are working on this bead
	if !result.Rejected {
		t.Error("result.Rejected should be true when others have claims")
	}
	if !strings.Contains(result.RejectionReason, "other-agent") {
		t.Errorf("rejection reason should mention other-agent, got: %q", result.RejectionReason)
	}
	if !strings.Contains(result.RejectionReason, "--:jump-in") {
		t.Errorf("rejection reason should suggest --:jump-in, got: %q", result.RejectionReason)
	}
}

func TestPassthrough_CloseWithJumpInWhenOthersHaveClaims(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	os.MkdirAll(".beads", 0755)

	var messageSent bool
	var sentToAgentID string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/bdh/command" {
			json.NewEncoder(w).Encode(map[string]any{
				"approved": true,
				"context": map[string]any{
					"messages_waiting": 0,
					"beads_in_progress": []any{
						map[string]any{
							"bead_id":      "bd-42",
							"workspace_id": "other-ws-id",
							"alias":        "other-agent",
							"human_name":   "Maria",
						},
					},
				},
			})
			return
		}
		if r.URL.Path == "/v1/messages" {
			messageSent = true
			var req map[string]string
			json.NewDecoder(r.Body).Decode(&req)
			sentToAgentID = req["to_agent_id"]
			json.NewEncoder(w).Encode(map[string]any{
				"message_id": "msg_123",
				"status":     "delivered",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
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

	// Close with --:jump-in to override
	result, err := runPassthrough([]string{"close", "bd-42", "--reason", "done", "--:jump-in", "Closing because tests pass"})

	if err != nil {
		t.Fatalf("runPassthrough error: %v", err)
	}

	// Should NOT be rejected - --:jump-in overrides
	if result.Rejected {
		t.Error("result.Rejected should be false when --:jump-in is used")
	}

	// Should have notified the other agent
	if !messageSent {
		t.Error("should have sent notification to other agent")
	}
	if sentToAgentID != "other-ws-id" {
		t.Errorf("sent to wrong agent: got %q, want 'other-ws-id'", sentToAgentID)
	}
}

func TestPassthrough_CloseWorksWhenOnlyClaimant(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	os.MkdirAll(".beads", 0755)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/bdh/command" {
			// Server approves, and we are the only claimant
			json.NewEncoder(w).Encode(map[string]any{
				"approved": true,
				"context": map[string]any{
					"messages_waiting": 0,
					"beads_in_progress": []any{
						map[string]any{
							"bead_id":      "bd-42",
							"workspace_id": "a1b2c3d4-5678-90ab-cdef-1234567890ab", // Same as our workspace
							"alias":        "test-agent",
							"human_name":   "Test Human",
						},
					},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
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

	// Close when we're the only claimant - should work without --:jump-in
	result, err := runPassthrough([]string{"close", "bd-42", "--reason", "done"})

	if err != nil {
		t.Fatalf("runPassthrough error: %v", err)
	}

	// Should NOT be rejected - we're the only claimant
	if result.Rejected {
		t.Errorf("result.Rejected should be false when we're the only claimant, got rejection: %s", result.RejectionReason)
	}
}

// =============================================================================
// Argument passthrough integrity tests
// =============================================================================

func TestExtractBeadID_FromArgs(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		wantID string
	}{
		{
			name:   "update command",
			args:   []string{"update", "bd-42", "--status", "in_progress"},
			wantID: "bd-42",
		},
		{
			name:   "close command",
			args:   []string{"close", "bd-42", "--reason", "done"},
			wantID: "bd-42",
		},
		{
			name:   "close with reason containing spaces",
			args:   []string{"close", "bd-42", "--reason", "task is complete"},
			wantID: "bd-42",
		},
		{
			name:   "show command (no bead ID extraction)",
			args:   []string{"show", "bd-42"},
			wantID: "",
		},
		{
			name:   "empty args",
			args:   []string{},
			wantID: "",
		},
		{
			name:   "only command",
			args:   []string{"update"},
			wantID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractBeadIDFromArgs(tt.args)
			if got != tt.wantID {
				t.Errorf("extractBeadIDFromArgs(%v) = %q, want %q", tt.args, got, tt.wantID)
			}
		})
	}
}

func TestIsCloseCommand_FromArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{
			name: "close command",
			args: []string{"close", "bd-42"},
			want: true,
		},
		{
			name: "close with reason",
			args: []string{"close", "bd-42", "--reason", "done"},
			want: true,
		},
		{
			name: "update command",
			args: []string{"update", "bd-42", "--status", "in_progress"},
			want: false,
		},
		{
			name: "show command",
			args: []string{"show", "bd-42"},
			want: false,
		},
		{
			name: "empty args",
			args: []string{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isCloseCommandFromArgs(tt.args)
			if got != tt.want {
				t.Errorf("isCloseCommandFromArgs(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

// =============================================================================
// Close command: Related work notification tests
// =============================================================================

func TestPassthrough_CloseShowsRelatedWorkInProgress(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a sh stub for bd")
	}

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	os.MkdirAll(".beads", 0755)

	// Stub out `bd` in PATH
	binDir := filepath.Join(tmpDir, "bin")
	os.MkdirAll(binDir, 0755)
	bdPath := filepath.Join(binDir, "bd")
	script := "#!/bin/sh\necho 'Closed bd-42'\n"
	os.WriteFile(bdPath, []byte(script), 0755)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Create issues.jsonl with related beads:
	// - bd-42: the one we're closing
	// - bd-43: depends on bd-42 (bd-42 blocks bd-43)
	// - bd-44: same parent epic as bd-42
	// - bd-45: unrelated
	issuesJSONL := `{"id":"bd-42","title":"Implement auth","status":"in_progress","dependencies":[{"issue_id":"bd-42","depends_on_id":"bd-40","type":"parent-child"}]}
{"id":"bd-43","title":"Add auth tests","status":"in_progress","dependencies":[{"issue_id":"bd-43","depends_on_id":"bd-42","type":"blocks"}]}
{"id":"bd-44","title":"Auth middleware","status":"in_progress","dependencies":[{"issue_id":"bd-44","depends_on_id":"bd-40","type":"parent-child"}]}
{"id":"bd-45","title":"Unrelated feature","status":"in_progress"}
`
	os.WriteFile(".beads/issues.jsonl", []byte(issuesJSONL), 0644)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/bdh/command" {
			// Server approves the close, and reports other agents working
			json.NewEncoder(w).Encode(map[string]any{
				"approved": true,
				"context": map[string]any{
					"messages_waiting": 0,
					"beads_in_progress": []any{
						// bd-43 is being worked on by claude-test
						map[string]any{
							"bead_id":      "bd-43",
							"workspace_id": "ws-test-id",
							"alias":        "claude-test",
							"human_name":   "Test Agent",
							"title":        "Add auth tests",
						},
						// bd-44 is being worked on by claude-fe
						map[string]any{
							"bead_id":      "bd-44",
							"workspace_id": "ws-fe-id",
							"alias":        "claude-fe",
							"human_name":   "Frontend Agent",
							"title":        "Auth middleware",
						},
						// bd-45 is being worked on by someone else (but unrelated)
						map[string]any{
							"bead_id":      "bd-45",
							"workspace_id": "ws-other-id",
							"alias":        "claude-other",
							"human_name":   "Other Agent",
							"title":        "Unrelated feature",
						},
					},
				},
			})
			return
		}
		if r.URL.Path == "/v1/bdh/sync" {
			json.NewEncoder(w).Encode(map[string]any{
				"synced":       true,
				"issues_count": 4,
			})
			return
		}
		if r.URL.Path == "/v1/chat/pending" {
			json.NewEncoder(w).Encode(map[string]any{
				"pending":          []any{},
				"messages_waiting": 0,
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
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

	result, err := runPassthrough([]string{"close", "bd-42", "--reason", "done"})

	if err != nil {
		t.Fatalf("runPassthrough error: %v", err)
	}

	// Should have related work suggestions
	if len(result.RelatedWork) == 0 {
		t.Fatal("expected related work suggestions, got none")
	}

	// Should include bd-43 (blocked by bd-42) and bd-44 (same parent)
	// but NOT bd-45 (unrelated)
	var foundBd43, foundBd44, foundBd45 bool
	for _, rw := range result.RelatedWork {
		switch rw.BeadID {
		case "bd-43":
			foundBd43 = true
			if rw.Alias != "claude-test" {
				t.Errorf("bd-43 should be worked on by claude-test, got %s", rw.Alias)
			}
			if rw.Title != "Add auth tests" {
				t.Errorf("bd-43 should have title 'Add auth tests', got %s", rw.Title)
			}
			if rw.Relation != "blocked by bd-42" {
				t.Errorf("bd-43 should have relation 'blocked by bd-42', got %s", rw.Relation)
			}
			if rw.HumanName != "Test Agent" {
				t.Errorf("bd-43 should have HumanName 'Test Agent', got %s", rw.HumanName)
			}
			if rw.WorkspaceID != "ws-test-id" {
				t.Errorf("bd-43 should have WorkspaceID 'ws-test-id', got %s", rw.WorkspaceID)
			}
		case "bd-44":
			foundBd44 = true
			if rw.Alias != "claude-fe" {
				t.Errorf("bd-44 should be worked on by claude-fe, got %s", rw.Alias)
			}
			if rw.Title != "Auth middleware" {
				t.Errorf("bd-44 should have title 'Auth middleware', got %s", rw.Title)
			}
			if rw.Relation != "same parent epic" {
				t.Errorf("bd-44 should have relation 'same parent epic', got %s", rw.Relation)
			}
			if rw.HumanName != "Frontend Agent" {
				t.Errorf("bd-44 should have HumanName 'Frontend Agent', got %s", rw.HumanName)
			}
			if rw.WorkspaceID != "ws-fe-id" {
				t.Errorf("bd-44 should have WorkspaceID 'ws-fe-id', got %s", rw.WorkspaceID)
			}
		case "bd-45":
			foundBd45 = true
		}
	}

	if !foundBd43 {
		t.Error("expected bd-43 (blocked by bd-42) in related work")
	}
	if !foundBd44 {
		t.Error("expected bd-44 (same parent as bd-42) in related work")
	}
	if foundBd45 {
		t.Error("bd-45 should NOT be in related work (unrelated)")
	}
}

func TestPassthrough_CloseOutputFormatsRelatedWorkSuggestions(t *testing.T) {
	result := &PassthroughResult{
		Stdout:   "Closed bd-42\n",
		ExitCode: 0,
		RelatedWork: []RelatedWorkItem{
			{
				BeadID:      "bd-43",
				Title:       "Add auth tests",
				Alias:       "claude-test",
				HumanName:   "Test Agent",
				WorkspaceID: "ws-test-id",
				Relation:    "blocked by bd-42",
			},
			{
				BeadID:      "bd-44",
				Title:       "Auth middleware",
				Alias:       "claude-fe",
				HumanName:   "Frontend Agent",
				WorkspaceID: "ws-fe-id",
				Relation:    "same parent epic",
			},
		},
	}

	output := formatPassthroughOutput(result)

	// Should show bd output first
	if !strings.Contains(output, "Closed bd-42") {
		t.Errorf("expected bd output, got:\n%s", output)
	}

	// Should show RELATED WORK IN PROGRESS section
	if !strings.Contains(output, "RELATED WORK IN PROGRESS:") {
		t.Errorf("expected RELATED WORK IN PROGRESS section, got:\n%s", output)
	}

	// Should show each related bead with agent info
	if !strings.Contains(output, "bd-43") || !strings.Contains(output, "claude-test") {
		t.Errorf("expected bd-43 and claude-test in output, got:\n%s", output)
	}
	if !strings.Contains(output, "bd-44") || !strings.Contains(output, "claude-fe") {
		t.Errorf("expected bd-44 and claude-fe in output, got:\n%s", output)
	}

	// Should suggest sending mail to specific agents
	if !strings.Contains(output, "bdh :aweb mail send claude-test") {
		t.Errorf("expected suggestion to send to claude-test, got:\n%s", output)
	}
	if !strings.Contains(output, "bdh :aweb mail send claude-fe") {
		t.Errorf("expected suggestion to send to claude-fe, got:\n%s", output)
	}
}

func TestFormatPassthroughOutput_SortsApexes(t *testing.T) {
	result := &PassthroughResult{
		IsReadyCommand: true,
		MyClaims: []client.Claim{
			{BeadID: "bd-1", ApexID: "bd-3", ApexTitle: "Third"},
			{BeadID: "bd-2", ApexID: "bd-1", ApexTitle: "First"},
			{BeadID: "bd-3", ApexID: "bd-2", ApexTitle: "Second"},
		},
	}

	output := formatPassthroughOutput(result)

	first := strings.Index(output, "bd-1 \"First\"")
	second := strings.Index(output, "bd-2 \"Second\"")
	third := strings.Index(output, "bd-3 \"Third\"")

	if first == -1 || second == -1 || third == -1 {
		t.Fatalf("expected apex entries in output, got:\n%s", output)
	}
	if first >= second || second >= third {
		t.Fatalf("expected apex entries sorted by id, got:\n%s", output)
	}
}

func TestPassthrough_CloseNoSuggestionsWhenNoRelatedWork(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a sh stub for bd")
	}

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	os.MkdirAll(".beads", 0755)

	binDir := filepath.Join(tmpDir, "bin")
	os.MkdirAll(binDir, 0755)
	bdPath := filepath.Join(binDir, "bd")
	script := "#!/bin/sh\necho 'Closed bd-42'\n"
	os.WriteFile(bdPath, []byte(script), 0755)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// No related beads
	issuesJSONL := `{"id":"bd-42","title":"Implement auth","status":"in_progress"}
`
	os.WriteFile(".beads/issues.jsonl", []byte(issuesJSONL), 0644)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/bdh/command" {
			json.NewEncoder(w).Encode(map[string]any{
				"approved": true,
				"context": map[string]any{
					"messages_waiting":  0,
					"beads_in_progress": []any{},
				},
			})
			return
		}
		if r.URL.Path == "/v1/bdh/sync" {
			json.NewEncoder(w).Encode(map[string]any{"synced": true})
			return
		}
		if r.URL.Path == "/v1/chat/pending" {
			json.NewEncoder(w).Encode(map[string]any{"pending": []any{}})
			return
		}
		w.WriteHeader(http.StatusNotFound)
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

	result, _ := runPassthrough([]string{"close", "bd-42", "--reason", "done"})
	output := formatPassthroughOutput(result)

	// Should NOT show RELATED WORK section when no related work
	if strings.Contains(output, "RELATED WORK IN PROGRESS") {
		t.Errorf("should not show related work section when none exists, got:\n%s", output)
	}
}

func TestPassthrough_JumpInNotNeededWhenApproved(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	os.MkdirAll(".beads", 0755)

	var messageSent bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/bdh/command" {
			// Server approves (no conflict)
			json.NewEncoder(w).Encode(map[string]any{
				"approved": true,
				"context": map[string]any{
					"messages_waiting":  0,
					"beads_in_progress": []any{},
				},
			})
			return
		}
		if r.URL.Path == "/v1/messages" {
			messageSent = true
			json.NewEncoder(w).Encode(map[string]any{
				"message_id": "msg_123",
				"status":     "delivered",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
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

	// --:jump-in when already approved should still work but not send notification
	_, err := runPassthrough([]string{"--version", "--:jump-in", "Just in case"})

	if err != nil {
		t.Fatalf("runPassthrough error: %v", err)
	}

	// Should NOT send notification when already approved (no one to notify)
	if messageSent {
		t.Error("should not send notification when command is approved")
	}
}

func TestIsClaimCommand(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{
			name: "update with --status in_progress",
			args: []string{"update", "bd-42", "--status", "in_progress"},
			want: true,
		},
		{
			name: "update with --status=in_progress",
			args: []string{"update", "bd-42", "--status=in_progress"},
			want: true,
		},
		{
			name: "update with -s in_progress",
			args: []string{"update", "bd-42", "-s", "in_progress"},
			want: true,
		},
		{
			name: "update with other status",
			args: []string{"update", "bd-42", "--status", "closed"},
			want: false,
		},
		{
			name: "close command",
			args: []string{"close", "bd-42"},
			want: false,
		},
		{
			name: "show command",
			args: []string{"show", "bd-42"},
			want: false,
		},
		{
			name: "update without status",
			args: []string{"update", "bd-42", "--priority", "1"},
			want: false,
		},
		{
			name: "empty args",
			args: []string{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isClaimCommand(tt.args); got != tt.want {
				t.Errorf("isClaimCommand(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestFormatPassthroughOutput_YourFocusNotRecentContext(t *testing.T) {
	// Test that "Your Focus" is used instead of "RECENT CONTEXT"
	result := &PassthroughResult{
		IsReadyCommand:   true,
		Stdout:           "Ready issues:\n",
		MyFocusApexID:    "epic-xyz",
		MyFocusApexTitle: "Test Epic",
	}

	output := formatPassthroughOutput(result)

	if strings.Contains(output, "RECENT CONTEXT") {
		t.Error("output should use 'Your Focus', not 'RECENT CONTEXT'")
	}
	if !strings.Contains(output, "## Your Focus") {
		t.Error("expected '## Your Focus' section")
	}
}

func TestFormatPassthroughOutput_TeamStatusShowsFocusApex(t *testing.T) {
	// Test that team status shows focus apex for members, not just claims
	result := &PassthroughResult{
		IsReadyCommand: true,
		Stdout:         "Ready issues:\n",
		TeamStatus: []client.Workspace{
			{
				Alias:          "agent-with-focus",
				FocusApexID:    "epic-42",
				FocusApexTitle: "Agent's Epic Focus",
				// No claims, just focus
			},
			{
				Alias:          "agent-with-claims",
				FocusApexID:    "epic-43",
				FocusApexTitle: "Claimed Epic",
				Claims: []client.Claim{
					{BeadID: "bd-100", Title: "Active task"},
				},
			},
		},
	}

	output := formatPassthroughOutput(result)

	// Team status should show the focus apex for agents
	if !strings.Contains(output, "agent-with-focus") {
		t.Error("expected agent-with-focus to appear in team status")
	}
	if !strings.Contains(output, "epic-42") || !strings.Contains(output, "Agent's Epic Focus") {
		t.Error("expected focus apex details for agent-with-focus")
	}
	if !strings.Contains(output, "agent-with-claims") {
		t.Error("expected agent-with-claims to appear in team status")
	}
}

func TestIsWorkspaceRecentlyActive(t *testing.T) {
	now := time.Now()
	threshold := now.Add(-6 * time.Hour)

	tests := []struct {
		name           string
		focusUpdatedAt string
		lastSeen       string
		want           bool
	}{
		{
			name:           "recent focus update",
			focusUpdatedAt: now.Add(-1 * time.Hour).Format(time.RFC3339),
			lastSeen:       now.Add(-10 * time.Hour).Format(time.RFC3339),
			want:           true, // Uses FocusUpdatedAt which is recent
		},
		{
			name:           "old focus but recent last seen",
			focusUpdatedAt: now.Add(-10 * time.Hour).Format(time.RFC3339),
			lastSeen:       now.Add(-1 * time.Hour).Format(time.RFC3339),
			want:           true, // OR logic: LastSeen is recent, so include
		},
		{
			name:           "both timestamps old",
			focusUpdatedAt: now.Add(-10 * time.Hour).Format(time.RFC3339),
			lastSeen:       now.Add(-8 * time.Hour).Format(time.RFC3339),
			want:           false, // Both are old, exclude
		},
		{
			name:           "no focus, recent last seen",
			focusUpdatedAt: "",
			lastSeen:       now.Add(-2 * time.Hour).Format(time.RFC3339),
			want:           true, // Falls back to LastSeen which is recent
		},
		{
			name:           "no focus, old last seen",
			focusUpdatedAt: "",
			lastSeen:       now.Add(-10 * time.Hour).Format(time.RFC3339),
			want:           false,
		},
		{
			name:           "no timestamps",
			focusUpdatedAt: "",
			lastSeen:       "",
			want:           true, // Conservative: include if we can't determine
		},
		{
			name:           "invalid timestamps",
			focusUpdatedAt: "not-a-date",
			lastSeen:       "also-not-a-date",
			want:           true, // Conservative: include if we can't parse
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws := client.Workspace{
				FocusUpdatedAt: tt.focusUpdatedAt,
				LastSeen:       tt.lastSeen,
			}
			got := isWorkspaceRecentlyActive(ws, threshold)
			if got != tt.want {
				t.Errorf("isWorkspaceRecentlyActive() = %v, want %v", got, tt.want)
			}
		})
	}
}
