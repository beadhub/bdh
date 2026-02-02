package commands

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/beadhub/bdh/internal/client"
	"github.com/beadhub/bdh/internal/config"
)

var escalateJSON bool

var escalateCmd = &cobra.Command{
	Use:   ":escalate <subject> <situation>",
	Short: "Escalate to human when stuck",
	Long: `Escalate an issue to a human for review.

Use this when you're blocked and cannot resolve the issue with other agents.
A human will review the escalation and respond.

Examples:
  bdh :escalate "Blocked on bd-42" "other-agent has had bd-42 for 3 hours"
  bdh :escalate "Need clarification" "Requirements unclear for feature X" --json`,
	Args: cobra.ExactArgs(2),
	RunE: runEscalate,
}

func init() {
	escalateCmd.Flags().BoolVar(&escalateJSON, "json", false, "Output as JSON")
}

// EscalateResult contains the result of creating an escalation.
type EscalateResult struct {
	EscalationID string
	Status       string
	CreatedAt    string
	ExpiresAt    string
}

func runEscalate(cmd *cobra.Command, args []string) error {
	subject := args[0]
	situation := args[1]

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

	result, err := createEscalationWithConfig(cfg, subject, situation)
	if err != nil {
		return err
	}

	output := formatEscalateOutput(result, escalateJSON)
	fmt.Print(output)
	return nil
}

// createEscalationWithConfig creates an escalation using the provided config (for testing).
func createEscalationWithConfig(cfg *config.Config, subject, situation string) (*EscalateResult, error) {
	if subject == "" {
		return nil, fmt.Errorf("subject cannot be empty")
	}
	if situation == "" {
		return nil, fmt.Errorf("situation cannot be empty")
	}

	c, err := newBeadHubClientRequired(cfg.BeadhubURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	resp, err := c.Escalate(ctx, &client.EscalateRequest{
		WorkspaceID: cfg.WorkspaceID,
		Alias:       cfg.Alias,
		Subject:     subject,
		Situation:   situation,
	})
	if err != nil {
		var clientErr *client.Error
		if errors.As(err, &clientErr) {
			return nil, fmt.Errorf("BeadHub error (%d): %s", clientErr.StatusCode, clientErr.Body)
		}
		return nil, fmt.Errorf("failed to create escalation: %w", err)
	}

	return &EscalateResult{
		EscalationID: resp.EscalationID,
		Status:       resp.Status,
		CreatedAt:    resp.CreatedAt,
		ExpiresAt:    resp.ExpiresAt,
	}, nil
}

// formatEscalateOutput formats the escalation result for display.
func formatEscalateOutput(result *EscalateResult, asJSON bool) string {
	if asJSON {
		output := struct {
			EscalationID string `json:"escalation_id"`
			Status       string `json:"status"`
			CreatedAt    string `json:"created_at"`
			ExpiresAt    string `json:"expires_at,omitempty"`
		}{
			EscalationID: result.EscalationID,
			Status:       result.Status,
			CreatedAt:    result.CreatedAt,
			ExpiresAt:    result.ExpiresAt,
		}
		return marshalJSONOrFallback(output)
	}

	return fmt.Sprintf("Escalation created: %s\nA human will review and respond.\n", result.EscalationID)
}
