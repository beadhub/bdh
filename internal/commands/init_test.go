package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/awebai/aw/awconfig"
	"github.com/beadhub/bdh/internal/beads"
	"github.com/beadhub/bdh/internal/config"
)

func resetInitFlags() {
	initURL = ""
	initAlias = ""
	initHuman = ""
	initProject = ""
	initRole = ""
	initUpdate = false
	initInjectDocs = false
}

func setupTempWorkspace(t *testing.T) string {
	t.Helper()

	resetInitFlags()

	tmpDir := t.TempDir()
	t.Setenv("AW_CONFIG_PATH", filepath.Join(tmpDir, "aw-config.yaml"))
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	_ = os.Chdir(tmpDir)

	// Ensure beads path discovery doesn't point at the real repo.
	beads.ResetCache()

	// Create .beads dir and a dummy DB file so :init doesn't try to run `bd init`.
	if err := os.MkdirAll(".beads", 0755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	if err := os.WriteFile(beads.DatabasePath(), []byte(""), 0600); err != nil {
		t.Fatalf("write beads.db: %v", err)
	}

	return tmpDir
}

func TestInitCommand_CreatesBeadhubFile(t *testing.T) {
	tmpDir := setupTempWorkspace(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/init" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var req struct {
			RepoOrigin  string `json:"repo_origin"`
			Alias       string `json:"alias"`
			HumanName   string `json:"human_name"`
			ProjectSlug string `json:"project_slug"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		if req.RepoOrigin != "git@github.com:test/repo.git" {
			t.Fatalf("repo_origin=%q want %q", req.RepoOrigin, "git@github.com:test/repo.git")
		}
		if req.Alias != "test-agent" {
			t.Fatalf("alias=%q want %q", req.Alias, "test-agent")
		}
		if req.HumanName != "Test Human" {
			t.Fatalf("human_name=%q want %q", req.HumanName, "Test Human")
		}
		if req.ProjectSlug != "test-project" {
			t.Fatalf("project_slug=%q want %q", req.ProjectSlug, "test-project")
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":           "ok",
			"api_key":          "aw_sk_123456789012345678901234567890123456",
			"project_id":       "test-project-uuid-1234",
			"project_slug":     req.ProjectSlug,
			"repo_id":          "c3d4e5f6-7890-12cd-ef01-345678901234",
			"canonical_origin": "github.com/test/repo",
			"workspace_id":     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
			"alias":            req.Alias,
			"created":          true,
		})
	}))
	defer server.Close()

	t.Setenv("BEADHUB_URL", server.URL)
	t.Setenv("BEADHUB_REPO_ORIGIN", "git@github.com:test/repo.git")
	t.Setenv("BEADHUB_ALIAS", "test-agent")
	t.Setenv("BEADHUB_HUMAN", "Test Human")
	t.Setenv("BEADHUB_PROJECT", "test-project")

	if err := runInit(); err != nil {
		t.Fatalf("runInit() error: %v", err)
	}

	if _, err := os.Stat(config.FileName); os.IsNotExist(err) {
		t.Fatal(".beadhub file not created")
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error: %v", err)
	}

	if cfg.BeadhubURL != server.URL {
		t.Errorf("BeadhubURL = %q, want %q", cfg.BeadhubURL, server.URL)
	}
	if cfg.RepoOrigin != "git@github.com:test/repo.git" {
		t.Errorf("RepoOrigin = %q, want %q", cfg.RepoOrigin, "git@github.com:test/repo.git")
	}
	if cfg.Alias != "test-agent" {
		t.Errorf("Alias = %q, want %q", cfg.Alias, "test-agent")
	}
	if cfg.HumanName != "Test Human" {
		t.Errorf("HumanName = %q, want %q", cfg.HumanName, "Test Human")
	}
	if cfg.ProjectSlug != "test-project" {
		t.Errorf("ProjectSlug = %q, want %q", cfg.ProjectSlug, "test-project")
	}
	if cfg.RepoID != "c3d4e5f6-7890-12cd-ef01-345678901234" {
		t.Errorf("RepoID = %q, want %q", cfg.RepoID, "c3d4e5f6-7890-12cd-ef01-345678901234")
	}
	if cfg.CanonicalOrigin != "github.com/test/repo" {
		t.Errorf("CanonicalOrigin = %q, want %q", cfg.CanonicalOrigin, "github.com/test/repo")
	}
	if cfg.WorkspaceID != "a1b2c3d4-5678-90ab-cdef-1234567890ab" {
		t.Errorf("WorkspaceID = %q, want %q", cfg.WorkspaceID, "a1b2c3d4-5678-90ab-cdef-1234567890ab")
	}

	if _, err := os.Stat(filepath.Join(tmpDir, ".env.beadhub")); !os.IsNotExist(err) {
		t.Fatalf(".env.beadhub should not exist (API keys are stored in the global aw config)")
	}

	ctxPath := filepath.Join(tmpDir, ".aw", "context")
	ctx, err := awconfig.LoadWorktreeContextFrom(ctxPath)
	if err != nil {
		t.Fatalf("loading .aw/context: %v", err)
	}
	if strings.TrimSpace(ctx.DefaultAccount) == "" {
		t.Fatalf(".aw/context default_account is empty")
	}

	serverName, err := awconfig.DeriveServerNameFromURL(server.URL)
	if err != nil {
		t.Fatalf("derive server name: %v", err)
	}
	if got := ctx.ServerAccounts[serverName]; got != ctx.DefaultAccount {
		t.Fatalf(".aw/context server_accounts[%q]=%q want %q", serverName, got, ctx.DefaultAccount)
	}

	global, err := awconfig.LoadGlobal()
	if err != nil {
		t.Fatalf("loading aw global config: %v", err)
	}
	acct, ok := global.Accounts[ctx.DefaultAccount]
	if !ok {
		t.Fatalf("global config missing account %q", ctx.DefaultAccount)
	}
	if acct.APIKey != "aw_sk_123456789012345678901234567890123456" {
		t.Fatalf("global account api_key=%q want %q", acct.APIKey, "aw_sk_123456789012345678901234567890123456")
	}
	if acct.DefaultProject != "test-project" {
		t.Fatalf("global account default_project=%q want %q", acct.DefaultProject, "test-project")
	}
	if acct.AgentID != cfg.WorkspaceID {
		t.Fatalf("global account agent_id=%q want %q", acct.AgentID, cfg.WorkspaceID)
	}
	if acct.AgentAlias != cfg.Alias {
		t.Fatalf("global account agent_alias=%q want %q", acct.AgentAlias, cfg.Alias)
	}

	srv, ok := global.Servers[acct.Server]
	if !ok {
		t.Fatalf("global config missing server %q for account %q", acct.Server, ctx.DefaultAccount)
	}
	if srv.URL != server.URL {
		t.Fatalf("global server url=%q want %q", srv.URL, server.URL)
	}
}

func TestInitCommand_SucceedsIfAlreadyInitialized(t *testing.T) {
	_ = setupTempWorkspace(t)

	if err := os.WriteFile(config.FileName, []byte("workspace_id: existing"), 0600); err != nil {
		t.Fatalf("write .beadhub: %v", err)
	}

	if err := runInit(); err != nil {
		t.Fatalf("runInit() should succeed when .beadhub exists, got error: %v", err)
	}
}

func TestInitCommand_FailsIfServerUnreachable(t *testing.T) {
	_ = setupTempWorkspace(t)

	t.Setenv("BEADHUB_URL", "http://localhost:59999")
	t.Setenv("BEADHUB_REPO_ORIGIN", "git@github.com:test/repo.git")
	t.Setenv("BEADHUB_ALIAS", "test-agent")
	t.Setenv("BEADHUB_HUMAN", "Test Human")
	t.Setenv("BEADHUB_PROJECT", "test-project")

	if err := runInit(); err == nil {
		t.Fatal("runInit() should error when server unreachable")
	}
}

func TestInitCommand_FailsWithInvalidRole(t *testing.T) {
	_ = setupTempWorkspace(t)

	t.Setenv("BEADHUB_REPO_ORIGIN", "git@github.com:test/repo.git")
	t.Setenv("BEADHUB_ROLE", "invalid role with too many words")

	err := runInit()
	if err == nil {
		t.Fatal("runInit() should error when role is invalid")
	}
	if !strings.Contains(err.Error(), "invalid role") {
		t.Fatalf("expected invalid role error, got: %v", err)
	}
}

func TestInitCommand_AddsToGitignore(t *testing.T) {
	setupTempWorkspace(t)

	// Create existing .gitignore without .beadhub/.aw/
	_ = os.WriteFile(".gitignore", []byte("node_modules/\n"), 0644)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/init" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":           "ok",
			"api_key":          "aw_sk_123456789012345678901234567890123456",
			"project_id":       "proj-1",
			"project_slug":     "test-project",
			"repo_id":          "c3d4e5f6-7890-12cd-ef01-345678901234",
			"canonical_origin": "github.com/test/repo",
			"workspace_id":     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
			"alias":            "test-agent",
			"created":          true,
		})
	}))
	defer server.Close()

	t.Setenv("BEADHUB_URL", server.URL)
	t.Setenv("BEADHUB_REPO_ORIGIN", "git@github.com:test/repo.git")
	t.Setenv("BEADHUB_ALIAS", "test-agent")
	t.Setenv("BEADHUB_HUMAN", "Test Human")
	t.Setenv("BEADHUB_PROJECT", "test-project")

	if err := runInit(); err != nil {
		t.Fatalf("runInit() error: %v", err)
	}

	content, _ := os.ReadFile(".gitignore")
	if !containsLine(string(content), ".beadhub") {
		t.Error(".gitignore should contain .beadhub")
	}
	if !containsLine(string(content), ".aw/") {
		t.Error(".gitignore should contain .aw/")
	}
}

func TestInitCommand_CreatesGitignoreIfMissing(t *testing.T) {
	setupTempWorkspace(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/init" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":           "ok",
			"api_key":          "aw_sk_123456789012345678901234567890123456",
			"project_id":       "proj-1",
			"project_slug":     "test-project",
			"repo_id":          "c3d4e5f6-7890-12cd-ef01-345678901234",
			"canonical_origin": "github.com/test/repo",
			"workspace_id":     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
			"alias":            "test-agent",
			"created":          true,
		})
	}))
	defer server.Close()

	t.Setenv("BEADHUB_URL", server.URL)
	t.Setenv("BEADHUB_REPO_ORIGIN", "git@github.com:test/repo.git")
	t.Setenv("BEADHUB_ALIAS", "test-agent")
	t.Setenv("BEADHUB_HUMAN", "Test Human")
	t.Setenv("BEADHUB_PROJECT", "test-project")

	if err := runInit(); err != nil {
		t.Fatalf("runInit() error: %v", err)
	}

	content, err := os.ReadFile(".gitignore")
	if err != nil {
		t.Fatal(".gitignore should be created")
	}
	if !containsLine(string(content), ".beadhub") {
		t.Error(".gitignore should contain .beadhub")
	}
	if !containsLine(string(content), ".aw/") {
		t.Error(".gitignore should contain .aw/")
	}
}

func TestInitCommand_DoesNotDuplicateGitignoreEntry(t *testing.T) {
	setupTempWorkspace(t)

	// Create .gitignore with existing .beadhub entry
	_ = os.WriteFile(".gitignore", []byte(".beadhub\nnode_modules/\n"), 0644)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/init" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":           "ok",
			"api_key":          "aw_sk_123456789012345678901234567890123456",
			"project_id":       "proj-1",
			"project_slug":     "test-project",
			"repo_id":          "c3d4e5f6-7890-12cd-ef01-345678901234",
			"canonical_origin": "github.com/test/repo",
			"workspace_id":     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
			"alias":            "test-agent",
			"created":          true,
		})
	}))
	defer server.Close()

	t.Setenv("BEADHUB_URL", server.URL)
	t.Setenv("BEADHUB_REPO_ORIGIN", "git@github.com:test/repo.git")
	t.Setenv("BEADHUB_ALIAS", "test-agent")
	t.Setenv("BEADHUB_HUMAN", "Test Human")
	t.Setenv("BEADHUB_PROJECT", "test-project")

	if err := runInit(); err != nil {
		t.Fatalf("runInit() error: %v", err)
	}

	content, _ := os.ReadFile(".gitignore")
	if countLines(string(content), ".beadhub") != 1 {
		t.Errorf(".beadhub appears %d times in .gitignore, want 1", countLines(string(content), ".beadhub"))
	}
}

func TestInitCommand_UsesProjectEnvVar(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AW_CONFIG_PATH", filepath.Join(tmpDir, "aw-config.yaml"))
	projectDir := filepath.Join(tmpDir, "Some-Directory")
	_ = os.MkdirAll(filepath.Join(projectDir, ".beads"), 0755)

	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	_ = os.Chdir(projectDir)

	beads.ResetCache()
	_ = os.WriteFile(beads.DatabasePath(), []byte(""), 0600)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/init" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		var req struct {
			ProjectSlug string `json:"project_slug"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.ProjectSlug != "my-actual-project" {
			t.Fatalf("server received project_slug=%q want %q", req.ProjectSlug, "my-actual-project")
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":           "ok",
			"api_key":          "aw_sk_123456789012345678901234567890123456",
			"project_id":       "proj-1",
			"project_slug":     req.ProjectSlug,
			"repo_id":          "c3d4e5f6-7890-12cd-ef01-345678901234",
			"canonical_origin": "github.com/test/repo",
			"workspace_id":     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
			"alias":            "test-agent",
			"created":          true,
		})
	}))
	defer server.Close()

	t.Setenv("BEADHUB_URL", server.URL)
	t.Setenv("BEADHUB_REPO_ORIGIN", "git@github.com:test/repo.git")
	t.Setenv("BEADHUB_ALIAS", "test-agent")
	t.Setenv("BEADHUB_HUMAN", "Test Human")
	t.Setenv("BEADHUB_PROJECT", "my-actual-project")

	if err := runInit(); err != nil {
		t.Fatalf("runInit() error: %v", err)
	}

	cfg, _ := config.Load()
	if cfg.ProjectSlug != "my-actual-project" {
		t.Errorf("ProjectSlug = %q, want %q", cfg.ProjectSlug, "my-actual-project")
	}
}

func TestInitCommand_AutoDetectsProjectFromRepo(t *testing.T) {
	setupTempWorkspace(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/init" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var req struct {
			ProjectSlug string `json:"project_slug"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if strings.TrimSpace(req.ProjectSlug) != "" {
			t.Fatalf("expected no project_slug, got %q", req.ProjectSlug)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":           "ok",
			"api_key":          "aw_sk_123456789012345678901234567890123456",
			"project_id":       "existing-project-uuid",
			"project_slug":     "auto-detected-project",
			"repo_id":          "c3d4e5f6-7890-12cd-ef01-345678901234",
			"canonical_origin": "github.com/test/repo",
			"workspace_id":     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
			"alias":            "test-agent",
			"created":          true,
		})
	}))
	defer server.Close()

	t.Setenv("BEADHUB_URL", server.URL)
	t.Setenv("BEADHUB_REPO_ORIGIN", "git@github.com:test/repo.git")
	t.Setenv("BEADHUB_ALIAS", "test-agent")
	t.Setenv("BEADHUB_HUMAN", "Test Human")

	if err := runInit(); err != nil {
		t.Fatalf("runInit() error: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error: %v", err)
	}

	if cfg.ProjectSlug != "auto-detected-project" {
		t.Errorf("ProjectSlug = %q, want %q", cfg.ProjectSlug, "auto-detected-project")
	}
	if cfg.RepoID != "c3d4e5f6-7890-12cd-ef01-345678901234" {
		t.Errorf("RepoID = %q, want %q", cfg.RepoID, "c3d4e5f6-7890-12cd-ef01-345678901234")
	}
	if cfg.CanonicalOrigin != "github.com/test/repo" {
		t.Errorf("CanonicalOrigin = %q, want %q", cfg.CanonicalOrigin, "github.com/test/repo")
	}
}

func TestInitCommand_SendsHostnameAndWorkspacePath(t *testing.T) {
	tmpDir := setupTempWorkspace(t)
	realTmpDir, _ := filepath.EvalSymlinks(tmpDir)

	var receivedHostname, receivedPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/init" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		var req struct {
			Hostname      string `json:"hostname"`
			WorkspacePath string `json:"workspace_path"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		receivedHostname = req.Hostname
		receivedPath = req.WorkspacePath

		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":           "ok",
			"api_key":          "aw_sk_123456789012345678901234567890123456",
			"project_id":       "proj-1",
			"project_slug":     "test-project",
			"repo_id":          "c3d4e5f6-7890-12cd-ef01-345678901234",
			"canonical_origin": "github.com/test/repo",
			"workspace_id":     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
			"alias":            "test-agent",
			"created":          true,
		})
	}))
	defer server.Close()

	t.Setenv("BEADHUB_URL", server.URL)
	t.Setenv("BEADHUB_REPO_ORIGIN", "git@github.com:test/repo.git")
	t.Setenv("BEADHUB_ALIAS", "test-agent")
	t.Setenv("BEADHUB_HUMAN", "Test Human")
	t.Setenv("BEADHUB_PROJECT", "test-project")

	if err := runInit(); err != nil {
		t.Fatalf("runInit() error: %v", err)
	}

	if receivedHostname == "" {
		t.Errorf("hostname should not be empty")
	}
	if receivedPath != realTmpDir {
		t.Errorf("workspace_path = %q, want %q", receivedPath, realTmpDir)
	}
}

func TestInitCommand_FailsOnEmptyCanonicalOriginFromLookup(t *testing.T) {
	setupTempWorkspace(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/init" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":           "ok",
			"api_key":          "aw_sk_123456789012345678901234567890123456",
			"project_id":       "existing-project-uuid",
			"project_slug":     "corrupted-project",
			"repo_id":          "c3d4e5f6-7890-12cd-ef01-345678901234",
			"canonical_origin": "",
			"workspace_id":     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
			"alias":            "test-agent",
			"created":          true,
		})
	}))
	defer server.Close()

	t.Setenv("BEADHUB_URL", server.URL)
	t.Setenv("BEADHUB_REPO_ORIGIN", "git@github.com:test/repo.git")
	t.Setenv("BEADHUB_ALIAS", "test-agent")
	t.Setenv("BEADHUB_HUMAN", "Test Human")

	err := runInit()
	if err == nil {
		t.Fatal("runInit() should error when canonical_origin is empty")
	}
	if !strings.Contains(err.Error(), "canonical_origin") {
		t.Errorf("error should mention canonical_origin, got: %v", err)
	}
}

func TestInitCommand_SanitizesInvalidDirectoryNames(t *testing.T) {
	testCases := []struct {
		dirName       string
		wantSanitized string
	}{
		{"my_project", "my-project"},
		{"my.project", "my-project"},
		{"my project", "my-project"},
		{"my---project", "my-project"},
	}

	for _, tc := range testCases {
		t.Run(tc.dirName, func(t *testing.T) {
			resetInitFlags()

			tmpDir := t.TempDir()
			t.Setenv("AW_CONFIG_PATH", filepath.Join(tmpDir, "aw-config.yaml"))
			workDir := filepath.Join(tmpDir, tc.dirName)
			if err := os.MkdirAll(filepath.Join(workDir, ".beads"), 0755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}

			origDir, _ := os.Getwd()
			t.Cleanup(func() { _ = os.Chdir(origDir) })
			_ = os.Chdir(workDir)

			beads.ResetCache()
			_ = os.WriteFile(beads.DatabasePath(), []byte(""), 0600)

			callCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/init" {
					w.WriteHeader(http.StatusNotFound)
					return
				}

				callCount++
				if callCount == 1 {
					w.WriteHeader(http.StatusUnprocessableEntity)
					_ = json.NewEncoder(w).Encode(map[string]any{"detail": "project_not_found"})
					return
				}

				var req struct {
					ProjectSlug string `json:"project_slug"`
				}
				_ = json.NewDecoder(r.Body).Decode(&req)
				if req.ProjectSlug != tc.wantSanitized {
					t.Fatalf("server received project_slug=%q want %q", req.ProjectSlug, tc.wantSanitized)
				}

				_ = json.NewEncoder(w).Encode(map[string]any{
					"status":           "ok",
					"api_key":          "aw_sk_123456789012345678901234567890123456",
					"project_id":       "proj-1",
					"project_slug":     req.ProjectSlug,
					"repo_id":          "c3d4e5f6-7890-12cd-ef01-345678901234",
					"canonical_origin": "github.com/test/repo",
					"workspace_id":     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
					"alias":            "test-agent",
					"created":          true,
				})
			}))
			defer server.Close()

			t.Setenv("BEADHUB_URL", server.URL)
			t.Setenv("BEADHUB_REPO_ORIGIN", "git@github.com:test/repo.git")
			t.Setenv("BEADHUB_ALIAS", "test-agent")
			t.Setenv("BEADHUB_HUMAN", "Test Human")

			if err := runInit(); err != nil {
				t.Fatalf("runInit() error for %q: %v", tc.dirName, err)
			}

			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("config.Load() error: %v", err)
			}
			if cfg.ProjectSlug != tc.wantSanitized {
				t.Errorf("ProjectSlug = %q, want %q", cfg.ProjectSlug, tc.wantSanitized)
			}
		})
	}
}

func TestInitCommand_UpdateUsesRegisterWorkspace(t *testing.T) {
	tmpDir := setupTempWorkspace(t)
	t.Cleanup(resetInitFlags)

	// Seed a .beadhub file so :init takes the --update path.
	cfg := &config.Config{
		WorkspaceID:     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
		BeadhubURL:      "http://example.invalid",
		ProjectSlug:     "test-project",
		RepoID:          "c3d4e5f6-7890-12cd-ef01-345678901234",
		RepoOrigin:      "git@github.com:test/repo.git",
		CanonicalOrigin: "github.com/test/repo",
		Alias:           "test-agent",
		HumanName:       "Test Human",
		Role:            "agent",
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	var gotAuth string
	var gotRepoOrigin string
	var gotRole string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/workspaces/register" {
			http.NotFound(w, r)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		var req struct {
			RepoOrigin string `json:"repo_origin"`
			Role       string `json:"role"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotRepoOrigin = req.RepoOrigin
		gotRole = req.Role

		_ = json.NewEncoder(w).Encode(map[string]any{
			"workspace_id":     cfg.WorkspaceID,
			"project_id":       "proj-1",
			"project_slug":     cfg.ProjectSlug,
			"repo_id":          cfg.RepoID,
			"canonical_origin": cfg.CanonicalOrigin,
			"alias":            cfg.Alias,
			"human_name":       cfg.HumanName,
			"created":          false,
		})
	}))
	defer server.Close()

	// Update beadhub_url in .beadhub to point at the mock server.
	cfg.BeadhubURL = server.URL
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// Seed account selection via ~/.config/aw/config.yaml + .aw/context.
	serverName, err := awconfig.DeriveServerNameFromURL(server.URL)
	if err != nil {
		t.Fatalf("derive server name: %v", err)
	}
	accountName := deriveAccountName(serverName, cfg.ProjectSlug, cfg.Alias)
	if err := awconfig.UpdateGlobalAt(os.Getenv("AW_CONFIG_PATH"), func(gc *awconfig.GlobalConfig) error {
		if gc.Servers == nil {
			gc.Servers = map[string]awconfig.Server{}
		}
		if gc.Accounts == nil {
			gc.Accounts = map[string]awconfig.Account{}
		}
		gc.Servers[serverName] = awconfig.Server{URL: server.URL}
		gc.Accounts[accountName] = awconfig.Account{
			Server:         serverName,
			APIKey:         "aw_sk_from_account",
			DefaultProject: cfg.ProjectSlug,
			AgentID:        cfg.WorkspaceID,
			AgentAlias:     cfg.Alias,
		}
		gc.DefaultAccount = accountName
		return nil
	}); err != nil {
		t.Fatalf("seed aw global config: %v", err)
	}
	if err := awconfig.SaveWorktreeContextTo(filepath.Join(tmpDir, awconfig.DefaultWorktreeContextRelativePath()), &awconfig.WorktreeContext{
		DefaultAccount: accountName,
		ServerAccounts: map[string]string{serverName: accountName},
	}); err != nil {
		t.Fatalf("seed .aw/context: %v", err)
	}

	t.Setenv("BEADHUB_REPO_ORIGIN", "git@github.com:test/repo.git")
	initUpdate = true
	initRole = "reviewer"

	if err := runInit(); err != nil {
		t.Fatalf("runInit --update: %v", err)
	}

	if gotAuth != "Bearer aw_sk_from_account" {
		t.Fatalf("Authorization=%q want %q", gotAuth, "Bearer aw_sk_from_account")
	}
	if gotRepoOrigin != "git@github.com:test/repo.git" {
		t.Fatalf("repo_origin=%q want %q", gotRepoOrigin, "git@github.com:test/repo.git")
	}
	if gotRole != "reviewer" {
		t.Fatalf("role=%q want %q", gotRole, "reviewer")
	}

	updated, err := config.Load()
	if err != nil {
		t.Fatalf("load updated config: %v", err)
	}
	if updated.Role != "reviewer" {
		t.Fatalf("role in .beadhub=%q want %q", updated.Role, "reviewer")
	}
}

func containsLine(content, line string) bool {
	lines := splitLines(content)
	for _, l := range lines {
		if l == line {
			return true
		}
	}
	return false
}

func countLines(content, line string) int {
	lines := splitLines(content)
	count := 0
	for _, l := range lines {
		if l == line {
			count++
		}
	}
	return count
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
