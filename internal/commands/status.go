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

var statusJSON bool

var statusCmd = &cobra.Command{
	Use:   ":status",
	Short: "Show coordination status",
	Long: `Show coordination status for this workspace.

Displays:
  - Your workspace information
  - Other active agents and what they're working on
  - Pending escalations count

Examples:
  bdh :status           # Show status
  bdh :status --json    # Output as JSON`,
	RunE: runStatus,
}

func init() {
	statusCmd.Flags().BoolVar(&statusJSON, "json", false, "Output as JSON")
}

// AgentInfo contains information about an agent.
type AgentInfo struct {
	Alias        string   `json:"alias"`
	Member       string   `json:"member,omitempty"`
	Program      string   `json:"program,omitempty"`
	Role         string   `json:"role,omitempty"`
	Status       string   `json:"status"`
	CurrentLocks []string `json:"current_locks,omitempty"`
	CurrentIssue string   `json:"current_issue,omitempty"`
	LastSeen     string   `json:"last_seen"`
}

// StatusResult contains the result of the status command.
type StatusResult struct {
	Agents             []AgentInfo
	YourLocks          []string
	EscalationsPending int
}

func runStatus(cmd *cobra.Command, args []string) error {
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

	// Set up coordination header for this agent
	SetCoordinationHeaderAlias(cfg.Alias)

	result, err := fetchStatusWithConfig(cfg)
	if err != nil {
		return err
	}

	output := formatStatusOutput(result, cfg, statusJSON)
	fmt.Print(output)
	return nil
}

// fetchStatusWithConfig fetches status information using the provided config (for testing).
func fetchStatusWithConfig(cfg *config.Config) (*StatusResult, error) {
	c, err := newBeadHubClientRequired(cfg.BeadhubURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	resp, err := c.Status(ctx, &client.StatusRequest{})
	if err != nil {
		var clientErr *client.Error
		if errors.As(err, &clientErr) {
			return nil, fmt.Errorf("BeadHub error (%d): %s", clientErr.StatusCode, clientErr.Body)
		}
		return nil, fmt.Errorf("failed to fetch status: %w", err)
	}

	result := &StatusResult{
		Agents:             make([]AgentInfo, 0, len(resp.Agents)),
		EscalationsPending: resp.EscalationsPending,
	}

	for _, agent := range resp.Agents {
		result.Agents = append(result.Agents, AgentInfo{
			Alias:        agent.Alias,
			Member:       agent.Member,
			Program:      agent.Program,
			Role:         agent.Role,
			Status:       agent.Status,
			CurrentLocks: agent.CurrentLocks,
			CurrentIssue: agent.CurrentIssue,
			LastSeen:     agent.LastSeen,
		})
	}

	// Fetch your own locks
	locksResp, err := c.ListLocks(ctx, &client.ListLocksRequest{
		WorkspaceID: cfg.WorkspaceID,
		Alias:       cfg.Alias,
	})
	if err == nil {
		for _, lock := range locksResp.Reservations {
			result.YourLocks = append(result.YourLocks, lock.Path)
		}
	}

	return result, nil
}

// formatStatusOutput formats the status result for display.
func formatStatusOutput(result *StatusResult, cfg *config.Config, asJSON bool) string {
	if asJSON {
		output := struct {
			WorkspaceID        string      `json:"workspace_id"`
			Alias              string      `json:"alias"`
			HumanName          string      `json:"human_name"`
			ProjectSlug        string      `json:"project_slug,omitempty"`
			BeadhubURL         string      `json:"beadhub_url"`
			Agents             []AgentInfo `json:"agents"`
			YourLocks          []string    `json:"your_locks,omitempty"`
			EscalationsPending int         `json:"escalations_pending"`
		}{
			WorkspaceID:        cfg.WorkspaceID,
			Alias:              cfg.Alias,
			HumanName:          cfg.HumanName,
			ProjectSlug:        cfg.ProjectSlug,
			BeadhubURL:         cfg.BeadhubURL,
			Agents:             result.Agents,
			YourLocks:          result.YourLocks,
			EscalationsPending: result.EscalationsPending,
		}
		return marshalJSONOrFallback(output)
	}

	var sb strings.Builder

	// Print coordination header (once, before first section)
	sb.WriteString(FormatCoordinationHeader())

	// Workspace info - this is the agent's identity
	sb.WriteString(fmt.Sprintf("## Your Identity (%s)\n", cfg.Alias))
	sb.WriteString(fmt.Sprintf("- Workspace: %s\n", truncateID(cfg.WorkspaceID)))
	sb.WriteString(fmt.Sprintf("- Alias: %s\n", cfg.Alias))
	sb.WriteString(fmt.Sprintf("- Human: %s\n", cfg.HumanName))
	if cfg.ProjectSlug != "" {
		sb.WriteString(fmt.Sprintf("- Project: %s\n", cfg.ProjectSlug))
	}
	sb.WriteString(fmt.Sprintf("- BeadHub: %s\n", cfg.BeadhubURL))

	// Other agents - for coordination
	if len(result.Agents) == 0 {
		sb.WriteString("\n## Team Status\nNo other agents are currently active.\n")
	} else {
		sb.WriteString("\n## Team Status\n")
		sb.WriteString("Check before claiming work to avoid conflicts:\n")
		for _, agent := range result.Agents {
			timeAgo := formatTimeAgo(agent.LastSeen)
			sb.WriteString(fmt.Sprintf("- %s", agent.Alias))
			if agent.Member != "" {
				sb.WriteString(fmt.Sprintf(" (%s)", agent.Member))
			}
			if agent.Role != "" {
				sb.WriteString(fmt.Sprintf(" — %s", agent.Role))
			}
			sb.WriteString(fmt.Sprintf(" — %s", agent.Status))
			if agent.CurrentIssue != "" {
				sb.WriteString(fmt.Sprintf(" — working on %s", agent.CurrentIssue))
			}
			sb.WriteString(fmt.Sprintf(" — %s\n", timeAgo))
		}
	}

	// Your reservations
	if len(result.YourLocks) > 0 {
		sb.WriteString("\n## Your Reservations\n")
		for _, lock := range result.YourLocks {
			sb.WriteString(fmt.Sprintf("- %s\n", lock))
		}
	}

	// Escalations
	if result.EscalationsPending > 0 {
		sb.WriteString(fmt.Sprintf("\n## Escalations\nYou have %d pending escalations to review.\n", result.EscalationsPending))
	}

	return sb.String()
}

// truncateID truncates a UUID for display.
func truncateID(id string) string {
	if len(id) > 8 {
		return id[:8] + "..."
	}
	return id
}

// detectAndCleanGoneWorkspaces checks for workspaces registered on this hostname
// whose paths no longer exist, and deletes them from the server.
func detectAndCleanGoneWorkspaces(cfg *config.Config) []GoneWorkspace {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		return nil
	}

	c := newBeadHubClient(cfg.BeadhubURL)
	ctx := context.Background()

	includePresence := false
	resp, err := c.Workspaces(ctx, &client.WorkspacesRequest{
		Hostname:        hostname,
		IncludePresence: &includePresence,
	})
	if err != nil {
		return nil
	}

	var goneWorkspaces []GoneWorkspace
	for _, ws := range resp.Workspaces {
		if ws.WorkspacePath == "" {
			continue
		}

		if ws.WorkspaceID == cfg.WorkspaceID {
			continue
		}

		if _, err := os.Stat(ws.WorkspacePath); os.IsNotExist(err) {
			_, deleteErr := c.DeleteWorkspace(ctx, ws.WorkspaceID)
			if deleteErr == nil {
				goneWorkspaces = append(goneWorkspaces, GoneWorkspace{
					WorkspaceID:   ws.WorkspaceID,
					Alias:         ws.Alias,
					WorkspacePath: ws.WorkspacePath,
				})
			}
		}
	}

	return goneWorkspaces
}
