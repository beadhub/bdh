// Package beads provides worktree-aware beads directory discovery.
//
// This package mirrors the approach in bd's internal/git/gitdir.go and
// internal/beads/beads.go to ensure bdh finds the same .beads directory
// that bd uses, even when running in a git worktree.
package beads

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/beadhub/bdh/internal/config"
)

// beadsContext holds cached beads directory information.
// All fields are populated once and cached for efficiency.
type beadsContext struct {
	beadsDir string // The resolved .beads directory path
	warning  string // Non-fatal warning (e.g., git detection failed)
}

var (
	beadsCtxMu   sync.Mutex
	beadsCtxOnce sync.Once
	beadsCtx     beadsContext
)

// initBeadsContext populates the beadsContext by finding the correct .beads directory.
// This is called once per process via sync.Once.
func initBeadsContext() {
	beadsCtx.beadsDir, beadsCtx.warning = findBeadsDir()
}

// findBeadsDir finds the correct .beads directory.
// Search order:
// 1. If in a git worktree, check main repo's .beads
// 2. If in a git repo (not worktree), check repo root's .beads
// 3. Fall back to .beads in current working directory (relative path)
//
// Returns the beads directory and an optional warning if git detection failed.
func findBeadsDir() (string, string) {
	// Try to get main repo root (handles worktrees)
	mainRepoRoot, err := getMainRepoRoot()
	if err != nil {
		// Git detection failed - fall back with a warning
		return ".beads", fmt.Sprintf("git detection failed: %v (using fallback .beads)", err)
	}

	if mainRepoRoot != "" {
		beadsDir := filepath.Join(mainRepoRoot, ".beads")
		// Resolve symlinks for consistent paths
		if resolved, resolveErr := filepath.EvalSymlinks(beadsDir); resolveErr == nil {
			beadsDir = resolved
		}
		if info, statErr := os.Stat(beadsDir); statErr == nil && info.IsDir() {
			return beadsDir, ""
		}
	}

	// Fall back to .beads in current working directory (relative path).
	// This will be resolved relative to CWD when files are accessed.
	return ".beads", ""
}

// getMainRepoRoot returns the main repository root directory.
// When in a worktree, this returns the main repository root (not the worktree).
// Uses git rev-parse --git-common-dir to find the shared .git directory.
func getMainRepoRoot() (string, error) {
	// Get git-common-dir which points to the main repo's .git even in worktrees
	cmd := exec.Command("git", "rev-parse", "--git-common-dir")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	commonDir := strings.TrimSpace(string(output))
	if commonDir == "" {
		return "", nil
	}

	// Convert to absolute path if relative
	if !filepath.IsAbs(commonDir) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		commonDir = filepath.Join(cwd, commonDir)
	}

	// Clean up the path and resolve symlinks for consistent comparison
	commonDir = filepath.Clean(commonDir)
	if resolved, err := filepath.EvalSymlinks(commonDir); err == nil {
		commonDir = resolved
	}

	// The main repo root is the parent directory of the git directory.
	// For regular repos: .git is a directory, parent is repo root
	// For worktrees: commonDir points to main repo's .git, parent is main repo root
	return filepath.Dir(commonDir), nil
}

// GetBeadsDir returns the correct .beads directory path.
// This is worktree-aware: when running in a git worktree, it returns
// the .beads directory from the main repository.
// The result is cached after the first call.
func GetBeadsDir() string {
	beadsCtxMu.Lock()
	defer beadsCtxMu.Unlock()

	beadsCtxOnce.Do(initBeadsContext)
	return beadsCtx.beadsDir
}

// GetWarning returns any warning from the beads directory detection.
// This is useful for debugging when git detection fails.
// Returns an empty string if there was no warning.
func GetWarning() string {
	beadsCtxMu.Lock()
	defer beadsCtxMu.Unlock()

	beadsCtxOnce.Do(initBeadsContext)
	return beadsCtx.warning
}

// IssuesJSONLPath returns the path to issues.jsonl in the beads directory.
func IssuesJSONLPath() string {
	return filepath.Join(GetBeadsDir(), "issues.jsonl")
}

// DatabasePath returns the path to beads.db in the beads directory.
func DatabasePath() string {
	return filepath.Join(GetBeadsDir(), "beads.db")
}

// SyncStatePath returns the path to sync-state.json in the BeadHub cache directory.
func SyncStatePath() string {
	workspaceRoot, err := config.WorkspaceRoot()
	if err != nil {
		return filepath.Join(".beadhub-cache", "sync-state.json")
	}
	return filepath.Join(workspaceRoot, ".beadhub-cache", "sync-state.json")
}

// ResetCache resets the cached beads directory. This is intended for use
// by tests that need to change directory between subtests.
// In production, the cache is safe because the working directory
// doesn't change during a single command execution.
//
// This function is thread-safe.
func ResetCache() {
	beadsCtxMu.Lock()
	defer beadsCtxMu.Unlock()

	beadsCtxOnce = sync.Once{}
	beadsCtx = beadsContext{}
}
