package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/beadhub/bdh/internal/client"
	"github.com/beadhub/bdh/internal/config"
)

var (
	nextAliasPrefixJSON bool
	nextAliasPrefixURL  string
)

var nextAliasPrefixCmd = &cobra.Command{
	Use:   ":next-alias-prefix",
	Short: "Get the next available alias name prefix",
	Long: `Get the next available alias name prefix for a new workspace.

This is useful for scripts that need to know the name prefix before creating
a worktree. The prefix is the name part of an alias (e.g., "alice" from
"alice-programmer").

The command queries the BeadHub server for the next available name in the
project associated with the current repository.

Examples:
  bdh :next-alias-prefix           # Output: alice
  bdh :next-alias-prefix --json    # Output as JSON

Use with bdh :add-worktree:
  NAME=$(bdh :next-alias-prefix)
  ALIAS="${NAME}-backend"
  git worktree add "../repo-$ALIAS" -b "$ALIAS"
  (cd "../repo-$ALIAS" && bdh :init --alias "$ALIAS" --role backend)`,
	RunE: runNextAliasPrefix,
}

func init() {
	nextAliasPrefixCmd.Flags().BoolVar(&nextAliasPrefixJSON, "json", false, "Output as JSON")
	nextAliasPrefixCmd.Flags().StringVar(&nextAliasPrefixURL, "beadhub-url", "", "BeadHub server URL (default: from config or http://localhost:8000)")
}

func runNextAliasPrefix(cmd *cobra.Command, args []string) error {
	// Get git remote origin
	repoOrigin := os.Getenv("BEADHUB_REPO_ORIGIN")
	if repoOrigin == "" {
		originCmd := exec.Command("git", "remote", "get-url", "origin")
		output, err := originCmd.Output()
		if err != nil {
			return fmt.Errorf("failed to get git remote origin: %w\n(are you in a git repository?)", err)
		}
		repoOrigin = strings.TrimSpace(string(output))
	}

	// Determine BeadHub URL
	beadhubURL := nextAliasPrefixURL
	if beadhubURL == "" {
		beadhubURL = os.Getenv("BEADHUB_URL")
	}
	if beadhubURL == "" {
		// Try to load from existing config
		if cfg, err := config.Load(); err == nil && cfg.BeadhubURL != "" {
			beadhubURL = cfg.BeadhubURL
		} else {
			beadhubURL = "http://localhost:8000"
		}
	}

	// Call suggest-name-prefix API
	c := client.New(beadhubURL)
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	resp, err := c.SuggestNamePrefix(ctx, &client.SuggestNamePrefixRequest{
		OriginURL: repoOrigin,
	})
	if err != nil {
		var clientErr *client.Error
		if errors.As(err, &clientErr) {
			if clientErr.StatusCode == 404 {
				return fmt.Errorf("repo not registered. Run 'bdh :init' first to register this repository")
			}
			if clientErr.StatusCode == 409 {
				return fmt.Errorf("repo exists in multiple projects. Use --project to specify which one during :init")
			}
			return fmt.Errorf("BeadHub error (%d): %s", clientErr.StatusCode, clientErr.Body)
		}
		return fmt.Errorf("failed to get name prefix suggestion: %w", err)
	}

	if nextAliasPrefixJSON {
		output := struct {
			NamePrefix  string `json:"name_prefix"`
			ProjectSlug string `json:"project_slug"`
		}{
			NamePrefix:  resp.NamePrefix,
			ProjectSlug: resp.ProjectSlug,
		}
		fmt.Println(marshalJSONOrFallback(output))
	} else {
		fmt.Println(resp.NamePrefix)
	}

	return nil
}
