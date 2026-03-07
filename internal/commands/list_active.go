package commands

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/beadhub/bdh/internal/client"
)

var (
	listActiveRepo     string
	listActiveAssignee string
	listActiveJSON     bool
)

var listActiveCmd = &cobra.Command{
	Use:   ":list-active",
	Short: "Show in-progress beads across all repos",
	Long: `Show all in_progress beads across the project, grouped by repo.

This provides cross-repo visibility into what's actively being worked on.

Examples:
  bdh :list-active                          # All in-progress beads
  bdh :list-active --repo=github.com/o/r    # Filter to specific repo
  bdh :list-active --assignee=alice         # Filter by assignee
  bdh :list-active --json                   # Output as JSON`,
	RunE: runListActive,
}

func init() {
	listActiveCmd.Flags().StringVar(&listActiveRepo, "repo", "", "Filter by repo (canonical origin)")
	listActiveCmd.Flags().StringVar(&listActiveAssignee, "assignee", "", "Filter by assignee")
	listActiveCmd.Flags().BoolVar(&listActiveJSON, "json", false, "Output as JSON")
}

// ListActiveIssue represents a bead issue for the list-active display.
type ListActiveIssue struct {
	BeadID   string `json:"bead_id"`
	Repo     string `json:"repo"`
	Branch   string `json:"branch"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Priority int    `json:"priority"`
	Type     string `json:"type"`
	Assignee string `json:"assignee,omitempty"`
}

// ListActiveResult contains the result of the list-active command.
type ListActiveResult struct {
	Issues []ListActiveIssue `json:"issues"`
}

func runListActive(cmd *cobra.Command, args []string) error {
	cfg, err := loadAndValidateConfig()
	if err != nil {
		return err
	}

	result, err := fetchListActive(cfg.BeadhubURL)
	if err != nil {
		return err
	}

	output := formatListActiveOutput(result, listActiveJSON)
	fmt.Print(output)
	return nil
}

func fetchListActive(beadhubURL string) (*ListActiveResult, error) {
	c, err := newBeadHubClientRequired(beadhubURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	resp, err := c.BeadsIssues(ctx, &client.BeadsIssuesRequest{
		Status:   "in_progress",
		Repo:     listActiveRepo,
		Assignee: listActiveAssignee,
		Limit:    200,
	})
	if err != nil {
		var clientErr *client.Error
		if errors.As(err, &clientErr) {
			return nil, formatClientErr(err)
		}
		return nil, fmt.Errorf("failed to fetch active beads: %w", err)
	}

	issues := make([]ListActiveIssue, 0, len(resp.Issues))
	for _, issue := range resp.Issues {
		issues = append(issues, ListActiveIssue{
			BeadID:   issue.BeadID,
			Repo:     issue.Repo,
			Branch:   issue.Branch,
			Title:    issue.Title,
			Status:   issue.Status,
			Priority: issue.Priority,
			Type:     issue.Type,
			Assignee: issue.Assignee,
		})
	}

	return &ListActiveResult{Issues: issues}, nil
}

func formatListActiveOutput(result *ListActiveResult, asJSON bool) string {
	if asJSON {
		return marshalJSONOrFallback(result)
	}

	if len(result.Issues) == 0 {
		return "No active beads across the project.\n"
	}

	// Group by repo
	type repoGroup struct {
		repo   string
		issues []ListActiveIssue
	}
	repoMap := map[string]*repoGroup{}
	for _, issue := range result.Issues {
		g, ok := repoMap[issue.Repo]
		if !ok {
			g = &repoGroup{repo: issue.Repo}
			repoMap[issue.Repo] = g
		}
		g.issues = append(g.issues, issue)
	}

	// Sort repos alphabetically
	repos := make([]string, 0, len(repoMap))
	for repo := range repoMap {
		repos = append(repos, repo)
	}
	sort.Strings(repos)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Active beads across project (%d):\n", len(result.Issues)))

	for _, repo := range repos {
		g := repoMap[repo]
		sb.WriteString(fmt.Sprintf("\n## %s (%d)\n", g.repo, len(g.issues)))
		for _, issue := range g.issues {
			icon := priorityIcon(issue.Priority)
			line := fmt.Sprintf("  %s %s [P%d] [%s] %s",
				icon, issue.BeadID, issue.Priority, issue.Type, issue.Title)
			if issue.Assignee != "" {
				line += fmt.Sprintf("  (%s)", issue.Assignee)
			}
			if issue.Branch != "" && issue.Branch != "main" && issue.Branch != "master" {
				line += fmt.Sprintf("  [%s]", issue.Branch)
			}
			sb.WriteString(line + "\n")
		}
	}

	return sb.String()
}
