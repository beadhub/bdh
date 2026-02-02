// Package commands implements the bdh CLI commands.
package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"

	"github.com/beadhub/bdh/internal/config"
)

var versionInfo struct {
	version string
	commit  string
	date    string
}

// SetVersionInfo sets version information from main (populated by goreleaser).
func SetVersionInfo(version, commit, date string) {
	versionInfo.version = version
	versionInfo.commit = commit
	versionInfo.date = date
}

var rootCmd = &cobra.Command{
	Use:   "bdh",
	Short: "bd wrapper with agent coordination tools",
	Long: `bdh wraps bd with tools for coordination between agents:

1. Notifies BeadHub what you're doing (for visibility)
2. Runs bd with all provided arguments
3. Syncs .beads/issues.jsonl to BeadHub after mutation commands

bd always runs, even if the server is down. The one exception: claiming
a bead another agent has (use --:jump-in to override).

Commands:
  bdh <bd-command>      - Run bd command with coordination
  bdh :aweb <command>   - aweb protocol commands (mail/chat/locks)
  bdh :<command>        - BeadHub-specific utilities (see below)

Setup:
  bdh init              - Initialize beads (passthrough to bd)
  bdh :init             - Register workspace with BeadHub

Global flags:
  -h, --help               - Show bdh help + bd help
  --:help                  - Show only bdh help (not bd)
  --:local-config <path>   - Use an alternate .beadhub config file

Environment variables (for bdh :init):
  BEADHUB_URL          - Server URL (default: http://localhost:8000)
  BEADHUB_PROJECT      - Project slug (required if repo not registered)
  BEADHUB_ALIAS        - Workspace alias (default: auto-suggested)
  BEADHUB_ROLE         - Workspace role (default: agent)
  BEADHUB_HUMAN        - Human name (default: $USER)
  BEADHUB_REPO_ORIGIN  - Override git remote origin (testing only)`,
	// Don't show usage/errors on errors from subcommands (main.go handles errors)
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	// Disable cobra's auto-generated commands - they pollute the namespace
	rootCmd.CompletionOptions.DisableDefaultCmd = true
	rootCmd.SetHelpCommand(&cobra.Command{Use: "no-op", Hidden: true, Run: func(*cobra.Command, []string) {}})

	// Add subcommands (all :* commands)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(nextAliasPrefixCmd)
	rootCmd.AddCommand(escalateCmd)
	rootCmd.AddCommand(awebCmd)
	rootCmd.AddCommand(dashboardCmd)
	rootCmd.AddCommand(policyCmd)
	rootCmd.AddCommand(resetPolicyCmd)
	rootCmd.AddCommand(projectsCmd)
	rootCmd.AddCommand(addWorktreeCmd)
}

func loadDotenvBestEffort() {
	// Prefer the workspace root (dir containing .beadhub) so subdir invocations work.
	if root, err := config.WorkspaceRoot(); err == nil {
		_ = godotenv.Load(filepath.Join(root, ".env"))
		return
	}
	// Fallback: load from the current working directory.
	_ = godotenv.Load()
}

// Execute runs the root command.
// Routing rules:
//   - :* commands      → bdh handles (cobra subcommands)
//   - -h, --help       → bdh help, then bd --help
//   - -V, --version    → bdh version, then bd --version
//   - everything else  → passthrough to bd
func Execute() error {
	// Parse --:local-config globally (affects all commands)
	if len(os.Args) > 1 {
		cleanedArgs, configPath, hasLocalConfig := parseLocalConfig(os.Args[1:])
		if hasLocalConfig && configPath != "" {
			config.SetPath(configPath)
			defer config.SetPath("") // Reset after command completes
		}
		// Update os.Args with cleaned version (removes --:local-config)
		os.Args = append([]string{os.Args[0]}, cleanedArgs...)
	}

	loadDotenvBestEffort()

	if len(os.Args) <= 1 {
		// No args - show help
		return rootCmd.Execute()
	}

	firstArg := os.Args[1]

	// :* commands are handled by cobra
	if strings.HasPrefix(firstArg, ":") {
		return rootCmd.Execute()
	}

	// --:help: show only bdh help (no bd help)
	if firstArg == "--:help" {
		_ = rootCmd.Help()
		fmt.Println("\nAll bd commands are also available (e.g., bdh ready, bdh create, bdh close).")
		fmt.Println("Run 'bdh --help' to see the full list.")
		return nil
	}

	// Help: show bdh help, then bd help
	if firstArg == "-h" || firstArg == "--help" {
		_ = rootCmd.Help()
		fmt.Println("\n--- bd commands (all available via bdh) ---")
		fmt.Println()
		return executePassthrough([]string{"--help"})
	}

	// Version: show bdh version, then bd version
	if firstArg == "-V" || firstArg == "--version" {
		fmt.Printf("bdh %s\n", versionInfo.version)
		if versionInfo.commit != "" && versionInfo.commit != "none" {
			fmt.Printf("  commit: %s\n", versionInfo.commit)
		}
		if versionInfo.date != "" && versionInfo.date != "unknown" {
			fmt.Printf("  built:  %s\n", versionInfo.date)
		}
		fmt.Println()
		return executePassthrough([]string{"--version"})
	}

	// Everything else passes through to bd
	return executePassthrough(os.Args[1:])
}

// executePassthrough runs a bd command with coordination.
func executePassthrough(args []string) error {
	result, err := runPassthrough(args)
	if err != nil {
		return err
	}

	// Print formatted output (notifications are printed by main.go)
	output := formatPassthroughOutput(result)
	fmt.Print(output)

	// Exit with non-zero code if rejected (bd was not run)
	if result.Rejected {
		os.Exit(1)
	}

	// Exit with bd's exit code
	if result.ExitCode != 0 {
		os.Exit(result.ExitCode)
	}

	return nil
}
