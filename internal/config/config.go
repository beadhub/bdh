// Package config handles .beadhub configuration file parsing.
//
// The .beadhub file is located at the workspace root and contains:
//
//	workspace_id: "uuid"                      - Workspace identifier (auto-generated)
//	beadhub_url: "http://..."                 - BeadHub server URL
//	project_slug: "beadhub"                   - Human-readable project slug
//	repo_origin: "git@github.com:org/repo"    - Git remote origin URL
//	alias: "claude-code"                      - Human-friendly workspace address
//	human_name: "Juan"                        - Human owner of this workspace
//	role: "reviewer"                          - Optional short workspace role
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// FileName is the name of the configuration file.
const FileName = ".beadhub"

// customPath holds an optional custom config file path.
// When empty, Load() uses the default FileName.
var customPath string

// SetPath sets a custom config file path for Load() to use.
// Pass an empty string to reset to the default path.
func SetPath(path string) {
	customPath = path
}

// GetPath returns the current config file path.
// Returns the custom path if set, otherwise the default FileName.
func GetPath() string {
	if customPath != "" {
		return customPath
	}
	return FileName
}

// FindPath resolves the config file path using the same logic as Load(),
// without reading or parsing the file contents.
func FindPath() (string, error) {
	if customPath != "" {
		return customPath, nil
	}
	return findDefaultConfigPath()
}

// WorkspaceRoot returns the directory containing the resolved config file.
func WorkspaceRoot() (string, error) {
	path, err := FindPath()
	if err != nil {
		return "", err
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return filepath.Dir(path), nil
}

// Validation patterns (matching the bash script)
var (
	uuidPattern  = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	urlPattern   = regexp.MustCompile(`^https?://[^\s]+$`)
	aliasPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
	// Match server: src/beadhub/beads_sync.py:HUMAN_NAME_PATTERN
	humanNamePattern   = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9 '\-]{0,63}$`)
	projectSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	// repoOriginPattern matches git SSH (git@host:path) or HTTPS URLs
	repoOriginPattern = regexp.MustCompile(`^(git@[^\s:]+:[^\s]+|https?://[^\s]+)$`)
	// canonicalOriginPattern matches normalized repo URLs like "github.com/org/repo"
	canonicalOriginPattern = regexp.MustCompile(`^[a-z0-9.-]+/[a-zA-Z0-9._-]+/[a-zA-Z0-9._-]+$`)
	roleWordPattern        = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)
	roleMaxLength          = 50
	roleMaxWords           = 2
)

// Config represents the .beadhub configuration file.
type Config struct {
	WorkspaceID      string `yaml:"workspace_id"`
	BeadhubURL       string `yaml:"beadhub_url"`
	ProjectSlug      string `yaml:"project_slug"`
	RepoID           string `yaml:"repo_id,omitempty"`
	RepoOrigin       string `yaml:"repo_origin"`
	CanonicalOrigin  string `yaml:"canonical_origin"`
	Alias            string `yaml:"alias"`
	HumanName        string `yaml:"human_name"`
	Role             string `yaml:"role,omitempty"`
	AutoReserve      *bool  `yaml:"auto_reserve,omitempty"`
	ReserveUntracked *bool  `yaml:"reserve_untracked,omitempty"`
}

func (c *Config) AutoReserveEnabled() bool {
	if c.AutoReserve == nil {
		return true
	}
	return *c.AutoReserve
}

func (c *Config) ReserveUntrackedEnabled() bool {
	if c.ReserveUntracked == nil {
		return false
	}
	return *c.ReserveUntracked
}

// Load reads and parses the .beadhub configuration file.
// Uses the custom path if set via SetPath(), otherwise uses the default FileName.
func Load() (*Config, error) {
	if customPath != "" {
		return LoadFrom(customPath)
	}

	path, err := findDefaultConfigPath()
	if err != nil {
		return nil, err
	}
	return LoadFrom(path)
}

// LoadFrom reads and parses a .beadhub configuration file from a specific path.
func LoadFrom(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err // Return unwrapped for os.IsNotExist() checks
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	return &cfg, nil
}

func findDefaultConfigPath() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		// Fallback: look only in current directory
		return FileName, nil
	}

	gitRoot, ok := findGitRoot(cwd)
	if !ok {
		// If we're not in a git worktree, don't walk parents (avoid accidentally
		// picking up an unrelated .beadhub higher up).
		return FileName, nil
	}

	dir := cwd
	for {
		candidate := filepath.Join(dir, FileName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}

		if dir == gitRoot {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// Return an IsNotExist error with a helpful path (repo root) so callers
	// can still rely on os.IsNotExist(err).
	rootCandidate := filepath.Join(gitRoot, FileName)
	return rootCandidate, &os.PathError{Op: "open", Path: rootCandidate, Err: os.ErrNotExist}
}

func findGitRoot(start string) (string, bool) {
	dir := start
	for {
		gitPath := filepath.Join(dir, ".git")
		if _, err := os.Stat(gitPath); err == nil {
			return dir, true
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", false
}

// Save writes the configuration to the config file.
// Uses the custom path if set via SetPath(), otherwise uses the default FileName.
func (c *Config) Save() error {
	path := GetPath()
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	// Write with header comment
	header := "# Generated by: bdh init\n# DO NOT COMMIT - add to .gitignore\n\n"
	content := header + string(data)

	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	return nil
}

// Validate checks that all required fields are present and valid.
func (c *Config) Validate() error {
	if c.WorkspaceID == "" {
		return fmt.Errorf("workspace_id is required")
	}
	if !uuidPattern.MatchString(c.WorkspaceID) {
		return fmt.Errorf("workspace_id must be a valid UUID")
	}
	if c.BeadhubURL == "" {
		return fmt.Errorf("beadhub_url is required")
	}
	if !urlPattern.MatchString(c.BeadhubURL) {
		return fmt.Errorf("beadhub_url must be a valid HTTP(S) URL")
	}
	if c.ProjectSlug == "" {
		return fmt.Errorf("project_slug is required")
	}
	if !projectSlugPattern.MatchString(c.ProjectSlug) {
		return fmt.Errorf("project_slug must be lowercase alphanumeric with hyphens")
	}
	if c.RepoID != "" && !uuidPattern.MatchString(c.RepoID) {
		return fmt.Errorf("repo_id must be a valid UUID")
	}
	if c.RepoOrigin == "" {
		return fmt.Errorf("repo_origin is required")
	}
	if !repoOriginPattern.MatchString(c.RepoOrigin) {
		return fmt.Errorf("repo_origin must be a git SSH URL (git@host:path) or HTTPS URL")
	}
	if c.CanonicalOrigin == "" {
		return fmt.Errorf("canonical_origin is required")
	}
	if !canonicalOriginPattern.MatchString(c.CanonicalOrigin) {
		return fmt.Errorf("canonical_origin must be in format host/org/repo (e.g., github.com/org/repo)")
	}
	if c.Alias == "" {
		return fmt.Errorf("alias is required")
	}
	if !aliasPattern.MatchString(c.Alias) {
		return fmt.Errorf("alias must start with an alphanumeric and contain only alphanumerics, dashes, or underscores (max 64 chars)")
	}
	if c.HumanName == "" {
		return fmt.Errorf("human_name is required")
	}
	if !humanNamePattern.MatchString(c.HumanName) {
		return fmt.Errorf("human_name must start with a letter and contain only letters, digits, spaces, hyphens, or apostrophes (max 64 chars)")
	}
	if c.Role != "" && !IsValidRole(c.Role) {
		return fmt.Errorf("role must be 1-2 words (letters/numbers) with hyphens/underscores allowed; max 50 chars")
	}

	return nil
}

// IsValidAlias checks if the alias matches the server-compatible workspace alias rules.
func IsValidAlias(alias string) bool {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return false
	}
	return aliasPattern.MatchString(alias)
}

// NormalizeRole trims, collapses spaces, and lowercases a role string.
func NormalizeRole(role string) string {
	fields := strings.Fields(strings.TrimSpace(role))
	if len(fields) == 0 {
		return ""
	}
	return strings.ToLower(strings.Join(fields, " "))
}

// IsValidRole checks if the role is 1-2 words and uses valid characters.
func IsValidRole(role string) bool {
	normalized := NormalizeRole(role)
	if normalized == "" || len(normalized) > roleMaxLength {
		return false
	}
	words := strings.Split(normalized, " ")
	if len(words) > roleMaxWords {
		return false
	}
	for _, word := range words {
		if !roleWordPattern.MatchString(word) {
			return false
		}
	}
	return true
}

// RoleToAliasPrefix converts a role to a hyphenated alias prefix.
func RoleToAliasPrefix(role string) string {
	return strings.ReplaceAll(NormalizeRole(role), " ", "-")
}

// SanitizeSlug converts a string (typically a directory name) to a valid project slug.
// It handles common issues like underscores, dots, spaces, and special characters.
// Returns an empty string if the input cannot be sanitized to a valid slug.
func SanitizeSlug(name string) string {
	if name == "" {
		return ""
	}

	// Lowercase first
	s := strings.ToLower(name)

	// Replace common separators with hyphens
	replacer := strings.NewReplacer(
		"_", "-",
		".", "-",
		" ", "-",
	)
	s = replacer.Replace(s)

	// Remove any characters that aren't alphanumeric or hyphen
	var result strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result.WriteRune(r)
		}
	}
	s = result.String()

	// Collapse multiple hyphens to single
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}

	// Trim leading/trailing hyphens
	s = strings.Trim(s, "-")

	// Enforce max length (63 chars for DNS-compatible slug)
	if len(s) > 63 {
		s = s[:63]
		// Ensure we don't end with a hyphen after truncation
		s = strings.TrimRight(s, "-")
	}

	// Must start with alphanumeric
	if len(s) == 0 {
		return ""
	}
	if (s[0] < 'a' || s[0] > 'z') && (s[0] < '0' || s[0] > '9') {
		return ""
	}

	return s
}

// IsValidSlug checks if a string is a valid project slug.
func IsValidSlug(slug string) bool {
	return projectSlugPattern.MatchString(slug) && len(slug) <= 63
}
