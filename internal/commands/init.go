package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/beadhub/bdh/internal/beads"
	"github.com/beadhub/bdh/internal/client"
	"github.com/beadhub/bdh/internal/config"
)

// CLI flags for init command
var (
	initURL        string
	initAlias      string
	initHuman      string
	initProject    string
	initRole       string
	initUpdate     bool
	initInjectDocs bool
)

var initCmd = &cobra.Command{
	Use:   ":init",
	Short: "Register workspace with BeadHub",
	Long: `Register this workspace with BeadHub by creating a .beadhub configuration file.

This command will:
1. Register with BeadHub server
2. Create .beadhub configuration file

Configuration sources (in priority order):
1. Command line flags (--beadhub-url, --alias, --human, --project, --role)
2. Environment variables (BEADHUB_URL, BEADHUB_ALIAS, BEADHUB_HUMAN, BEADHUB_PROJECT, BEADHUB_ROLE)
3. .env file in current directory
4. Interactive prompts (TTY mode only)
5. Defaults (role: agent, alias: server-suggested, human: $USER)

Default alias format: <name>-<role> (e.g., alice-implementer, bob-reviewer).
The server suggests a unique name prefix per project; you can override in TTY mode.

Use --update to update the workspace's hostname and workspace_path on the server.
This is useful when moving a workspace to a different machine or directory.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInit()
	},
}

func init() {
	initCmd.Flags().StringVar(&initURL, "beadhub-url", "", "BeadHub server URL")
	initCmd.Flags().StringVar(&initAlias, "alias", "", "Workspace alias (e.g., claude-main)")
	initCmd.Flags().StringVar(&initHuman, "human", "", "Human name for this workspace")
	initCmd.Flags().StringVar(&initProject, "project", "", "Project slug")
	initCmd.Flags().StringVar(&initRole, "role", "", "Workspace role (e.g., reviewer)")
	initCmd.Flags().BoolVar(&initUpdate, "update", false, "Update workspace location (hostname/path) on server")
	initCmd.Flags().BoolVar(&initInjectDocs, "inject-docs", false, "Inject bdh instructions into CLAUDE.md/AGENTS.md")
}

// isTTY returns true if stdin is a terminal.
func isTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// runInit implements the :init command logic.
// Flags are parsed by Cobra and stored in initURL, initAlias, etc.
func runInit() error {
	// Load .env best-effort (workspace root preferred) so env-based config works even
	// when invoked from a subdirectory.
	loadDotenvBestEffort()

	// Check if already initialized (just check file existence, like bash)
	if _, err := os.Stat(config.FileName); err == nil {
		cfg, loadErr := config.Load()
		if loadErr != nil {
			return fmt.Errorf("failed to load existing config: %w", loadErr)
		}

		// Handle --inject-docs for existing workspace
		if initInjectDocs {
			wd, _ := os.Getwd()
			if agentDocsResult, err := InjectAgentDocs(wd); err != nil {
				return fmt.Errorf("failed to inject agent docs: %w", err)
			} else {
				PrintAgentDocsResult(agentDocsResult)
			}
			// Also inject PRIME.md override
			primeResult := InjectPrimeOverride(wd)
			PrintPrimeOverrideResult(primeResult)
			return nil
		}

		if !initUpdate {
			// No --update flag: just print info and exit
			wd, _ := os.Getwd()
			fmt.Printf("BeadHub workspace already initialized at %s/%s\n", wd, config.FileName)
			fmt.Printf("  workspace_id: %s\n", cfg.WorkspaceID)
			fmt.Printf("  project_slug: %s\n", cfg.ProjectSlug)
			fmt.Printf("  alias: %s\n", cfg.Alias)
			if cfg.Role != "" {
				fmt.Printf("  role: %s\n", cfg.Role)
			}
			fmt.Println()
			fmt.Println("Use --update to update hostname/workspace_path on the server.")
			fmt.Println("Use --inject-docs to inject bdh instructions into CLAUDE.md/AGENTS.md.")
			return nil
		}

		// --update flag: re-register to update hostname/workspace_path on server
		fmt.Println("Updating workspace registration...")

		// Get current hostname and workspace path
		hostname, _ := os.Hostname()
		workspacePath, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not determine workspace path: %v\n", err)
			workspacePath = ""
		}

		// Determine role: use --role flag if provided, otherwise keep existing
		role := cfg.Role
		if initRole != "" {
			role = config.NormalizeRole(initRole)
			if !config.IsValidRole(role) {
				return fmt.Errorf("invalid role: use 1-2 words (letters/numbers) with hyphens/underscores allowed; max 50 chars")
			}
		}

		repoOrigin := currentRepoOriginBestEffort(cfg)
		if strings.TrimSpace(repoOrigin) == "" {
			repoOrigin = cfg.RepoOrigin
		}

		c, err := newBeadHubClientRequired(cfg.BeadhubURL)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
		defer cancel()
		workspaceResp, err := c.RegisterWorkspace(ctx, &client.RegisterWorkspaceRequest{
			RepoOrigin:    repoOrigin,
			Role:          role,
			Hostname:      hostname,
			WorkspacePath: workspacePath,
		})
		if err != nil {
			return fmt.Errorf("failed to update workspace registration: %w", err)
		}
		if strings.TrimSpace(workspaceResp.WorkspaceID) != "" && workspaceResp.WorkspaceID != cfg.WorkspaceID {
			return fmt.Errorf(
				"account/workspace mismatch: selected account agent_id=%q but .beadhub workspace_id=%q (check .aw/context)",
				workspaceResp.WorkspaceID,
				cfg.WorkspaceID,
			)
		}

		// Update local config if role changed
		if role != cfg.Role {
			cfg.Role = role
			if err := cfg.Save(); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}
		}

		fmt.Println()
		fmt.Println("Workspace registration updated")
		fmt.Printf("  workspace_id: %s\n", cfg.WorkspaceID)
		fmt.Printf("  alias: %s\n", cfg.Alias)
		fmt.Printf("  hostname: %s\n", hostname)
		fmt.Printf("  workspace_path: %s\n", workspacePath)
		if role != "" {
			fmt.Printf("  role: %s\n", role)
		}
		return nil
	}

	// Check if beads needs to be initialized (will run at end)
	needsBeadsInit := false
	if _, err := os.Stat(beads.DatabasePath()); os.IsNotExist(err) {
		needsBeadsInit = true
	}

	// Branch based on API key existence:
	// - Always use /v1/init endpoint (gets API key + creates all resources)
	return runInitWithNewEndpoint(needsBeadsInit)
}

// resolveConfig returns value with priority: CLI flag > env var > default.
func resolveConfig(cliFlag, envVar, defaultValue string) string {
	if cliFlag != "" {
		return cliFlag
	}
	if val := os.Getenv(envVar); val != "" {
		return val
	}
	return defaultValue
}

// getDefaultHumanName returns the default human name from $USER or "developer".
func getDefaultHumanName() string {
	if user := os.Getenv("USER"); user != "" {
		return user
	}
	return "developer"
}

// promptForRole prompts the user interactively for the workspace role.
func promptForRole() (string, error) {
	reader := bufio.NewReader(os.Stdin)

	defaultRole := "agent"
	for {
		fmt.Printf("Workspace role [%s]: ", defaultRole)
		input, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}

		input = strings.TrimSpace(input)
		if input == "" {
			return defaultRole, nil
		}

		normalized := config.NormalizeRole(input)
		if !config.IsValidRole(normalized) {
			fmt.Println("Invalid role. Use 1-2 words (letters/numbers) with hyphens/underscores allowed; max 50 chars.")
			continue
		}
		return normalized, nil
	}
}

// promptForAlias prompts the user interactively for the workspace alias.
func promptForAlias(suggested string) (string, error) {
	reader := bufio.NewReader(os.Stdin)

	fmt.Printf("Workspace alias [%s]: ", suggested)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return suggested, nil
	}
	return strings.ToLower(input), nil
}

func suggestAliasForRepo(beadhubURL, repoOrigin, role, apiKey string) (string, error) {
	var c *client.Client
	if strings.TrimSpace(apiKey) != "" {
		c = client.NewWithAPIKey(beadhubURL, apiKey)
	} else {
		c = client.New(beadhubURL)
	}
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	resp, err := c.SuggestNamePrefix(ctx, &client.SuggestNamePrefixRequest{OriginURL: repoOrigin})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(resp.NamePrefix) == "" {
		return "", fmt.Errorf("server returned empty name_prefix")
	}
	if strings.TrimSpace(role) == "" {
		return resp.NamePrefix, nil
	}
	return fmt.Sprintf("%s-%s", resp.NamePrefix, config.RoleToAliasPrefix(role)), nil
}

// promptForProjectSlug prompts the user interactively for the project slug.
func promptForProjectSlug() (string, error) {
	reader := bufio.NewReader(os.Stdin)

	// Suggest sanitized directory name as default
	wd, _ := os.Getwd()
	suggested := config.SanitizeSlug(filepath.Base(wd))

	fmt.Printf("Project slug [%s]: ", suggested)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return suggested, nil
	}
	// Sanitize user input as well
	return config.SanitizeSlug(input), nil
}

// getGitOrigin returns the git remote origin URL.
func getGitOrigin() (string, error) {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// addToGitignore adds an entry to .gitignore if not already present.
func addToGitignore(entry string) error {
	gitignorePath := ".gitignore"

	// Check if entry already exists
	if content, err := os.ReadFile(gitignorePath); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(content)))
		for scanner.Scan() {
			if strings.TrimSpace(scanner.Text()) == entry {
				return nil // Already present
			}
		}
		// Append to existing file
		f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		// Add newline if file doesn't end with one
		if len(content) > 0 && content[len(content)-1] != '\n' {
			if _, err := f.WriteString("\n"); err != nil {
				return err
			}
		}
		_, err = f.WriteString(entry + "\n")
		return err
	}

	// Create new .gitignore
	return os.WriteFile(gitignorePath, []byte(entry+"\n"), 0644)
}

// runInitWithNewEndpoint implements the new init flow using POST /v1/init.
// This atomically creates project, repo, workspace, and API key in one call.
func runInitWithNewEndpoint(needsBeadsInit bool) error {
	// Get git remote origin
	repoOrigin := os.Getenv("BEADHUB_REPO_ORIGIN")
	if repoOrigin == "" {
		var err error
		repoOrigin, err = getGitOrigin()
		if err != nil {
			return fmt.Errorf("failed to get git remote origin: %w\n(are you in a git repository?)", err)
		}
	}

	// Get configuration with priority: CLI flag > env var > default
	beadhubURL := resolveConfig(initURL, "BEADHUB_URL", "http://localhost:8000")
	humanName := resolveConfig(initHuman, "BEADHUB_HUMAN", getDefaultHumanName())
	email := os.Getenv("BEADHUB_EMAIL") // For Cloud (ignored by OSS)

	// Get role with priority: CLI flag > env var > prompt (TTY) > default
	role := ""
	if initRole != "" {
		role = initRole
	} else if envRole := os.Getenv("BEADHUB_ROLE"); envRole != "" {
		role = envRole
	}

	if role != "" {
		role = config.NormalizeRole(role)
		if !config.IsValidRole(role) {
			return fmt.Errorf("invalid role: use 1-2 words (letters/numbers) with hyphens/underscores allowed; max 50 chars")
		}
	} else if isTTY() {
		var err error
		role, err = promptForRole()
		if err != nil {
			return fmt.Errorf("getting role: %w", err)
		}
	} else {
		role = "agent"
	}

	// Get alias with priority: CLI flag > env var > prompt (TTY) > default
	aliasFromFlag := initAlias != ""
	aliasFromEnv := os.Getenv("BEADHUB_ALIAS") != ""
	alias := resolveConfig(initAlias, "BEADHUB_ALIAS", "")
	aliasIsDefaultSuggestion := false
	if alias == "" {
		suggestedAlias := fmt.Sprintf("alice-%s", config.RoleToAliasPrefix(role))
		if serverSuggested, err := suggestAliasForRepo(beadhubURL, repoOrigin, role, apiKeyFromEnv()); err == nil {
			suggestedAlias = serverSuggested
		} else {
			var clientErr *client.Error
			if errors.As(err, &clientErr) && clientErr.StatusCode != 404 {
				return fmt.Errorf("failed to get alias suggestion: %w", err)
			}
		}
		if isTTY() {
			var err error
			alias, err = promptForAlias(suggestedAlias)
			if err != nil {
				return fmt.Errorf("getting alias: %w", err)
			}
			aliasIsDefaultSuggestion = alias == suggestedAlias
		} else {
			alias = suggestedAlias
			aliasIsDefaultSuggestion = true
		}
	}

	// Get hostname and workspace path
	hostname, _ := os.Hostname()
	workspacePath, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not determine workspace path: %v\n", err)
		workspacePath = ""
	}

	stringPtr := func(s string) *string { return &s }

	// Build init request
	initReq := &client.InitRequest{
		RepoOrigin:    repoOrigin,
		Alias:         stringPtr(alias),
		HumanName:     humanName,
		Role:          role,
		Email:         email,
		Hostname:      hostname,
		WorkspacePath: workspacePath,
	}

	// Get project slug if provided via flag/env
	projectSlug := resolveConfig(initProject, "BEADHUB_PROJECT", "")
	if projectSlug != "" {
		initReq.ProjectSlug = projectSlug
	}

	c := client.New(beadhubURL)

	fmt.Println("Initializing workspace...")

	// Call POST /v1/init
	initResp, err := c.Init(context.Background(), initReq)
	if err != nil {
		// Check for specific error codes
		if clientErr, ok := err.(*client.Error); ok {
			// Parse error body for code
			if strings.Contains(clientErr.Body, "project_not_found") {
				// Repo not registered, need project_slug
				fmt.Println("Repo not registered. Creating new project...")
				if projectSlug == "" {
					if isTTY() {
						projectSlug, err = promptForProjectSlug()
						if err != nil {
							return fmt.Errorf("getting project slug: %w", err)
						}
					} else {
						// Non-TTY: use sanitized directory name
						wd, _ := os.Getwd()
						projectSlug = config.SanitizeSlug(filepath.Base(wd))
						if projectSlug == "" {
							return fmt.Errorf("repo not registered and directory name cannot be converted to valid slug. Use --project or BEADHUB_PROJECT")
						}
						fmt.Printf("Using sanitized directory name as project slug: %s\n", projectSlug)
					}
				}

				// Confirm project creation
				if isTTY() {
					fmt.Printf("Create project '%s'? (y/n): ", projectSlug)
					reader := bufio.NewReader(os.Stdin)
					confirm, _ := reader.ReadString('\n')
					confirm = strings.TrimSpace(strings.ToLower(confirm))
					if confirm != "y" && confirm != "yes" {
						return fmt.Errorf("project creation cancelled")
					}
				}

				// Retry with project_slug
				initReq.ProjectSlug = projectSlug
				initResp, err = c.Init(context.Background(), initReq)
				if err != nil {
					return fmt.Errorf("failed to initialize workspace: %w", err)
				}
			} else if strings.Contains(clientErr.Body, "alias_exists") {
				aliasExplicit := aliasFromFlag || aliasFromEnv || !aliasIsDefaultSuggestion
				if !aliasExplicit {
					fmt.Printf("Default alias '%s' is already taken; asking server to assign the next available name...\n", alias)
					initReq.Alias = nil
					initResp, err = c.Init(context.Background(), initReq)
					if err != nil {
						return fmt.Errorf("failed to initialize workspace: %w", err)
					}
				} else {
					return fmt.Errorf("alias '%s' is already taken. Use --alias to specify a different one", alias)
				}
			} else if strings.Contains(clientErr.Body, "pending_validation") {
				// Cloud: email validation pending
				fmt.Println("\nEmail validation required.")
				fmt.Println("Check your email and click the validation link, then run 'bdh :init' again.")
				return nil
			} else {
				return fmt.Errorf("failed to initialize workspace: %w", err)
			}
		} else {
			return fmt.Errorf("could not reach BeadHub at %s: %w", beadhubURL, err)
		}
	}

	// Validate API key format before saving
	if !strings.HasPrefix(initResp.APIKey, "aw_sk_") || len(initResp.APIKey) < 38 {
		return fmt.Errorf("server returned malformed API key")
	}

	if initResp.Alias == "" {
		return fmt.Errorf("server did not return alias")
	}

	// Create config object (validation only, no file write yet)
	cfg := &config.Config{
		WorkspaceID:     initResp.WorkspaceID,
		BeadhubURL:      beadhubURL,
		ProjectSlug:     initResp.ProjectSlug,
		RepoID:          initResp.RepoID,
		RepoOrigin:      repoOrigin,
		CanonicalOrigin: initResp.CanonicalOrigin,
		Alias:           initResp.Alias,
		HumanName:       humanName,
		Role:            role,
	}

	// Validate config before any writes
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// Track what we've written for cleanup on failure
	configWritten := false
	contextWritten := false
	contextPath := ""

	// Cleanup function for rollback on failure
	cleanup := func() {
		if configWritten {
			_ = os.Remove(config.FileName)
		}
		if contextWritten {
			_ = os.Remove(contextPath)
		}
	}

	// Save config
	if err := cfg.Save(); err != nil {
		cleanup()
		return fmt.Errorf("failed to save config: %w", err)
	}
	configWritten = true

	// Add .beadhub to .gitignore (non-fatal, but cleanup on total failure)
	if err := addToGitignore(config.FileName); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not update .gitignore: %v\n", err)
	}

	// Persist a beadhub account (API key) globally and create worktree-local context.
	accountName, serverName, err := persistBeadhubAccountAndContext(beadhubURL, cfg.ProjectSlug, cfg.Alias, initResp.APIKey, cfg.WorkspaceID)
	if err != nil {
		cleanup()
		return fmt.Errorf("failed to persist account/context: %w", err)
	}
	if root, err := config.WorkspaceRoot(); err == nil {
		contextPath = filepath.Join(root, ".aw", "context")
		contextWritten = true
	}
	_ = addToGitignore(".aw/")

	// Print success
	fmt.Println()
	fmt.Println("Initialized BeadHub workspace")
	fmt.Printf("  workspace_id: %s\n", cfg.WorkspaceID)
	fmt.Printf("  beadhub_url: %s\n", cfg.BeadhubURL)
	fmt.Printf("  project_slug: %s\n", cfg.ProjectSlug)
	fmt.Printf("  repo_id: %s\n", cfg.RepoID)
	fmt.Printf("  canonical_origin: %s\n", cfg.CanonicalOrigin)
	fmt.Printf("  alias: %s\n", cfg.Alias)
	fmt.Printf("  role: %s\n", cfg.Role)
	if initResp.WorkspaceCreated {
		fmt.Println("  (new workspace registered)")
	}
	fmt.Printf("  human_name: %s\n", cfg.HumanName)
	fmt.Printf("  account: %s (server: %s)\n", accountName, serverName)
	fmt.Println()
	fmt.Printf("Created %s\n", config.FileName)
	fmt.Println()
	fmt.Println("Dashboard:")
	fmt.Println("  - Open and auto-authenticate: `bdh :dashboard`")
	fmt.Println("  - Uses the selected account from .aw/context (or BEADHUB_API_KEY override)")

	// Inject bdh instructions into CLAUDE.md/AGENTS.md
	wd, _ := os.Getwd()
	if agentDocsResult, err := InjectAgentDocs(wd); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to inject agent docs: %v\n", err)
	} else {
		PrintAgentDocsResult(agentDocsResult)
	}

	// Run bd init if beads database doesn't exist
	if needsBeadsInit {
		runBeadsInit(initResp.APIKey)
	}

	// Inject PRIME.md override
	primeResult := InjectPrimeOverride(wd)
	PrintPrimeOverrideResult(primeResult)

	return nil
}

// runBeadsInit attempts to initialize beads issue tracking.
// Provides appropriate error messages based on whether bd is installed.
func runBeadsInit(apiKey string) {
	fmt.Println()

	// Check if bd is installed
	if _, err := exec.LookPath("bd"); err != nil {
		fmt.Println("Beads (bd) not found in PATH.")
		fmt.Println("Install beads for issue tracking: https://github.com/steveyegge/beads")
		fmt.Println("Then run 'bd init' in this directory.")
		return
	}

	fmt.Println("Initializing beads issue tracking...")
	cmd := exec.Command("bd", "init")
	cmd.Env = append(os.Environ(), "BEADHUB_API_KEY="+apiKey)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Println()
		fmt.Println("Note: 'bd init' failed. You may need to run 'bd doctor --fix'")
	} else {
		fmt.Println()
		fmt.Println("For multi-agent setups, add to .beads/config.yaml:")
		fmt.Println("  no-daemon: true       # agents sync manually")
		fmt.Println("  sync-branch: beads-sync  # shared sync branch")
	}
}
