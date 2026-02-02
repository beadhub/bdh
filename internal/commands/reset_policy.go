package commands

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/beadhub/bdh/internal/config"
)

var resetPolicyForce bool

var resetPolicyCmd = &cobra.Command{
	Use:   ":reset-policy",
	Short: "Reset project policy to defaults",
	Long: `Reset the project's policy to the current default policy bundle.

This creates a new policy version from the server's default bundle and
activates it. The old policy versions are preserved in history.

Requires --force to confirm.

Example:
  bdh :reset-policy --force`,
	RunE: runResetPolicy,
}

func init() {
	resetPolicyCmd.Flags().BoolVar(&resetPolicyForce, "force", false, "Confirm reset (required)")
}

func runResetPolicy(cmd *cobra.Command, args []string) error {
	if !resetPolicyForce {
		return fmt.Errorf("this will reset the project policy to defaults - use --force to confirm")
	}

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

	c, err := newBeadHubClientRequired(cfg.BeadhubURL)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := c.ResetPolicy(ctx)
	if err != nil {
		return fmt.Errorf("resetting policy: %w", err)
	}

	fmt.Printf("✓ Policy reset to defaults (version %d)\n", resp.Version)
	return nil
}
