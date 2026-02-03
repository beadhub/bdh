// ABOUTME: Implements the :status command showing identity, team status, and escalations.
// ABOUTME: Combines aweb whoami with beadhub team coordination info.

package commands

import (
	"context"
	"errors"
	"fmt"
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
  - Your workspace identity (from aweb)
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

func runStatus(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("no .beadhub file found - run 'bdh :init' first")
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid .beadhub config: %w", err)
	}

	// Get identity from aweb
	awebClient, err := newAwebClientRequired("")
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	identity, err := awebClient.Introspect(ctx)
	if err != nil {
		return fmt.Errorf("failed to get identity: %w", err)
	}

	// Get team status from beadhub
	bhClient, err := newBeadHubClientRequired(cfg.BeadhubURL)
	if err != nil {
		return err
	}

	statusResp, err := bhClient.Status(ctx, &client.StatusRequest{})
	if err != nil {
		var clientErr *client.Error
		if errors.As(err, &clientErr) {
			return fmt.Errorf("BeadHub error (%d): %s", clientErr.StatusCode, clientErr.Body)
		}
		return fmt.Errorf("failed to fetch status: %w", err)
	}

	if statusJSON {
		output := struct {
			Identity struct {
				ProjectID string `json:"project_id"`
				AgentID   string `json:"agent_id"`
				Alias     string `json:"alias"`
				HumanName string `json:"human_name,omitempty"`
				AgentType string `json:"agent_type,omitempty"`
			} `json:"identity"`
			Agents             []client.StatusAgent `json:"agents"`
			EscalationsPending int                  `json:"escalations_pending"`
		}{
			Identity: struct {
				ProjectID string `json:"project_id"`
				AgentID   string `json:"agent_id"`
				Alias     string `json:"alias"`
				HumanName string `json:"human_name,omitempty"`
				AgentType string `json:"agent_type,omitempty"`
			}{
				ProjectID: identity.ProjectID,
				AgentID:   identity.AgentID,
				Alias:     identity.Alias,
				HumanName: identity.HumanName,
				AgentType: identity.AgentType,
			},
			Agents:             statusResp.Agents,
			EscalationsPending: statusResp.EscalationsPending,
		}
		fmt.Println(marshalJSONOrFallback(output))
		return nil
	}

	// Format text output
	var sb strings.Builder

	// Identity section
	sb.WriteString(fmt.Sprintf("## Your Identity (%s)\n", identity.Alias))
	sb.WriteString(fmt.Sprintf("- Project: %s\n", identity.ProjectID))
	sb.WriteString(fmt.Sprintf("- Agent: %s\n", truncateID(identity.AgentID)))
	sb.WriteString(fmt.Sprintf("- Alias: %s\n", identity.Alias))
	if identity.HumanName != "" {
		sb.WriteString(fmt.Sprintf("- Human: %s\n", identity.HumanName))
	}
	if identity.AgentType != "" {
		sb.WriteString(fmt.Sprintf("- Type: %s\n", identity.AgentType))
	}

	// Team status section
	if len(statusResp.Agents) == 0 {
		sb.WriteString("\n## Team Status\nNo other agents are currently active.\n")
	} else {
		sb.WriteString("\n## Team Status\n")
		for _, agent := range statusResp.Agents {
			// Skip self
			if agent.Alias == identity.Alias {
				continue
			}
			timeAgo := formatTimeAgo(agent.LastSeen)
			sb.WriteString(fmt.Sprintf("- %s", agent.Alias))
			if agent.Member != "" {
				sb.WriteString(fmt.Sprintf(" (%s)", agent.Member))
			}
			if agent.Role != "" {
				sb.WriteString(fmt.Sprintf(" — %s", agent.Role))
			}
			if agent.Status != "" {
				sb.WriteString(fmt.Sprintf(" — %s", agent.Status))
			}
			if agent.CurrentIssue != "" {
				sb.WriteString(fmt.Sprintf(" — working on %s", agent.CurrentIssue))
			}
			sb.WriteString(fmt.Sprintf(" — %s\n", timeAgo))
		}
	}

	// Escalations section
	if statusResp.EscalationsPending > 0 {
		sb.WriteString(fmt.Sprintf("\n## Escalations\nYou have %d pending escalations to review.\n", statusResp.EscalationsPending))
	}

	fmt.Print(sb.String())
	return nil
}

// truncateID truncates a UUID for display.
func truncateID(id string) string {
	if len(id) > 8 {
		return id[:8] + "..."
	}
	return id
}
