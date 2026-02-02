package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/beadhub/bdh/internal/client"
	"github.com/beadhub/bdh/internal/config"
)

var (
	addWorktreeAlias string
)

var addWorktreeCmd = &cobra.Command{
	Use:   ":add-worktree <role>",
	Short: "Create an agent worktree (git worktree + bdh :init)",
	Long: `Create an agent worktree and initialize bdh for multi-agent development.

This command:
1. Queries BeadHub for the next available name prefix (e.g., "alice", "bob")
2. Computes the alias <name-prefix>-<role> (unless overridden)
3. Creates a git worktree at ../<repo-name>-<alias>/ on a branch named <alias>
4. Runs bdh :init in the new worktree with the computed alias

The role should be 1-2 words (e.g., "coord", "backend", "full-stack").

Examples:
  bdh :add-worktree coord                 # Creates worktree with alias like alice-coord
  bdh :add-worktree backend --alias bob-backend  # Override default alias`,
	Args: cobra.ExactArgs(1),
	RunE: runAddWorktree,
}

func init() {
	addWorktreeCmd.Flags().StringVar(&addWorktreeAlias, "alias", "", "Override the default alias (default: <name-prefix>-<role>)")
}

// validateNamePrefix checks that the name prefix matches the expected format:
// lowercase letters with optional -NN suffix (e.g., "alice" or "alice-01").
func validateNamePrefix(prefix string) bool {
	if prefix == "" {
		return false
	}
	matched, _ := regexp.MatchString(`^[a-z]+(-[0-9]{2})?$`, prefix)
	return matched
}

// deriveWorktreePath computes the worktree path: ../<repo-name>-<branch>
// Returns an error if the resulting path would escape the parent directory (path traversal).
func deriveWorktreePath(mainRepo, branchName string) (string, error) {
	repoName := filepath.Base(mainRepo)
	parentDir := filepath.Dir(mainRepo)
	worktreePath := filepath.Join(parentDir, repoName+"-"+branchName)

	// Validate against path traversal attacks
	cleanPath := filepath.Clean(worktreePath)
	rel, err := filepath.Rel(parentDir, cleanPath)
	if err != nil {
		return "", fmt.Errorf("invalid worktree path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid worktree path: path traversal detected")
	}

	return cleanPath, nil
}

// findMainRepoRoot returns the git repository root.
func findMainRepoRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not in a git repository")
	}
	return strings.TrimSpace(string(output)), nil
}

// branchExists checks if a git branch exists.
func branchExists(repoPath, branch string) bool {
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "--verify", branch)
	return cmd.Run() == nil
}

// createGitWorktree creates a git worktree at the given path.
// If the branch already exists, it uses that branch; otherwise creates a new branch.
func createGitWorktree(repoPath, worktreePath, branchName string) (branchCreated bool, err error) {
	if branchExists(repoPath, branchName) {
		fmt.Printf("  Using existing branch '%s'\n", branchName)
		cmd := exec.Command("git", "-C", repoPath, "worktree", "add", worktreePath, branchName)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return false, cmd.Run()
	}

	fmt.Printf("  Creating new branch '%s'\n", branchName)
	cmd := exec.Command("git", "-C", repoPath, "worktree", "add", worktreePath, "-b", branchName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return true, cmd.Run()
}

// cleanupWorktree removes a worktree and optionally deletes the branch.
// Logs warnings to stderr on failure, but continues cleanup attempts.
func cleanupWorktree(repoPath, worktreePath, branchName string, deleteBranch bool) {
	// Remove worktree
	cmd := exec.Command("git", "-C", repoPath, "worktree", "remove", worktreePath, "--force")
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: git worktree remove failed: %v\n", err)
	}

	// Fallback: remove directory if git worktree remove failed
	if _, err := os.Stat(worktreePath); err == nil {
		if err := os.RemoveAll(worktreePath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to remove directory %s: %v\n", worktreePath, err)
		}
	}

	// Delete branch if we created it
	if deleteBranch && branchExists(repoPath, branchName) {
		// Check if another worktree is using this branch by looking for it in worktree list output.
		// Use exact branch name match pattern to avoid false positives with similar names.
		listCmd := exec.Command("git", "-C", repoPath, "worktree", "list")
		output, _ := listCmd.Output()
		// Look for " [branchName]" pattern to avoid matching alice-01 when deleting alice
		branchPattern := " [" + branchName + "]"
		if !strings.Contains(string(output), branchPattern) {
			deleteCmd := exec.Command("git", "-C", repoPath, "branch", "-d", branchName)
			if err := deleteCmd.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to delete branch %s: %v\n", branchName, err)
			}
		}
	}
}

func runAddWorktree(cmd *cobra.Command, args []string) error {
	role := args[0]

	// Validate role
	normalizedRole := config.NormalizeRole(role)
	if !config.IsValidRole(normalizedRole) {
		return fmt.Errorf("invalid role: use 1-2 words (letters/numbers) with hyphens/underscores allowed; max 50 chars")
	}

	// Find main repo
	mainRepo, err := findMainRepoRoot()
	if err != nil {
		return fmt.Errorf("failed to find git repository: %w", err)
	}

	// Load existing config to get BeadHub URL
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w\n(run 'bdh :init' first to register this repository)", err)
	}

	// Get git remote origin for API call
	repoOrigin := os.Getenv("BEADHUB_REPO_ORIGIN")
	if repoOrigin == "" {
		originCmd := exec.Command("git", "remote", "get-url", "origin")
		output, err := originCmd.Output()
		if err != nil {
			return fmt.Errorf("failed to get git remote origin: %w", err)
		}
		repoOrigin = strings.TrimSpace(string(output))
	}

	alias := strings.TrimSpace(addWorktreeAlias)
	aliasExplicit := alias != ""
	if aliasExplicit && !config.IsValidAlias(alias) {
		return fmt.Errorf("invalid alias %q: must start with an alphanumeric and contain only alphanumerics, dashes, or underscores (max 64 chars)", alias)
	}

	// If alias is not explicit, we need an authenticated BeadHub client to ask for the
	// next available name prefix (alice, bob, ...).
	var c *client.Client
	if !aliasExplicit {
		fmt.Println("Querying BeadHub for next available name...")
		c, err = newBeadHubClientRequired(cfg.BeadhubURL)
		if err != nil {
			return err
		}
	}

	origDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	const maxAttempts = 25
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if !aliasExplicit {
			ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
			resp, err := c.SuggestNamePrefix(ctx, &client.SuggestNamePrefixRequest{OriginURL: repoOrigin})
			cancel()
			if err != nil {
				var clientErr *client.Error
				if errors.As(err, &clientErr) {
					if clientErr.StatusCode == 404 {
						return fmt.Errorf("repo not registered. Run 'bdh :init' first")
					}
				}
				return fmt.Errorf("failed to get name prefix: %w", err)
			}

			namePrefix := resp.NamePrefix
			if !validateNamePrefix(namePrefix) {
				return fmt.Errorf("invalid name prefix from server: %q\nExpected format: lowercase letters with optional -NN suffix (e.g., 'alice' or 'alice-01')", namePrefix)
			}

			alias = fmt.Sprintf("%s-%s", namePrefix, config.RoleToAliasPrefix(normalizedRole))
		}

		branchName := alias
		worktreePath, err := deriveWorktreePath(mainRepo, branchName)
		if err != nil {
			return fmt.Errorf("security error: %w", err)
		}

		if _, err := os.Stat(worktreePath); err == nil {
			return fmt.Errorf("directory %s already exists", worktreePath)
		}

		fmt.Printf("Creating worktree for branch '%s'...\n", branchName)
		fmt.Printf("  Main repo: %s\n", mainRepo)
		fmt.Printf("  Worktree:  %s\n", worktreePath)
		fmt.Printf("  Role:      %s\n", normalizedRole)
		fmt.Printf("  Alias:     %s\n", alias)
		fmt.Println()

		fmt.Println("Creating git worktree...")
		branchCreated, err := createGitWorktree(mainRepo, worktreePath, branchName)
		if err != nil {
			return fmt.Errorf("failed to create worktree: %w", err)
		}

		fmt.Println("Initializing bdh...")
		if err := os.Chdir(worktreePath); err != nil {
			cleanupWorktree(mainRepo, worktreePath, branchName, branchCreated)
			return fmt.Errorf("failed to change to worktree directory: %w", err)
		}

		resetAddWorktreeInitFlags()
		initURL = cfg.BeadhubURL
		initAlias = alias
		initRole = normalizedRole
		initHuman = cfg.HumanName
		initProject = cfg.ProjectSlug

		initErr := runInit()
		_ = os.Chdir(origDir)
		if initErr != nil {
			fmt.Println()
			fmt.Println("Error: Failed to initialize bdh. Cleaning up worktree...")
			cleanupWorktree(mainRepo, worktreePath, branchName, branchCreated)

			if !aliasExplicit && isAliasAlreadyTakenError(initErr) {
				fmt.Printf("Alias %q was taken; retrying with a new name...\n", alias)
				if attempt < maxAttempts {
					// Avoid hammering the server when racing with other worktrees.
					time.Sleep(time.Duration(attempt) * 50 * time.Millisecond)
				}
				continue
			}

			return fmt.Errorf("bdh :init failed: %w", initErr)
		}

		fmt.Println()
		displayPath := worktreePath
		if rel, err := filepath.Rel(mainRepo, worktreePath); err == nil {
			displayPath = rel
		}
		fmt.Printf("Agent worktree created at %s\n", displayPath)
		fmt.Println()
		fmt.Println("To use:")
		fmt.Printf("  cd %s\n", worktreePath)
		fmt.Println("  bdh ready")
		return nil
	}

	return fmt.Errorf("exhausted %d attempts to create a worktree (try specifying --alias)", maxAttempts)
}

func isAliasAlreadyTakenError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "alias") && strings.Contains(s, "already taken")
}

// resetAddWorktreeInitFlags resets init flags before calling runInit.
func resetAddWorktreeInitFlags() {
	initURL = ""
	initAlias = ""
	initHuman = ""
	initProject = ""
	initRole = ""
	initUpdate = false
	initInjectDocs = false
}
