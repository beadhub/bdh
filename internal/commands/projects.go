package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/beadhub/bdh/internal/client"
	"github.com/beadhub/bdh/internal/config"
)

var projectsJSON bool

var projectsCmd = &cobra.Command{
	Use:   ":projects",
	Short: "List and manage projects",
	Long: `List and manage BeadHub projects.

By default, lists all projects. Use subcommands for other operations.

Examples:
  bdh :projects             # List all projects
  bdh :projects list        # List all projects (explicit)
  bdh :projects delete <id> # Delete a project by ID or slug`,
	RunE: runProjectsList,
}

var projectsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all projects",
	Long: `List all projects registered with BeadHub.

Examples:
  bdh :projects list        # List all projects
  bdh :projects list --json # Output as JSON`,
	RunE: runProjectsList,
}

var projectsDeleteConfirm bool

var projectsDeleteCmd = &cobra.Command{
	Use:   "delete <project-id-or-slug>",
	Short: "Delete a project",
	Long: `Delete a project by ID or slug.

DANGER: Project deletion is catastrophic and irreversible!
This will cascade delete ALL repos, workspaces, claims, and messages.

You MUST pass --confirm to proceed. The command will show what will be
deleted and require the flag as an explicit safety gate.

Examples:
  bdh :projects delete my-project           # Shows what would be deleted (dry run)
  bdh :projects delete my-project --confirm # Actually deletes the project`,
	Args: cobra.ExactArgs(1),
	RunE: runProjectsDelete,
}

func init() {
	projectsCmd.Flags().BoolVar(&projectsJSON, "json", false, "Output as JSON")
	projectsListCmd.Flags().BoolVar(&projectsJSON, "json", false, "Output as JSON")
	projectsDeleteCmd.Flags().BoolVar(&projectsDeleteConfirm, "confirm", false, "Confirm destructive deletion (REQUIRED)")

	projectsCmd.AddCommand(projectsListCmd)
	projectsCmd.AddCommand(projectsDeleteCmd)
}

// ProjectsListResult contains the result of listing projects.
type ProjectsListResult struct {
	Projects []client.ProjectSummary
}

func runProjectsList(cmd *cobra.Command, args []string) error {
	result, err := listProjects()
	if err != nil {
		return err
	}

	output := formatProjectsListOutput(result, projectsJSON)
	fmt.Print(output)
	return nil
}

func listProjects() (*ProjectsListResult, error) {
	// Load config to get BeadHub URL
	cfg, err := config.Load()
	beadhubURL := "http://localhost:8000" // default
	if err != nil {
		// Warn about using default URL - user may be hitting wrong server
		fmt.Fprintf(os.Stderr, "Warning: No .beadhub config found, using default URL %s\n", beadhubURL)
		fmt.Fprintf(os.Stderr, "Run 'bdh :init' to configure your workspace.\n\n")
	} else {
		beadhubURL = cfg.BeadhubURL
	}

	c := client.New(beadhubURL)
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	resp, err := c.ListProjects(ctx)
	if err != nil {
		var clientErr *client.Error
		if errors.As(err, &clientErr) {
			return nil, fmt.Errorf("BeadHub error (%d): %s", clientErr.StatusCode, clientErr.Body)
		}
		return nil, fmt.Errorf("failed to list projects: %w", err)
	}

	return &ProjectsListResult{
		Projects: resp.Projects,
	}, nil
}

func formatProjectsListOutput(result *ProjectsListResult, asJSON bool) string {
	if asJSON {
		output := struct {
			Projects []client.ProjectSummary `json:"projects"`
			Count    int                     `json:"count"`
		}{
			Projects: result.Projects,
			Count:    len(result.Projects),
		}
		return marshalJSONOrFallback(output)
	}

	var sb strings.Builder

	if len(result.Projects) == 0 {
		sb.WriteString("No projects found.\n")
		return sb.String()
	}

	sb.WriteString("PROJECTS:\n")
	for _, p := range result.Projects {
		sb.WriteString(fmt.Sprintf("  %s", p.Slug))
		if p.Name != "" && p.Name != p.Slug {
			sb.WriteString(fmt.Sprintf(" (%s)", p.Name))
		}
		sb.WriteString(fmt.Sprintf(" — %d repos, %d workspaces", p.RepoCount, p.WorkspaceCount))
		sb.WriteString(fmt.Sprintf(" — %s\n", p.ID))
	}

	sb.WriteString(fmt.Sprintf("\nTotal: %d project(s)\n", len(result.Projects)))
	return sb.String()
}

func runProjectsDelete(cmd *cobra.Command, args []string) error {
	idOrSlug := args[0]

	// Load config - REQUIRED for destructive operations
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("no .beadhub config found: destructive operations require a configured workspace.\nRun 'bdh :init' to configure your workspace")
	}
	beadhubURL := cfg.BeadhubURL

	c := client.New(beadhubURL)
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	// First, resolve slug to ID and get project details
	var project *client.ProjectSummary
	resp, err := c.ListProjects(ctx)
	if err != nil {
		var clientErr *client.Error
		if errors.As(err, &clientErr) {
			return fmt.Errorf("BeadHub error (%d): %s", clientErr.StatusCode, clientErr.Body)
		}
		return fmt.Errorf("failed to list projects: %w", err)
	}

	for i := range resp.Projects {
		p := &resp.Projects[i]
		if p.Slug == idOrSlug || p.ID == idOrSlug {
			project = p
			break
		}
	}

	if project == nil {
		return fmt.Errorf("project not found: %s", idOrSlug)
	}

	// Show what will be deleted
	fmt.Printf("\n⚠️  DANGER: Project deletion is CATASTROPHIC and IRREVERSIBLE!\n\n")
	fmt.Printf("Project to delete:\n")
	fmt.Printf("  Name:       %s\n", project.Slug)
	fmt.Printf("  ID:         %s\n", project.ID)
	fmt.Printf("  Repos:      %d (will be HARD DELETED)\n", project.RepoCount)
	fmt.Printf("  Workspaces: %d (will be SOFT DELETED)\n", project.WorkspaceCount)
	fmt.Printf("\nThis will also delete all claims, messages, and presence data.\n\n")

	if !projectsDeleteConfirm {
		fmt.Printf("To proceed, re-run with --confirm:\n")
		fmt.Printf("  bdh :projects delete %s --confirm\n\n", idOrSlug)
		return fmt.Errorf("deletion aborted: --confirm flag required")
	}

	// User confirmed, proceed with deletion
	fmt.Printf("Deleting project...\n")

	deleteResp, err := c.DeleteProject(ctx, project.ID)
	if err != nil {
		var clientErr *client.Error
		if errors.As(err, &clientErr) {
			if clientErr.StatusCode == 404 {
				return fmt.Errorf("project not found: %s", idOrSlug)
			}
			return fmt.Errorf("BeadHub error (%d): %s", clientErr.StatusCode, clientErr.Body)
		}
		return fmt.Errorf("failed to delete project: %w", err)
	}

	fmt.Printf("\n✓ Project deleted: %s (ID: %s)\n", project.Slug, deleteResp.ID)
	fmt.Printf("  Repos deleted:      %d\n", deleteResp.ReposDeleted)
	fmt.Printf("  Workspaces deleted: %d\n", deleteResp.WorkspacesDeleted)
	fmt.Printf("  Claims deleted:     %d\n", deleteResp.ClaimsDeleted)
	fmt.Printf("  Presence cleared:   %d\n", deleteResp.PresenceCleared)
	return nil
}
