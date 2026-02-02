package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	aweb "github.com/awebai/aw"
	"github.com/beadhub/bdh/internal/config"
)

const autoReserveReason = "auto-reserve"

type ReservationConflict struct {
	ResourceKey       string `json:"resource_key"`
	HeldBy            string `json:"held_by"`
	RetryAfterSeconds int    `json:"retry_after_seconds"`
	ExpiresAt         string `json:"expires_at,omitempty"`
}

type AutoReserveResult struct {
	Acquired  []string
	Renewed   []string
	Released  []string
	Conflicts []ReservationConflict
	Warning   string
}

type gitStatusEntry struct {
	X        byte
	Y        byte
	Path     string
	OrigPath string // rename/copy source path (if any)
}

func autoReserve(ctx context.Context, cfg *config.Config, c *aweb.Client) *AutoReserveResult {
	if !cfg.AutoReserveEnabled() {
		return nil
	}

	result := &AutoReserveResult{}

	gitTimeout := 5 * time.Second
	ctxGit, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()

	repoRoot, err := gitRepoRoot(ctxGit)
	if err != nil {
		result.Warning = fmt.Sprintf("Auto-reserve: git repo not detected (%v)", err)
		return result
	}

	entries, err := gitStatusPorcelainV1Z(ctxGit, repoRoot, cfg.ReserveUntrackedEnabled())
	if err != nil {
		result.Warning = fmt.Sprintf("Auto-reserve: git status failed (%v)", err)
		return result
	}

	// desiredLockPaths may return empty, but we still need to release
	// previously auto-managed locks below.
	desired := desiredLockPaths(entries, cfg.ReserveUntrackedEnabled())

	listCtx, listCancel := context.WithTimeout(ctx, apiTimeout)
	defer listCancel()

	allLocksResp, err := c.ReservationList(listCtx, "")
	if err != nil {
		result.Warning = fmt.Sprintf("Auto-reserve: unable to list current locks (%v)", err)
		return result
	}

	heldAny := make(map[string]struct{}, len(allLocksResp.Reservations))
	heldAuto := make(map[string]struct{}, len(allLocksResp.Reservations))
	for _, lock := range allLocksResp.Reservations {
		if lock.HolderAlias != cfg.Alias {
			continue
		}
		if lock.ResourceKey == "" {
			continue
		}
		heldAny[lock.ResourceKey] = struct{}{}
		if reason, ok := lock.Metadata["reason"].(string); ok && reason == autoReserveReason {
			heldAuto[lock.ResourceKey] = struct{}{}
		}
	}

	var toAcquire []string
	var toRenew []string
	for path := range desired {
		if _, ok := heldAny[path]; ok {
			if _, isAuto := heldAuto[path]; isAuto {
				toRenew = append(toRenew, path)
			}
			continue
		}
		toAcquire = append(toAcquire, path)
	}

	var toRelease []string
	for path := range heldAuto {
		if _, ok := desired[path]; ok {
			continue
		}
		toRelease = append(toRelease, path)
	}

	sort.Strings(toAcquire)
	sort.Strings(toRenew)
	sort.Strings(toRelease)

	if len(toAcquire) == 0 && len(toRenew) == 0 && len(toRelease) == 0 {
		return nil
	}

	if len(toAcquire) > 0 {
		for _, path := range toAcquire {
			lockCtx, lockCancel := context.WithTimeout(ctx, apiTimeout)
			_, err := c.ReservationAcquire(lockCtx, &aweb.ReservationAcquireRequest{
				ResourceKey: path,
				TTLSeconds:  reserveDefaultTTL,
				Metadata: map[string]any{
					"reason": autoReserveReason,
				},
			})
			lockCancel()
			if err != nil {
				var held *aweb.ReservationHeldError
				if errors.As(err, &held) {
					retryAfter := ttlRemainingSeconds(held.ExpiresAt, time.Now())
					result.Conflicts = append(result.Conflicts, ReservationConflict{
						ResourceKey:       path,
						HeldBy:            held.HolderAlias,
						RetryAfterSeconds: retryAfter,
						ExpiresAt:         held.ExpiresAt,
					})
					continue
				}
				result.Warning = fmt.Sprintf("Auto-reserve: unable to acquire reservations (%v)", err)
				return result
			}
			result.Acquired = append(result.Acquired, path)
		}
	}

	if len(toRenew) > 0 {
		for _, path := range toRenew {
			lockCtx, lockCancel := context.WithTimeout(ctx, apiTimeout)
			_, err := c.ReservationRenew(lockCtx, &aweb.ReservationRenewRequest{
				ResourceKey: path,
				TTLSeconds:  reserveDefaultTTL,
			})
			lockCancel()
			if err != nil {
				result.Warning = fmt.Sprintf("Auto-reserve: unable to renew reservations (%v)", err)
				return result
			}
			result.Renewed = append(result.Renewed, path)
		}
	}

	if len(toRelease) > 0 {
		for _, path := range toRelease {
			unlockCtx, unlockCancel := context.WithTimeout(ctx, apiTimeout)
			_, err := c.ReservationRelease(unlockCtx, &aweb.ReservationReleaseRequest{
				ResourceKey: path,
			})
			unlockCancel()
			if err != nil {
				result.Warning = fmt.Sprintf("Auto-reserve: unable to release locks (%v)", err)
				return result
			}
			result.Released = append(result.Released, path)
		}
	}

	sort.Strings(result.Acquired)
	sort.Strings(result.Renewed)
	sort.Strings(result.Released)
	return result
}

// gitRepoRoot returns the repository root path from git.
// The returned path is cleaned and validated to be absolute.
func gitRepoRoot(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(string(out))
	if err := validateGitRepoPath(root); err != nil {
		return "", fmt.Errorf("invalid git repo path: %w", err)
	}
	return root, nil
}

// validateGitRepoPath validates a path returned by git for use with git -C.
// Defense-in-depth: ensures paths from git are safe before passing back to git.
func validateGitRepoPath(path string) error {
	if path == "" {
		return fmt.Errorf("path is empty")
	}
	// Clean the path to resolve any . or .. components
	cleaned := filepath.Clean(path)
	if cleaned != path {
		return fmt.Errorf("path contains unclean components")
	}
	// Require absolute path (git --show-toplevel should always return absolute)
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path must be absolute")
	}
	return nil
}

func gitStatusPorcelainV1Z(ctx context.Context, repoRoot string, includeUntracked bool) ([]gitStatusEntry, error) {
	untrackedMode := "no"
	if includeUntracked {
		untrackedMode = "normal"
	}

	args := []string{
		"-C", repoRoot,
		"status",
		"--porcelain=v1",
		"-z",
		"--untracked-files=" + untrackedMode,
		"--",
		":!.beads/",
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parseGitStatusPorcelainV1Z(out)
}

func parseGitStatusPorcelainV1Z(out []byte) ([]gitStatusEntry, error) {
	parts := bytes.Split(out, []byte{0})
	entries := make([]gitStatusEntry, 0, len(parts))

	for i := 0; i < len(parts); i++ {
		part := parts[i]
		if len(part) == 0 {
			continue
		}
		if len(part) < 4 || part[2] != ' ' {
			return nil, fmt.Errorf("unexpected porcelain entry: %q", string(part))
		}

		entry := gitStatusEntry{
			X:    part[0],
			Y:    part[1],
			Path: string(part[3:]),
		}

		if entry.X == 'R' || entry.Y == 'R' || entry.X == 'C' || entry.Y == 'C' {
			if i+1 >= len(parts) || len(parts[i+1]) == 0 {
				return nil, fmt.Errorf("missing rename/copy destination for: %q", entry.Path)
			}
			entry.OrigPath = entry.Path
			entry.Path = string(parts[i+1])
			i++
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

func desiredLockPaths(entries []gitStatusEntry, reserveUntracked bool) map[string]struct{} {
	desired := make(map[string]struct{}, len(entries))

	for _, entry := range entries {
		if entry.Path == "" {
			continue
		}
		if entry.X == '?' && entry.Y == '?' && !reserveUntracked {
			continue
		}
		if entry.X == 'D' || entry.Y == 'D' {
			continue
		}
		if err := validatePath(entry.Path); err != nil {
			continue
		}
		desired[entry.Path] = struct{}{}
	}

	return desired
}
