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

var (
	reservationsMine bool
	reservationsJSON bool
)

var reservationsCmd = &cobra.Command{
	Use:   ":reservations",
	Short: "List active file reservations",
	Long: `List active file reservations in the project.

Shows all reservations by default. Use --mine to show only your reservations.

Examples:
  bdh :reservations           # List all active reservations in the project
  bdh :reservations --mine    # List only your reservations
  bdh :reservations --json    # Output as JSON`,
	RunE: runReservations,
}

func init() {
	reservationsCmd.Flags().BoolVar(&reservationsMine, "mine", false, "Only show your reservations")
	reservationsCmd.Flags().BoolVar(&reservationsJSON, "json", false, "Output as JSON")
}

// ReservationsResult contains the result of listing reservations.
type ReservationsResult struct {
	Reservations []client.LockInfo
	Count        int
	MyAlias      string
	Warning      string
}

func runReservations(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err == nil && cfg.Validate() == nil {
		if err := validateRepoOriginMatchesCurrent(cfg); err != nil {
			return err
		}
	}

	result, err := listReservations()
	if err != nil {
		return err
	}

	output := formatReservationsOutput(result, reservationsJSON)
	fmt.Print(output)
	return nil
}

func listReservations() (*ReservationsResult, error) {
	result := &ReservationsResult{}

	cfg, err := config.Load()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no .beadhub file found - run 'bdh init' first")
		}
		return nil, fmt.Errorf("loading config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid .beadhub config: %w", err)
	}
	if err := validateRepoOriginMatchesCurrent(cfg); err != nil {
		return nil, err
	}

	result.MyAlias = cfg.Alias

	req := &client.ListLocksRequest{
		WorkspaceID: cfg.WorkspaceID,
	}
	if reservationsMine {
		req.Alias = cfg.Alias
	}

	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	c, err := newBeadHubClientRequired(cfg.BeadhubURL)
	if err != nil {
		return nil, err
	}
	resp, err := c.ListLocks(ctx, req)

	if err != nil {
		var clientErr *client.Error
		if errors.As(err, &clientErr) {
			return nil, fmt.Errorf("BeadHub error (%d): %s", clientErr.StatusCode, clientErr.Body)
		}
		result.Warning = fmt.Sprintf("BeadHub unreachable at %s", cfg.BeadhubURL)
		return result, nil
	}

	result.Reservations = resp.Reservations
	result.Count = resp.Count

	return result, nil
}

func formatReservationsOutput(result *ReservationsResult, asJSON bool) string {
	var sb strings.Builder

	if asJSON {
		output := struct {
			Warning      string            `json:"warning,omitempty"`
			Reservations []client.LockInfo `json:"reservations"`
			Count        int               `json:"count"`
		}{
			Warning:      result.Warning,
			Reservations: result.Reservations,
			Count:        result.Count,
		}
		return marshalJSONOrFallback(output)
	}

	if result.Warning != "" {
		sb.WriteString(fmt.Sprintf("Warning: %s\n\n", result.Warning))
		return sb.String()
	}

	if len(result.Reservations) == 0 {
		if reservationsMine {
			sb.WriteString("You have no active reservations.\n")
		} else {
			sb.WriteString("No active reservations in the project.\n")
		}
		return sb.String()
	}

	// Separate own reservations from others'
	var yours, others []client.LockInfo
	for _, r := range result.Reservations {
		if r.Alias == result.MyAlias {
			yours = append(yours, r)
		} else {
			others = append(others, r)
		}
	}

	// Show own reservations first
	if len(yours) > 0 {
		sb.WriteString("## Your Reservations\n")
		sb.WriteString("Files you have locked:\n")
		for _, reservation := range yours {
			expiresIn := formatDuration(reservation.TTLRemainingSeconds)
			sb.WriteString(fmt.Sprintf("- `%s` (expires in %s)", reservation.Path, expiresIn))
			if reservation.Reason != nil && *reservation.Reason != "" {
				sb.WriteString(fmt.Sprintf(" \"%s\"", *reservation.Reason))
			}
			sb.WriteString("\n")
		}
	}

	// Show others' reservations
	if len(others) > 0 {
		sb.WriteString("\n## Other Agents' Reservations\n")
		sb.WriteString("Do not edit these files:\n")
		for _, reservation := range others {
			expiresIn := formatDuration(reservation.TTLRemainingSeconds)
			sb.WriteString(fmt.Sprintf("- `%s` — %s (expires in %s)", reservation.Path, reservation.Alias, expiresIn))
			if reservation.Reason != nil && *reservation.Reason != "" {
				sb.WriteString(fmt.Sprintf(" \"%s\"", *reservation.Reason))
			}
			sb.WriteString("\n")
		}
	}

	return sb.String()
}
