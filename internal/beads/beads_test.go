package beads

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beadhub/bdh/internal/config"
)

func TestGetBeadsDir_NonGitDirectory(t *testing.T) {
	// Create a temp directory (not a git repo)
	tmpDir, err := os.MkdirTemp("", "beads-test-nongit-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create .beads directory
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Change to the temp directory
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Reset cache before test
	ResetCache()

	// GetBeadsDir should return .beads in current directory
	got := GetBeadsDir()
	want := ".beads"
	if got != want {
		t.Errorf("GetBeadsDir() = %q, want %q", got, want)
	}
}

func TestGetBeadsDir_RegularGitRepo(t *testing.T) {
	// Create a temp directory with git repo
	tmpDir, err := os.MkdirTemp("", "beads-test-git-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Resolve symlinks for consistent comparison (macOS /var -> /private/var)
	tmpDir, err = filepath.EvalSymlinks(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init failed: %v", err)
	}

	// Create .beads directory
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Change to the git repo
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Reset cache before test
	ResetCache()

	// GetBeadsDir should return the .beads directory
	got := GetBeadsDir()
	want := filepath.Join(tmpDir, ".beads")
	if got != want {
		t.Errorf("GetBeadsDir() = %q, want %q", got, want)
	}
}

func TestGetBeadsDir_GitWorktree(t *testing.T) {
	// Create main repo
	mainDir, err := os.MkdirTemp("", "beads-test-main-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(mainDir)

	// Resolve symlinks for consistent comparison (macOS /var -> /private/var)
	mainDir, err = filepath.EvalSymlinks(mainDir)
	if err != nil {
		t.Fatal(err)
	}

	// Initialize main git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = mainDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init failed: %v", err)
	}

	// Configure git user for commits
	cmd = exec.Command("git", "config", "user.email", "test@test.com")
	cmd.Dir = mainDir
	cmd.Run()
	cmd = exec.Command("git", "config", "user.name", "Test")
	cmd.Dir = mainDir
	cmd.Run()

	// Create initial commit (required for worktrees)
	dummyFile := filepath.Join(mainDir, "dummy.txt")
	if err := os.WriteFile(dummyFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "add", "dummy.txt")
	cmd.Dir = mainDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git add failed: %v", err)
	}
	cmd = exec.Command("git", "commit", "-m", "initial")
	cmd.Dir = mainDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git commit failed: %v", err)
	}

	// Create .beads directory in main repo
	beadsDir := filepath.Join(mainDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create worktree
	worktreeDir, err := os.MkdirTemp("", "beads-test-worktree-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(worktreeDir)
	// Remove the dir because git worktree add wants to create it
	os.RemoveAll(worktreeDir)

	cmd = exec.Command("git", "worktree", "add", worktreeDir, "-b", "test-branch")
	cmd.Dir = mainDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git worktree add failed: %v", err)
	}
	// Clean up worktree on exit
	defer func() {
		cmd := exec.Command("git", "worktree", "remove", worktreeDir, "--force")
		cmd.Dir = mainDir
		cmd.Run()
	}()

	// Change to the worktree
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldDir)
	if err := os.Chdir(worktreeDir); err != nil {
		t.Fatal(err)
	}

	// Reset cache before test
	ResetCache()

	// GetBeadsDir should return .beads from MAIN repo, not worktree
	got := GetBeadsDir()
	want := beadsDir
	if got != want {
		t.Errorf("GetBeadsDir() = %q, want %q (main repo's .beads)", got, want)
	}
}

func TestGetBeadsDir_GitWorktree_NoBeadsInMain(t *testing.T) {
	// Create main repo WITHOUT .beads
	mainDir, err := os.MkdirTemp("", "beads-test-main-nobeads-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(mainDir)

	// Initialize main git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = mainDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init failed: %v", err)
	}

	// Configure git user for commits
	cmd = exec.Command("git", "config", "user.email", "test@test.com")
	cmd.Dir = mainDir
	cmd.Run()
	cmd = exec.Command("git", "config", "user.name", "Test")
	cmd.Dir = mainDir
	cmd.Run()

	// Create initial commit (required for worktrees)
	dummyFile := filepath.Join(mainDir, "dummy.txt")
	if err := os.WriteFile(dummyFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "add", "dummy.txt")
	cmd.Dir = mainDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git add failed: %v", err)
	}
	cmd = exec.Command("git", "commit", "-m", "initial")
	cmd.Dir = mainDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git commit failed: %v", err)
	}

	// Create worktree
	worktreeDir, err := os.MkdirTemp("", "beads-test-worktree-nobeads-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(worktreeDir)
	os.RemoveAll(worktreeDir)

	cmd = exec.Command("git", "worktree", "add", worktreeDir, "-b", "test-branch-nobeads")
	cmd.Dir = mainDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git worktree add failed: %v", err)
	}
	defer func() {
		cmd := exec.Command("git", "worktree", "remove", worktreeDir, "--force")
		cmd.Dir = mainDir
		cmd.Run()
	}()

	// Change to the worktree
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldDir)
	if err := os.Chdir(worktreeDir); err != nil {
		t.Fatal(err)
	}

	// Reset cache before test
	ResetCache()

	// GetBeadsDir should fall back to .beads in current directory
	// (main repo has no .beads, so we use local)
	got := GetBeadsDir()
	want := ".beads"
	if got != want {
		t.Errorf("GetBeadsDir() = %q, want %q (fallback to local)", got, want)
	}
}

func TestIssuesJSONLPath(t *testing.T) {
	// Create a temp directory (not a git repo)
	tmpDir, err := os.MkdirTemp("", "beads-test-jsonl-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create .beads directory
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Change to the temp directory
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Reset cache before test
	ResetCache()

	// IssuesJSONLPath should return correct path
	got := IssuesJSONLPath()
	want := filepath.Join(".beads", "issues.jsonl")
	if got != want {
		t.Errorf("IssuesJSONLPath() = %q, want %q", got, want)
	}
}

func TestDatabasePath(t *testing.T) {
	// Create a temp directory (not a git repo)
	tmpDir, err := os.MkdirTemp("", "beads-test-db-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create .beads directory
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Change to the temp directory
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Reset cache before test
	ResetCache()

	// DatabasePath should return correct path
	got := DatabasePath()
	want := filepath.Join(".beads", "beads.db")
	if got != want {
		t.Errorf("DatabasePath() = %q, want %q", got, want)
	}
}

func TestGetBeadsDir_SubdirectoryInGitRepo(t *testing.T) {
	// Create a temp directory with git repo
	tmpDir, err := os.MkdirTemp("", "beads-test-subdir-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Resolve symlinks for consistent comparison (macOS /var -> /private/var)
	tmpDir, err = filepath.EvalSymlinks(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init failed: %v", err)
	}

	// Create .beads directory at repo root
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a subdirectory: tmpDir/src/subdir
	subDir := filepath.Join(tmpDir, "src", "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Change to the subdirectory
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldDir)
	if err := os.Chdir(subDir); err != nil {
		t.Fatal(err)
	}

	// Reset cache before test
	ResetCache()

	// GetBeadsDir should still find .beads at repo root, not in subdir
	got := GetBeadsDir()
	want := beadsDir
	if got != want {
		t.Errorf("GetBeadsDir() from subdirectory = %q, want %q (repo root's .beads)", got, want)
	}
}

func TestGetWarning_NonGitDirectory(t *testing.T) {
	// Create a temp directory (not a git repo)
	tmpDir, err := os.MkdirTemp("", "beads-test-warning-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Change to the temp directory
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Reset cache before test
	ResetCache()

	// GetWarning should return a warning about git detection failing
	warning := GetWarning()
	if warning == "" {
		t.Error("GetWarning() = empty, expected warning about git detection failure")
	}
	if !strings.Contains(warning, "git detection failed") {
		t.Errorf("GetWarning() = %q, expected to contain 'git detection failed'", warning)
	}
}

func TestSyncStatePath(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".beadhub")

	config.SetPath(configPath)
	defer config.SetPath("")

	got := SyncStatePath()
	want := filepath.Join(tmpDir, ".beadhub-cache", "sync-state.json")
	if got != want {
		t.Errorf("SyncStatePath() = %q, want %q", got, want)
	}
}
