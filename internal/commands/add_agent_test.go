package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/awebai/aw/awconfig"
	"github.com/beadhub/bdh/internal/beads"
	"github.com/beadhub/bdh/internal/config"
)

func TestValidateNamePrefix(t *testing.T) {
	tests := []struct {
		name      string
		prefix    string
		wantValid bool
	}{
		{"simple name", "alice", true},
		{"name with suffix", "alice-01", true},
		{"uppercase rejected", "Alice", false},
		{"spaces rejected", "alice bob", false},
		{"numbers rejected", "alice123", false},
		{"hyphens in middle rejected", "alice-bob", false},
		{"empty rejected", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := validateNamePrefix(tc.prefix)
			if got != tc.wantValid {
				t.Errorf("validateNamePrefix(%q) = %v, want %v", tc.prefix, got, tc.wantValid)
			}
		})
	}
}

func TestDeriveWorktreePath(t *testing.T) {
	tests := []struct {
		name       string
		mainRepo   string
		branchName string
		want       string
		wantErr    bool
	}{
		{
			name:       "simple path",
			mainRepo:   "/home/user/projects/myrepo",
			branchName: "alice",
			want:       "/home/user/projects/myrepo-alice",
		},
		{
			name:       "path with suffix",
			mainRepo:   "/home/user/projects/myrepo-main",
			branchName: "bob-01",
			want:       "/home/user/projects/myrepo-main-bob-01",
		},
		{
			name:       "path traversal rejected",
			mainRepo:   "/home/user/projects/myrepo",
			branchName: "../../../etc",
			wantErr:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := deriveWorktreePath(tc.mainRepo, tc.branchName)
			if tc.wantErr {
				if err == nil {
					t.Errorf("deriveWorktreePath(%q, %q) expected error, got nil",
						tc.mainRepo, tc.branchName)
				}
				return
			}
			if err != nil {
				t.Errorf("deriveWorktreePath(%q, %q) unexpected error: %v",
					tc.mainRepo, tc.branchName, err)
				return
			}
			if got != tc.want {
				t.Errorf("deriveWorktreePath(%q, %q) = %q, want %q",
					tc.mainRepo, tc.branchName, got, tc.want)
			}
		})
	}
}

func TestAddAgent_Integration(t *testing.T) {
	// Create a temporary directory structure for the git repo
	tmpDir := t.TempDir()
	t.Setenv("AW_CONFIG_PATH", filepath.Join(tmpDir, "aw-config.yaml"))
	mainRepo := filepath.Join(tmpDir, "myrepo")
	if err := os.MkdirAll(mainRepo, 0755); err != nil {
		t.Fatalf("mkdir main repo: %v", err)
	}

	// Initialize git repo
	if err := exec.Command("git", "-C", mainRepo, "init").Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}

	// Configure git user for commits
	_ = exec.Command("git", "-C", mainRepo, "config", "user.email", "test@test.com").Run()
	_ = exec.Command("git", "-C", mainRepo, "config", "user.name", "Test User").Run()

	// Create initial commit (required for worktree)
	if err := os.WriteFile(filepath.Join(mainRepo, "README.md"), []byte("# Test"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	if err := exec.Command("git", "-C", mainRepo, "add", ".").Run(); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if err := exec.Command("git", "-C", mainRepo, "commit", "-m", "Initial commit").Run(); err != nil {
		t.Fatalf("git commit: %v", err)
	}

	// Add a fake remote origin
	if err := exec.Command("git", "-C", mainRepo, "remote", "add", "origin", "git@github.com:test/repo.git").Run(); err != nil {
		t.Fatalf("git remote add: %v", err)
	}

	// Create .beads directory and dummy DB
	if err := os.MkdirAll(filepath.Join(mainRepo, ".beads"), 0755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	beads.ResetCache()
	if err := os.WriteFile(filepath.Join(mainRepo, ".beads", "beads.db"), []byte(""), 0600); err != nil {
		t.Fatalf("write beads.db: %v", err)
	}

	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/workspaces/suggest-name-prefix":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name_prefix":  "testname",
				"project_slug": "test-project",
			})
		case "/v1/init":
			var initReq struct {
				Alias string `json:"alias"`
			}
			_ = json.NewDecoder(r.Body).Decode(&initReq)
			alias := initReq.Alias
			if strings.TrimSpace(alias) == "" {
				alias = "testname-coord"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":           "ok",
				"api_key":          "aw_sk_123456789012345678901234567890123456",
				"project_id":       "test-project-uuid",
				"project_slug":     "test-project",
				"repo_id":          "c3d4e5f6-7890-12cd-ef01-345678901234",
				"canonical_origin": "github.com/test/repo",
				"workspace_id":     "a1b2c3d4-5678-90ab-cdef-1234567890ab",
				"alias":            alias,
				"created":          true,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Set up environment
	t.Setenv("BEADHUB_URL", server.URL)
	t.Setenv("BEADHUB_REPO_ORIGIN", "git@github.com:test/repo.git")

	// Change to main repo directory
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	_ = os.Chdir(mainRepo)

	// Create initial .beadhub config so the command can load it
	cfg := &config.Config{
		WorkspaceID:     "initial-workspace-id",
		BeadhubURL:      server.URL,
		ProjectSlug:     "test-project",
		RepoID:          "initial-repo-id",
		RepoOrigin:      "git@github.com:test/repo.git",
		CanonicalOrigin: "github.com/test/repo",
		Alias:           "initial-agent",
		HumanName:       "Test Human",
		Role:            "agent",
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// Seed the current worktree with a beadhub account selection via ~/.config/aw/config.yaml + .aw/context.
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
			APIKey:         "aw_sk_seed_key_123456789012345678901234567890",
			DefaultProject: cfg.ProjectSlug,
			AgentID:        cfg.WorkspaceID,
			AgentAlias:     cfg.Alias,
		}
		gc.DefaultAccount = accountName
		return nil
	}); err != nil {
		t.Fatalf("seed aw global config: %v", err)
	}
	if err := awconfig.SaveWorktreeContextTo(filepath.Join(mainRepo, awconfig.DefaultWorktreeContextRelativePath()), &awconfig.WorktreeContext{
		DefaultAccount: accountName,
		ServerAccounts: map[string]string{serverName: accountName},
	}); err != nil {
		t.Fatalf("seed .aw/context: %v", err)
	}

	// Reset flags
	addWorktreeAlias = ""
	resetAddWorktreeInitFlags()

	// Run the command
	err = runAddWorktree(addWorktreeCmd, []string{"coord"})
	if err != nil {
		t.Fatalf("runAddWorktree() error: %v", err)
	}

	// Verify worktree was created
	worktreePath := filepath.Join(tmpDir, "myrepo-testname-coord")
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		t.Error("worktree directory was not created")
	}

	// Verify .beadhub was created in worktree
	if _, err := os.Stat(filepath.Join(worktreePath, ".beadhub")); os.IsNotExist(err) {
		t.Error(".beadhub was not created in worktree")
	}

	// Verify .aw/context was created in worktree
	if _, err := os.Stat(filepath.Join(worktreePath, ".aw", "context")); os.IsNotExist(err) {
		t.Error(".aw/context was not created in worktree")
	}

	// Verify git branch was created
	branchCmd := exec.Command("git", "-C", mainRepo, "branch", "--list", "testname-coord")
	output, _ := branchCmd.Output()
	if len(output) == 0 {
		t.Error("git branch 'testname-coord' was not created")
	}

	// Cleanup: remove worktree
	_ = exec.Command("git", "-C", mainRepo, "worktree", "remove", worktreePath, "--force").Run()
}

func TestAddAgent_DirectoryAlreadyExists(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AW_CONFIG_PATH", filepath.Join(tmpDir, "aw-config.yaml"))
	mainRepo := filepath.Join(tmpDir, "myrepo")
	if err := os.MkdirAll(mainRepo, 0755); err != nil {
		t.Fatalf("mkdir main repo: %v", err)
	}

	// Initialize git repo
	if err := exec.Command("git", "-C", mainRepo, "init").Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	_ = exec.Command("git", "-C", mainRepo, "config", "user.email", "test@test.com").Run()
	_ = exec.Command("git", "-C", mainRepo, "config", "user.name", "Test User").Run()

	// Create initial commit
	if err := os.WriteFile(filepath.Join(mainRepo, "README.md"), []byte("# Test"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	_ = exec.Command("git", "-C", mainRepo, "add", ".").Run()
	_ = exec.Command("git", "-C", mainRepo, "commit", "-m", "Initial commit").Run()
	_ = exec.Command("git", "-C", mainRepo, "remote", "add", "origin", "git@github.com:test/repo.git").Run()

	// Create .beads directory
	if err := os.MkdirAll(filepath.Join(mainRepo, ".beads"), 0755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	beads.ResetCache()
	if err := os.WriteFile(filepath.Join(mainRepo, ".beads", "beads.db"), []byte(""), 0600); err != nil {
		t.Fatalf("write beads.db: %v", err)
	}

	// Create the directory that would conflict with the worktree
	conflictPath := filepath.Join(tmpDir, "myrepo-alice-coord")
	if err := os.MkdirAll(conflictPath, 0755); err != nil {
		t.Fatalf("mkdir conflict path: %v", err)
	}

	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/workspaces/suggest-name-prefix" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name_prefix":  "alice",
				"project_slug": "test-project",
			})
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	t.Setenv("BEADHUB_URL", server.URL)
	t.Setenv("BEADHUB_REPO_ORIGIN", "git@github.com:test/repo.git")

	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	_ = os.Chdir(mainRepo)

	// Create initial config
	cfg := &config.Config{
		WorkspaceID:     "initial-workspace-id",
		BeadhubURL:      server.URL,
		ProjectSlug:     "test-project",
		RepoID:          "initial-repo-id",
		RepoOrigin:      "git@github.com:test/repo.git",
		CanonicalOrigin: "github.com/test/repo",
		Alias:           "initial-agent",
		HumanName:       "Test Human",
		Role:            "agent",
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// Seed the current worktree with a beadhub account selection via ~/.config/aw/config.yaml + .aw/context.
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
			APIKey:         "aw_sk_seed_key_123456789012345678901234567890",
			DefaultProject: cfg.ProjectSlug,
			AgentID:        cfg.WorkspaceID,
			AgentAlias:     cfg.Alias,
		}
		gc.DefaultAccount = accountName
		return nil
	}); err != nil {
		t.Fatalf("seed aw global config: %v", err)
	}
	if err := awconfig.SaveWorktreeContextTo(filepath.Join(mainRepo, awconfig.DefaultWorktreeContextRelativePath()), &awconfig.WorktreeContext{
		DefaultAccount: accountName,
		ServerAccounts: map[string]string{serverName: accountName},
	}); err != nil {
		t.Fatalf("seed .aw/context: %v", err)
	}

	addWorktreeAlias = ""
	resetAddWorktreeInitFlags()

	// Run the command - should fail due to existing directory
	err = runAddWorktree(addWorktreeCmd, []string{"coord"})
	if err == nil {
		t.Fatal("runAddWorktree() should error when directory exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists', got: %v", err)
	}
}

func TestAddWorktree_InvalidExplicitAliasRejected(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AW_CONFIG_PATH", filepath.Join(tmpDir, "aw-config.yaml"))
	mainRepo := filepath.Join(tmpDir, "myrepo")
	if err := os.MkdirAll(mainRepo, 0755); err != nil {
		t.Fatalf("mkdir main repo: %v", err)
	}

	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	_ = os.Chdir(mainRepo)

	if err := exec.Command("git", "-C", mainRepo, "init").Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	_ = exec.Command("git", "-C", mainRepo, "config", "user.email", "test@test.com").Run()
	_ = exec.Command("git", "-C", mainRepo, "config", "user.name", "Test User").Run()

	if err := os.WriteFile(filepath.Join(mainRepo, "README.md"), []byte("# Test"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	_ = exec.Command("git", "-C", mainRepo, "add", ".").Run()
	_ = exec.Command("git", "-C", mainRepo, "commit", "-m", "Initial commit").Run()
	_ = exec.Command("git", "-C", mainRepo, "remote", "add", "origin", "git@github.com:test/repo.git").Run()

	// Create .beads directory and dummy DB
	if err := os.MkdirAll(filepath.Join(mainRepo, ".beads"), 0755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	beads.ResetCache()
	if err := os.WriteFile(filepath.Join(mainRepo, ".beads", "beads.db"), []byte(""), 0600); err != nil {
		t.Fatalf("write beads.db: %v", err)
	}

	cfg := &config.Config{
		WorkspaceID:     "initial-workspace-id",
		BeadhubURL:      "http://localhost:8000",
		ProjectSlug:     "test-project",
		RepoID:          "initial-repo-id",
		RepoOrigin:      "git@github.com:test/repo.git",
		CanonicalOrigin: "github.com/test/repo",
		Alias:           "initial-agent",
		HumanName:       "Test Human",
		Role:            "agent",
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	t.Setenv("BEADHUB_REPO_ORIGIN", "git@github.com:test/repo.git")

	addWorktreeAlias = "_invalid"
	resetAddWorktreeInitFlags()

	err := runAddWorktree(addWorktreeCmd, []string{"coord"})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "invalid alias") {
		t.Fatalf("expected invalid alias error, got: %v", err)
	}
}
