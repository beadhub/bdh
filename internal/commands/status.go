package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/beadhub/bdh/internal/client"
	"github.com/beadhub/bdh/internal/config"
)

var statusJSON bool

var statusCmd = &cobra.Command{
	Use:   ":status",
	Short: "Show coordination status",
	Long: `Show comprehensive coordination status.

Displays:
  - Your identity (alias, role)
  - Your claims and reservations
  - Team members with their claims, reservations, and status
  - Pending escalations

Examples:
  bdh :status           # Show status
  bdh :status --json    # Output as JSON`,
	RunE: runStatus,
}

func init() {
	statusCmd.Flags().BoolVar(&statusJSON, "json", false, "Output as JSON")
}

// ClaimInfo represents a bead claim for display.
type ClaimInfo struct {
	BeadID    string `json:"bead_id"`
	Title     string `json:"title,omitempty"`
	ClaimedAt string `json:"claimed_at"`
}

// LockSummary represents a file reservation held by a workspace.
type LockSummary struct {
	Path                string  `json:"path"`
	TTLRemainingSeconds int     `json:"ttl_remaining_seconds"`
	BeadID              *string `json:"bead_id,omitempty"`
	Reason              *string `json:"reason,omitempty"`
}

// TeamMemberInfo contains information about a team member.
type TeamMemberInfo struct {
	Alias             string        `json:"alias"`
	Role              string        `json:"role,omitempty"`
	Status            string        `json:"status"`
	LastSeen          string        `json:"last_seen"`
	RepoName          string        `json:"repo_name,omitempty"`
	Branch            string        `json:"branch,omitempty"`
	ApexID            string        `json:"apex_id,omitempty"`
	ApexTitle         string        `json:"apex_title,omitempty"`
	ApexType          string        `json:"apex_type,omitempty"`
	Claims            []ClaimInfo   `json:"claims,omitempty"`
	Locks             []LockSummary `json:"locks,omitempty"`
	IsYou             bool          `json:"is_you,omitempty"`
}

// StatusResult contains the result of the status command.
type StatusResult struct {
	Alias              string
	Role               string
	YourClaims         []ClaimInfo
	YourLocks          []LockSummary
	Team               []TeamMemberInfo
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

	result, err := fetchStatusWithConfig(cfg)
	if err != nil {
		return err
	}

	output := formatStatusOutput(result, statusJSON)
	fmt.Print(output)
	return nil
}

// fetchStatusWithConfig fetches status information using the provided config.
func fetchStatusWithConfig(cfg *config.Config) (*StatusResult, error) {
	c, err := newBeadHubClientRequired(cfg.BeadhubURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	result := &StatusResult{
		Alias: cfg.Alias,
		Role:  cfg.Role,
	}

	// Fetch team workspaces with claims
	includeClaims := true
	includePresence := true
	teamResp, err := c.TeamWorkspaces(ctx, &client.TeamWorkspacesRequest{
		IncludeClaims:   &includeClaims,
		IncludePresence: &includePresence,
		Limit:           50,
	})
	if err != nil {
		var clientErr *client.Error
		if errors.As(err, &clientErr) {
			return nil, fmt.Errorf("BeadHub error (%d): %s", clientErr.StatusCode, clientErr.Body)
		}
		return nil, fmt.Errorf("failed to fetch team: %w", err)
	}

	// Fetch all locks
	locksByWorkspace := map[string][]LockSummary{}
	locksResp, err := c.ListLocks(ctx, &client.ListLocksRequest{
		WorkspaceID: cfg.WorkspaceID,
	})
	if err == nil {
		for _, lock := range locksResp.Reservations {
			locksByWorkspace[lock.WorkspaceID] = append(locksByWorkspace[lock.WorkspaceID], LockSummary{
				Path:                lock.Path,
				TTLRemainingSeconds: lock.TTLRemainingSeconds,
				BeadID:              lock.BeadID,
				Reason:              lock.Reason,
			})
		}
		for workspaceID, locks := range locksByWorkspace {
			sort.Slice(locks, func(i, j int) bool {
				return locks[i].Path < locks[j].Path
			})
			locksByWorkspace[workspaceID] = locks
		}
	}

	// Build team list and extract your own claims/locks
	for _, ws := range teamResp.Workspaces {
		claims := make([]ClaimInfo, 0, len(ws.Claims))
		for _, c := range ws.Claims {
			claims = append(claims, ClaimInfo{
				BeadID:    c.BeadID,
				Title:     c.Title,
				ClaimedAt: c.ClaimedAt,
			})
		}

		locks := locksByWorkspace[ws.WorkspaceID]
		isYou := ws.WorkspaceID == cfg.WorkspaceID

		if isYou {
			result.YourClaims = claims
			result.YourLocks = locks
		}

		result.Team = append(result.Team, TeamMemberInfo{
			Alias:     ws.Alias,
			Role:      ws.Role,
			Status:    ws.Status,
			LastSeen:  ws.LastSeen,
			RepoName:  ws.FocusApexRepoName,
			Branch:    ws.FocusApexBranch,
			ApexID:    ws.ApexID,
			ApexTitle: ws.ApexTitle,
			ApexType:  ws.ApexType,
			Claims:    claims,
			Locks:     locks,
			IsYou:     isYou,
		})
	}

	// Fetch escalations count
	statusResp, err := c.Status(ctx, &client.StatusRequest{})
	if err == nil {
		result.EscalationsPending = statusResp.EscalationsPending
	}

	return result, nil
}

// formatStatusOutput formats the status result for display.
func formatStatusOutput(result *StatusResult, asJSON bool) string {
	if asJSON {
		return marshalJSONOrFallback(result)
	}

	var sb strings.Builder

	// Your identity (brief)
	sb.WriteString("## You\n")
	sb.WriteString(fmt.Sprintf("- Alias: %s\n", result.Alias))
	if result.Role != "" {
		sb.WriteString(fmt.Sprintf("- Role: %s\n", result.Role))
	}

	// Your claims
	if len(result.YourClaims) > 0 {
		sb.WriteString("\n## Your Claims\n")
		for _, claim := range result.YourClaims {
			claimAge := formatTimeAgo(claim.ClaimedAt)
			staleIndicator := ""
			if isClaimStale(claim.ClaimedAt) {
				staleIndicator = " ⚠️"
			}
			if claim.Title != "" {
				sb.WriteString(fmt.Sprintf("- %s \"%s\" — %s%s\n", claim.BeadID, claim.Title, claimAge, staleIndicator))
			} else {
				sb.WriteString(fmt.Sprintf("- %s — %s%s\n", claim.BeadID, claimAge, staleIndicator))
			}
		}
	}

	// Your reservations
	if len(result.YourLocks) > 0 {
		sb.WriteString("\n## Your Reservations\n")
		for _, lock := range result.YourLocks {
			expiresIn := formatDuration(lock.TTLRemainingSeconds)
			sb.WriteString(fmt.Sprintf("- %s (expires in %s)\n", lock.Path, expiresIn))
		}
	}

	// Team
	sb.WriteString("\n## Team\n")
	if len(result.Team) == 0 {
		sb.WriteString("No team members found.\n")
	} else {
		for _, member := range result.Team {
			timeAgo := formatTimeAgo(member.LastSeen)
			youIndicator := ""
			if member.IsYou {
				youIndicator = " (you)"
			}

			// Header line: alias — role — status — time
			sb.WriteString(fmt.Sprintf("- **%s**%s", member.Alias, youIndicator))
			if member.Role != "" {
				sb.WriteString(fmt.Sprintf(" — %s", member.Role))
			}
			sb.WriteString(fmt.Sprintf(" — %s — %s\n", member.Status, timeAgo))

			// Repo/branch if available
			repoName := strings.TrimSpace(member.RepoName)
			branch := strings.TrimSpace(member.Branch)
			if repoName != "" {
				if branch != "" && branch != "main" && branch != "master" {
					sb.WriteString(fmt.Sprintf("  Repo: %s (%s)\n", repoName, branch))
				} else {
					sb.WriteString(fmt.Sprintf("  Repo: %s\n", repoName))
				}
			}

			// Working on / Epic
			apexID := strings.TrimSpace(member.ApexID)
			apexTitle := strings.TrimSpace(member.ApexTitle)
			if apexID != "" {
				prefix := "Working on"
				if member.ApexType == "epic" {
					prefix = "Epic"
				}
				if apexTitle != "" {
					sb.WriteString(fmt.Sprintf("  %s: %s \"%s\"\n", prefix, apexID, apexTitle))
				} else {
					sb.WriteString(fmt.Sprintf("  %s: %s\n", prefix, apexID))
				}
			}

			// Claims (skip for "you" since shown above)
			if !member.IsYou && len(member.Claims) > 0 {
				sb.WriteString("  Claims:\n")
				for _, claim := range member.Claims {
					claimAge := formatTimeAgo(claim.ClaimedAt)
					staleIndicator := ""
					if isClaimStale(claim.ClaimedAt) {
						staleIndicator = " ⚠️"
					}
					if claim.Title != "" {
						sb.WriteString(fmt.Sprintf("    %s \"%s\" — %s%s\n", claim.BeadID, claim.Title, claimAge, staleIndicator))
					} else {
						sb.WriteString(fmt.Sprintf("    %s — %s%s\n", claim.BeadID, claimAge, staleIndicator))
					}
				}
			}

			// Reservations (skip for "you" since shown above)
			if !member.IsYou && len(member.Locks) > 0 {
				sb.WriteString("  Reservations:\n")
				for i, lock := range member.Locks {
					if i >= 3 {
						sb.WriteString(fmt.Sprintf("    ...%d more\n", len(member.Locks)-3))
						break
					}
					expiresIn := formatDuration(lock.TTLRemainingSeconds)
					sb.WriteString(fmt.Sprintf("    %s (expires in %s)\n", lock.Path, expiresIn))
				}
			}
		}
	}

	// Escalations
	if result.EscalationsPending > 0 {
		sb.WriteString(fmt.Sprintf("\n## Escalations\nYou have %d pending escalation(s) to review.\n", result.EscalationsPending))
	}

	return sb.String()
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
