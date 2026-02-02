package commands

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/beadhub/bdh/internal/client"
	"github.com/beadhub/bdh/internal/config"
)

func validateRepoOriginMatchesCurrent(cfg *config.Config) error {
	// Allow explicit skip for legitimate testing environments
	if os.Getenv("BEADHUB_SKIP_REPO_CHECK") == "1" {
		return nil
	}

	origin := strings.TrimSpace(os.Getenv("BEADHUB_REPO_ORIGIN"))
	if origin == "" {
		var err error
		origin, err = getGitOrigin()
		if err != nil {
			// Only skip check for specific safe errors - fail closed otherwise
			if isGitNotFoundOrNotRepo(err) {
				// git not installed or not in a git repo - legitimate cases to skip
				return nil
			}
			// Unexpected error - fail closed for security
			return fmt.Errorf("repo validation failed: %w (set BEADHUB_SKIP_REPO_CHECK=1 to bypass)", err)
		}
	}

	currentCanonical := canonicalizeOriginURL(origin)
	if currentCanonical == "" || cfg.CanonicalOrigin == "" {
		return nil
	}

	if currentCanonical != cfg.CanonicalOrigin {
		return fmt.Errorf(
			"workspace repo mismatch: this workspace is bound to %q but git origin resolves to %q; re-run `bdh :init` in this repo",
			cfg.CanonicalOrigin,
			currentCanonical,
		)
	}

	return nil
}

// isGitNotFoundOrNotRepo returns true for errors that indicate git is not available
// or we're not in a git repository - legitimate cases to skip repo validation.
func isGitNotFoundOrNotRepo(err error) bool {
	if err == nil {
		return false
	}

	// git binary not found in PATH
	if errors.Is(err, exec.ErrNotFound) {
		return true
	}

	// git command failed - check exit error
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		stderr := string(exitErr.Stderr)
		// "not a git repository" or "no remote named 'origin'"
		if strings.Contains(stderr, "not a git repository") ||
			strings.Contains(stderr, "No such remote") ||
			strings.Contains(stderr, "fatal: No remote configured") {
			return true
		}
	}

	return false
}

func canonicalizeOriginURL(origin string) string {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return ""
	}

	if strings.HasPrefix(origin, "git@") {
		parts := strings.SplitN(origin, ":", 2)
		if len(parts) != 2 {
			return ""
		}
		host := strings.TrimPrefix(parts[0], "git@")
		path := strings.Trim(parts[1], "/")
		path = strings.TrimSuffix(path, ".git")
		if host == "" || path == "" {
			return ""
		}
		return host + "/" + path
	}

	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return ""
	}
	host := strings.ToLower(u.Host)
	path := strings.Trim(u.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	if host == "" || path == "" {
		return ""
	}
	return host + "/" + path
}

func currentRepoOriginBestEffort(cfg *config.Config) string {
	origin := strings.TrimSpace(os.Getenv("BEADHUB_REPO_ORIGIN"))
	if origin != "" {
		return origin
	}
	origin, err := getGitOrigin()
	if err == nil && strings.TrimSpace(origin) != "" {
		return strings.TrimSpace(origin)
	}
	return cfg.RepoOrigin
}

func currentGitBranch(repoRoot string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()

	args := []string{"rev-parse", "--abbrev-ref", "HEAD"}
	if repoRoot != "" {
		args = append([]string{"-C", repoRoot}, args...)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	branch := strings.TrimSpace(string(out))
	if branch == "" || branch == "HEAD" {
		return ""
	}
	return branch
}

func currentRepoRoot() string {
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()

	root, err := gitRepoRoot(ctx)
	if err != nil {
		return ""
	}
	return root
}

func refreshPresenceHeartbeat(cfg *config.Config) {
	repoRoot := currentRepoRoot()
	branch := currentGitBranch(repoRoot)
	repoOrigin := currentRepoOriginBestEffort(cfg)

	hostname, _ := os.Hostname()
	workspacePath := repoRoot
	if workspacePath == "" {
		if cwd, err := os.Getwd(); err == nil {
			workspacePath = cwd
		}
	}

	c := newBeadHubClient(cfg.BeadhubURL)
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	_, _ = c.RefreshPresence(ctx, &client.RefreshPresenceRequest{
		WorkspaceID:     cfg.WorkspaceID,
		Alias:           cfg.Alias,
		HumanName:       cfg.HumanName,
		ProjectSlug:     cfg.ProjectSlug,
		RepoID:          cfg.RepoID,
		RepoOrigin:      repoOrigin,
		CanonicalOrigin: cfg.CanonicalOrigin,
		Hostname:        hostname,
		WorkspacePath:   workspacePath,
		Repo:            cfg.CanonicalOrigin,
		Branch:          branch,
		Program:         "claude-code",
		Role:            cfg.Role,
	})
}
