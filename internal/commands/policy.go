package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/beadhub/bdh/internal/client"
	"github.com/beadhub/bdh/internal/config"
)

var (
	policyJSON         bool
	policyRole         string
	policyOnlySelected bool
	policyFormat       string
)

const policyCacheTTL = 60 * time.Second

var policyCmd = &cobra.Command{
	Use:   ":policy",
	Short: "Show active project policy and role playbook",
	Long: `Show the active project policy bundle and a role playbook for this workspace.

Examples:
  bdh :policy
  bdh :policy --role reviewer
  bdh :policy --json
  bdh :policy --only-selected=false`,
	RunE: runPolicy,
}

func init() {
	policyCmd.Flags().BoolVar(&policyJSON, "json", false, "Output as JSON")
	policyCmd.Flags().StringVar(&policyRole, "role", "", "Preview a specific role (defaults to .beadhub role)")
	policyCmd.Flags().BoolVar(&policyOnlySelected, "only-selected", true, "Show only invariants + selected role playbook (set false to include all roles)")
	policyCmd.Flags().StringVar(&policyFormat, "format", "plain", "Output format: plain or markdown")
}

type PolicyCacheInfo struct {
	Used     bool   `json:"used"`
	Mode     string `json:"mode,omitempty"` // fresh, validated, offline
	Stale    bool   `json:"stale,omitempty"`
	CachedAt string `json:"cached_at,omitempty"`
}

type PolicyResult struct {
	Role         string                       `json:"role"`
	OnlySelected bool                         `json:"only_selected"`
	Policy       *client.ActivePolicyResponse `json:"policy"`
	Cache        *PolicyCacheInfo             `json:"cache,omitempty"`
}

func runPolicy(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no .beadhub file found - run 'bdh :init' first")
		}
		return fmt.Errorf("loading config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid .beadhub config: %w", err)
	}
	if err := validateRepoOriginMatchesCurrent(cfg); err != nil {
		return err
	}

	// Notifications are handled by main.go's PrintNotifications

	role := policyRole
	if role == "" {
		role = cfg.Role
	}
	if role == "" {
		role = "implementer"
	}
	role = config.NormalizeRole(role)
	if role == "" || !config.IsValidRole(role) {
		return fmt.Errorf("invalid role %q", role)
	}

	format := strings.ToLower(strings.TrimSpace(policyFormat))
	if format == "" {
		format = "plain"
	}
	if format != "plain" && format != "markdown" {
		return fmt.Errorf("invalid --format %q (expected plain or markdown)", policyFormat)
	}

	workspaceRoot := filepath.Dir(config.GetPath())
	if root, err := config.WorkspaceRoot(); err == nil {
		workspaceRoot = root
	}
	result, err := fetchActivePolicyCachedWithConfig(cfg, role, policyOnlySelected, workspaceRoot)
	if err != nil {
		return err
	}

	fmt.Print(formatPolicyOutput(result, policyJSON, format))
	return nil
}

// fetchActivePolicyCachedWithConfig fetches the active policy bundle with workspace-local caching and offline fallback (for testing).
func fetchActivePolicyCachedWithConfig(cfg *config.Config, role string, onlySelected bool, workspaceRoot string) (*PolicyResult, error) {
	cacheDir := filepath.Join(workspaceRoot, ".beadhub-cache")
	cachePath := filepath.Join(cacheDir, policyCacheFilename(role, onlySelected))

	now := time.Now()
	cache, err := readPolicyCache(cachePath)
	if err != nil {
		// Corrupt cache should not block live fetch.
		cache = nil
	}

	// If cache is fresh, use it without hitting the server.
	if cache != nil && cache.Policy != nil && cacheIsFresh(cache.CachedAt, now, policyCacheTTL) {
		result := policyResultFromPolicy(cache.Policy, role, onlySelected)
		result.Cache = &PolicyCacheInfo{Used: true, Mode: "fresh", CachedAt: cache.CachedAt}
		return result, nil
	}

	c, err := newBeadHubClientRequired(cfg.BeadhubURL)
	if err != nil {
		if cache != nil && cache.Policy != nil {
			result := policyResultFromPolicy(cache.Policy, role, onlySelected)
			result.Cache = &PolicyCacheInfo{
				Used:     true,
				Mode:     "offline",
				Stale:    !cacheIsFresh(cache.CachedAt, now, policyCacheTTL),
				CachedAt: cache.CachedAt,
			}
			return result, nil
		}
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	var opts *client.ActivePolicyFetchOptions
	if cache != nil {
		opts = &client.ActivePolicyFetchOptions{
			IfNoneMatch:     cache.ETag,
			IfModifiedSince: cache.LastModified,
		}
	}

	fetchResp, fetchErr := c.ActivePolicyFetch(ctx, &client.ActivePolicyRequest{
		Role:         role,
		OnlySelected: onlySelected,
	}, opts)
	if fetchErr != nil {
		var clientErr *client.Error
		if errors.As(fetchErr, &clientErr) && role != "" && clientErr.StatusCode == 400 {
			if roles, rolesErr := fetchAvailablePolicyRolesWithConfig(cfg); rolesErr == nil && len(roles) > 0 {
				return nil, fmt.Errorf("role %q rejected by server (available roles: %s)", role, strings.Join(roles, ", "))
			}
		}
		if errors.As(fetchErr, &clientErr) {
			// Auth errors are common when users haven't loaded .env; fall back to cache when possible.
			if (clientErr.StatusCode == 401 || clientErr.StatusCode == 403) && cache != nil && cache.Policy != nil {
				result := policyResultFromPolicy(cache.Policy, role, onlySelected)
				result.Cache = &PolicyCacheInfo{
					Used:     true,
					Mode:     "offline",
					Stale:    !cacheIsFresh(cache.CachedAt, now, policyCacheTTL),
					CachedAt: cache.CachedAt,
				}
				return result, nil
			}
			return nil, fmt.Errorf("BeadHub error (%d): %s", clientErr.StatusCode, clientErr.Body)
		}

		// Network/unreachable: fall back to cache if available.
		if cache != nil && cache.Policy != nil {
			result := policyResultFromPolicy(cache.Policy, role, onlySelected)
			result.Cache = &PolicyCacheInfo{
				Used:     true,
				Mode:     "offline",
				Stale:    !cacheIsFresh(cache.CachedAt, now, policyCacheTTL),
				CachedAt: cache.CachedAt,
			}
			return result, nil
		}

		return nil, fmt.Errorf("failed to fetch policy: %w", fetchErr)
	}

	// 304 Not Modified: use cached policy and refresh cache timestamp.
	if fetchResp.StatusCode == 304 {
		if cache == nil || cache.Policy == nil {
			return nil, fmt.Errorf("server returned 304 Not Modified but no cache is available")
		}
		cache.CachedAt = now.Format(time.RFC3339)
		if fetchResp.ETag != "" {
			cache.ETag = fetchResp.ETag
		}
		if fetchResp.LastModified != "" {
			cache.LastModified = fetchResp.LastModified
		}
		if err := ensurePolicyCacheDir(workspaceRoot); err == nil {
			_ = writePolicyCache(cachePath, cache)
		}
		result := policyResultFromPolicy(cache.Policy, role, onlySelected)
		result.Cache = &PolicyCacheInfo{Used: true, Mode: "validated", CachedAt: cache.CachedAt}
		return result, nil
	}

	if fetchResp.Policy == nil {
		return nil, fmt.Errorf("unexpected empty policy response")
	}

	// 200 OK: write/update cache.
	if err := ensurePolicyCacheDir(workspaceRoot); err == nil {
		newCache := &policyCacheFile{
			CachedAt:     now.Format(time.RFC3339),
			ETag:         fetchResp.ETag,
			LastModified: fetchResp.LastModified,
			Policy:       fetchResp.Policy,
		}
		_ = writePolicyCache(cachePath, newCache)
	}

	return policyResultFromPolicy(fetchResp.Policy, role, onlySelected), nil
}

// fetchActivePolicyWithConfig fetches the active policy bundle for a workspace's project (for testing).
func fetchActivePolicyWithConfig(cfg *config.Config, role string, onlySelected bool) (*PolicyResult, error) {
	c := newBeadHubClient(cfg.BeadhubURL)
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	resp, err := c.ActivePolicy(ctx, &client.ActivePolicyRequest{
		Role:         role,
		OnlySelected: onlySelected,
	})
	if err != nil {
		var clientErr *client.Error
		if errors.As(err, &clientErr) && role != "" && clientErr.StatusCode == 400 {
			if roles, rolesErr := fetchAvailablePolicyRolesWithConfig(cfg); rolesErr == nil && len(roles) > 0 {
				return nil, fmt.Errorf("role %q rejected by server (available roles: %s)", role, strings.Join(roles, ", "))
			}
		}
		if errors.As(err, &clientErr) {
			return nil, fmt.Errorf("BeadHub error (%d): %s", clientErr.StatusCode, clientErr.Body)
		}
		return nil, fmt.Errorf("failed to fetch policy: %w", err)
	}

	// Best-effort: if the server returns roles but not selected_role, synthesize it.
	if resp.SelectedRole == nil && resp.Roles != nil {
		if playbook, ok := resp.Roles[role]; ok {
			resp.SelectedRole = &client.SelectedPolicyRole{
				Role:       role,
				Title:      playbook.Title,
				PlaybookMD: playbook.PlaybookMD,
			}
		}
	}

	return &PolicyResult{
		Role:         role,
		OnlySelected: onlySelected,
		Policy:       resp,
	}, nil
}

func policyResultFromPolicy(policy *client.ActivePolicyResponse, role string, onlySelected bool) *PolicyResult {
	if policy == nil {
		return &PolicyResult{
			Role:         role,
			OnlySelected: onlySelected,
			Policy:       &client.ActivePolicyResponse{},
		}
	}

	// Always attempt to show the requested role if roles are available (useful for cached full bundles).
	if policy.Roles != nil {
		if playbook, ok := policy.Roles[role]; ok {
			policy.SelectedRole = &client.SelectedPolicyRole{
				Role:       role,
				Title:      playbook.Title,
				PlaybookMD: playbook.PlaybookMD,
			}
		}
	}

	// If roles are not available, keep the server-provided selection (or empty).
	return &PolicyResult{
		Role:         role,
		OnlySelected: onlySelected,
		Policy:       policy,
	}
}

func fetchAvailablePolicyRolesWithConfig(cfg *config.Config) ([]string, error) {
	c := newBeadHubClient(cfg.BeadhubURL)
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	resp, err := c.ActivePolicy(ctx, nil)
	if err != nil {
		return nil, err
	}

	roles := make([]string, 0, len(resp.Roles))
	for role := range resp.Roles {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	return roles, nil
}

func formatPolicyOutput(result *PolicyResult, asJSON bool, format string) string {
	if asJSON {
		return marshalJSONOrFallback(result)
	}
	if format == "markdown" {
		return formatPolicyMarkdown(result)
	}
	return formatPolicyPlain(result)
}

func formatPolicyPlain(result *PolicyResult) string {
	p := result.Policy
	var sb strings.Builder

	if result.Cache != nil && result.Cache.Used && result.Cache.Mode == "offline" {
		if result.Cache.Stale {
			sb.WriteString(fmt.Sprintf("CACHED (STALE) — cached_at: %s\n\n", result.Cache.CachedAt))
		} else {
			sb.WriteString(fmt.Sprintf("CACHED — cached_at: %s\n\n", result.Cache.CachedAt))
		}
	}

	sb.WriteString(fmt.Sprintf("Policy: v%d", p.Version))
	if p.UpdatedAt != "" {
		sb.WriteString(fmt.Sprintf(" (updated %s)", formatTimeAgo(p.UpdatedAt)))
	}
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("Role: %s\n\n", result.Role))

	// Beads workflow content (first, as foundational reference)
	if beadsContent, err := GetBeadsPrimeContent(); err == nil && strings.TrimSpace(beadsContent) != "" {
		sb.WriteString("Beads workflow:\n")
		sb.WriteString(indentBlock(beadsContent, "  "))
		sb.WriteString("\n")
	}

	sb.WriteString("Global invariants:\n")
	invariants := p.Invariants // preserve server order (sorted by filename)
	if len(invariants) == 0 {
		sb.WriteString("  (none)\n")
	} else {
		for _, inv := range invariants {
			title := strings.TrimSpace(inv.Title)
			if title == "" {
				title = inv.ID
			}
			sb.WriteString(fmt.Sprintf("  - %s\n", title))
			if body := strings.TrimSpace(inv.BodyMD); body != "" {
				sb.WriteString(indentBlock(body, "    "))
			}
		}
	}
	sb.WriteString("\n")

	playbookRole := result.Role
	playbookTitle := ""
	playbookBody := ""
	if p.SelectedRole != nil {
		if p.SelectedRole.Role != "" {
			playbookRole = p.SelectedRole.Role
		}
		playbookTitle = p.SelectedRole.Title
		playbookBody = p.SelectedRole.PlaybookMD
	}

	sb.WriteString(fmt.Sprintf("Role playbook (%s):\n", playbookRole))
	if strings.TrimSpace(playbookTitle) != "" {
		sb.WriteString(fmt.Sprintf("  %s\n", strings.TrimSpace(playbookTitle)))
	}
	if strings.TrimSpace(playbookBody) == "" {
		sb.WriteString("  (empty)\n")
	} else {
		sb.WriteString(indentBlock(playbookBody, "  "))
	}

	return sb.String()
}

func formatPolicyMarkdown(result *PolicyResult) string {
	p := result.Policy
	var sb strings.Builder

	if result.Cache != nil && result.Cache.Used && result.Cache.Mode == "offline" {
		if result.Cache.Stale {
			sb.WriteString(fmt.Sprintf("> **CACHED (STALE)** — cached_at: %s\n\n", result.Cache.CachedAt))
		} else {
			sb.WriteString(fmt.Sprintf("> **CACHED** — cached_at: %s\n\n", result.Cache.CachedAt))
		}
	}

	sb.WriteString(fmt.Sprintf("# Project Policy (v%d)\n\n", p.Version))
	if p.UpdatedAt != "" {
		sb.WriteString(fmt.Sprintf("- Updated: %s\n", p.UpdatedAt))
	}
	sb.WriteString(fmt.Sprintf("- Role: %s\n\n", result.Role))

	// Beads workflow content (first, as foundational reference)
	if beadsContent, err := GetBeadsPrimeContent(); err == nil && strings.TrimSpace(beadsContent) != "" {
		sb.WriteString("## Beads workflow\n\n")
		sb.WriteString(strings.TrimRight(beadsContent, "\n"))
		sb.WriteString("\n\n")
	}

	sb.WriteString("## Global invariants\n")
	invariants := p.Invariants // preserve server order (sorted by filename)
	if len(invariants) == 0 {
		sb.WriteString("_None_\n")
	} else {
		for _, inv := range invariants {
			title := strings.TrimSpace(inv.Title)
			if title == "" {
				title = inv.ID
			}
			sb.WriteString(fmt.Sprintf("### %s\n\n", title))
			if body := strings.TrimSpace(inv.BodyMD); body != "" {
				sb.WriteString(body)
				sb.WriteString("\n\n")
			}
		}
	}

	playbookRole := result.Role
	playbookBody := ""
	if p.SelectedRole != nil {
		if p.SelectedRole.Role != "" {
			playbookRole = p.SelectedRole.Role
		}
		playbookBody = p.SelectedRole.PlaybookMD
	}

	sb.WriteString(fmt.Sprintf("## Role playbook (%s)\n", playbookRole))
	if strings.TrimSpace(playbookBody) == "" {
		sb.WriteString("_Empty_\n")
	} else {
		sb.WriteString("\n")
		sb.WriteString(strings.TrimRight(playbookBody, "\n"))
		sb.WriteString("\n")
	}

	return sb.String()
}

func indentBlock(s string, prefix string) string {
	trimmed := strings.TrimRight(s, "\n")
	if trimmed == "" {
		return ""
	}
	lines := strings.Split(trimmed, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n") + "\n"
}

type policyCacheFile struct {
	CachedAt     string                       `json:"cached_at"`
	ETag         string                       `json:"etag,omitempty"`
	LastModified string                       `json:"last_modified,omitempty"`
	Policy       *client.ActivePolicyResponse `json:"policy"`
}

func policyCacheFilename(role string, onlySelected bool) string {
	if !onlySelected {
		return "policy-active.json"
	}
	safeRole := strings.ReplaceAll(role, " ", "_")
	safeRole = strings.ReplaceAll(safeRole, "/", "_")
	if safeRole == "" {
		safeRole = "default"
	}
	return fmt.Sprintf("policy-active-only-selected-%s.json", safeRole)
}

func cacheIsFresh(cachedAt string, now time.Time, ttl time.Duration) bool {
	if cachedAt == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, cachedAt)
	if err != nil {
		return false
	}
	return now.Sub(t) < ttl
}

func readPolicyCache(path string) (*policyCacheFile, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	const maxCacheSize = 10 * 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(f, maxCacheSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxCacheSize {
		return nil, fmt.Errorf("cache file exceeds maximum size of %d bytes", maxCacheSize)
	}

	var cache policyCacheFile
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}
	return &cache, nil
}

func writePolicyCache(path string, cache *policyCacheFile) error {
	if cache == nil {
		return fmt.Errorf("cache is nil")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmpFile, err := os.CreateTemp(dir, "policy-cache-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()

	cleanup := func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpName)
	}

	if _, err := tmpFile.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}

	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	_ = os.Chmod(path, 0600)
	return nil
}

func ensurePolicyCacheDir(workspaceRoot string) error {
	cacheDir := filepath.Join(workspaceRoot, ".beadhub-cache")
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return err
	}
	_ = ensureGitExcludePattern(workspaceRoot, ".beadhub-cache/")
	return nil
}

func ensureGitExcludePattern(workspaceRoot string, pattern string) error {
	gitPath := filepath.Join(workspaceRoot, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	gitDir := gitPath
	if info.Mode().IsRegular() {
		data, err := os.ReadFile(gitPath)
		if err != nil {
			return nil
		}
		line := strings.TrimSpace(string(data))
		const prefix = "gitdir:"
		if !strings.HasPrefix(strings.ToLower(line), prefix) {
			return nil
		}
		dir := strings.TrimSpace(line[len(prefix):])
		if dir == "" {
			return nil
		}
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(workspaceRoot, dir)
		}
		gitDir = dir
	} else if !info.IsDir() {
		return nil
	}

	excludePath := filepath.Join(gitDir, "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(excludePath), 0700); err != nil {
		return nil
	}

	existing, err := os.ReadFile(excludePath)
	if err != nil && !os.IsNotExist(err) {
		return nil
	}

	for _, line := range strings.Split(string(existing), "\n") {
		if strings.TrimSpace(line) == strings.TrimSpace(pattern) {
			return nil
		}
	}

	f, err := os.OpenFile(excludePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		if _, err := f.WriteString("\n"); err != nil {
			return nil
		}
	}
	_, _ = f.WriteString(pattern + "\n")
	return nil
}
