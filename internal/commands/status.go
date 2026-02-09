package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	Alias     string        `json:"alias"`
	Role      string        `json:"role,omitempty"`
	Status    string        `json:"status"`
	LastSeen  string        `json:"last_seen"`
	Hostname  string        `json:"hostname,omitempty"`
	Path      string        `json:"workspace_path,omitempty"`
	RepoName  string        `json:"repo_name,omitempty"`
	Branch    string        `json:"branch,omitempty"`
	ApexID    string        `json:"apex_id,omitempty"`
	ApexTitle string        `json:"apex_title,omitempty"`
	ApexType  string        `json:"apex_type,omitempty"`
	Claims    []ClaimInfo   `json:"claims,omitempty"`
	Locks     []LockSummary `json:"locks,omitempty"`
}

// StatusResult contains the result of the status command.
type StatusResult struct {
	Alias              string
	Role               string
	Hostname           string
	Path               string
	RepoName           string
	Branch             string
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
	result.RepoName = cfg.CanonicalOrigin
	result.Hostname, _ = os.Hostname()
	if root, err := config.WorkspaceRoot(); err == nil {
		result.Path = root
	}

	// Fetch all project workspaces with claims
	includePresence := true
	teamResp, err := c.Workspaces(ctx, &client.WorkspacesRequest{
		IncludeClaims:   true,
		IncludePresence: &includePresence,
		Limit:           defaultStatusTeamLimit,
	})
	if err != nil {
		var clientErr *client.Error
		if errors.As(err, &clientErr) {
			return nil, formatClientErr(err)
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
			// Prefer server-tracked workspace location for self if available.
			if strings.TrimSpace(ws.Hostname) != "" {
				result.Hostname = ws.Hostname
			}
			if strings.TrimSpace(ws.WorkspacePath) != "" {
				result.Path = ws.WorkspacePath
			}
			if strings.TrimSpace(ws.FocusApexRepoName) != "" {
				result.RepoName = ws.FocusApexRepoName
			}
			if strings.TrimSpace(ws.FocusApexBranch) != "" {
				result.Branch = ws.FocusApexBranch
			}
			continue // Don't add self to team list - shown in "You" section
		}

		result.Team = append(result.Team, TeamMemberInfo{
			Alias:     ws.Alias,
			Role:      ws.Role,
			Status:    ws.Status,
			LastSeen:  ws.LastSeen,
			Hostname:  ws.Hostname,
			Path:      ws.WorkspacePath,
			RepoName:  ws.FocusApexRepoName,
			Branch:    ws.FocusApexBranch,
			ApexID:    ws.ApexID,
			ApexTitle: ws.ApexTitle,
			ApexType:  ws.ApexType,
			Claims:    claims,
			Locks:     locks,
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
	if h := strings.TrimSpace(result.Hostname); h != "" {
		sb.WriteString(fmt.Sprintf("- Hostname: %s\n", h))
	}
	if p := strings.TrimSpace(result.Path); p != "" {
		sb.WriteString(fmt.Sprintf("- Path: %s\n", abbreviateUserHome(p)))
	}
	if repo := strings.TrimSpace(result.RepoName); repo != "" {
		sb.WriteString(fmt.Sprintf("- Repo: %s\n", repo))
		if b := strings.TrimSpace(result.Branch); b != "" && b != "main" && b != "master" {
			sb.WriteString(fmt.Sprintf("- Branch: %s\n", b))
		}
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

	// Team (other members, not you)
	sb.WriteString("\n## Team\n")
	if len(result.Team) == 0 {
		sb.WriteString("No other team members.\n")
	} else {
		for _, member := range result.Team {
			timeAgo := formatTimeAgo(member.LastSeen)

			// Header line: alias — role — status — time
			sb.WriteString(fmt.Sprintf("- **%s**", member.Alias))
			if member.Role != "" {
				sb.WriteString(fmt.Sprintf(" — %s", member.Role))
			}
			sb.WriteString(fmt.Sprintf(" — %s — %s\n", member.Status, timeAgo))

			// Workspace location.
			host := strings.TrimSpace(member.Hostname)
			if host != "" {
				sb.WriteString(fmt.Sprintf("  Hostname: %s\n", host))
			}
			path := strings.TrimSpace(member.Path)
			if path != "" {
				sb.WriteString(fmt.Sprintf("  Path: %s\n", formatWorkspacePath(path, host, result.Hostname, result.Path)))
			}

			// Repo/branch.
			repoName := strings.TrimSpace(member.RepoName)
			branch := strings.TrimSpace(member.Branch)
			if repoName != "" {
				sb.WriteString(fmt.Sprintf("  Repo: %s\n", repoName))
			}
			if branch != "" && branch != "main" && branch != "master" {
				sb.WriteString(fmt.Sprintf("  Branch: %s\n", branch))
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

			// Claims
			if len(member.Claims) > 0 {
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

			// Reservations
			if len(member.Locks) > 0 {
				sb.WriteString("  Reservations:\n")
				for i, lock := range member.Locks {
					if i >= defaultStatusTeamReservationsMax {
						sb.WriteString(fmt.Sprintf("    ...%d more\n", len(member.Locks)-defaultStatusTeamReservationsMax))
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

func abbreviateUserHome(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	p = filepath.Clean(p)

	if strings.HasPrefix(p, "/Users/") {
		rest := strings.TrimPrefix(p, "/Users/")
		parts := strings.SplitN(rest, string(filepath.Separator), 2)
		if len(parts) == 1 {
			return "~"
		}
		return "~" + string(filepath.Separator) + parts[1]
	}
	if strings.HasPrefix(p, "/home/") {
		rest := strings.TrimPrefix(p, "/home/")
		parts := strings.SplitN(rest, string(filepath.Separator), 2)
		if len(parts) == 1 {
			return "~"
		}
		return "~" + string(filepath.Separator) + parts[1]
	}
	return p
}

func formatWorkspacePath(path, memberHost, yourHost, yourPath string) string {
	raw := strings.TrimSpace(path)
	if raw == "" {
		return ""
	}

	// Prefer relative paths when on the same host and both sides have absolute paths.
	if strings.TrimSpace(memberHost) != "" && strings.TrimSpace(yourHost) != "" && memberHost == yourHost {
		base := strings.TrimSpace(yourPath)
		if base != "" {
			if rel, err := filepath.Rel(base, raw); err == nil {
				// Only use rel when it's meaningfully shorter.
				if rel != "." && len(rel) < len(raw) {
					return rel
				}
			}
		}
	}

	return abbreviateUserHome(raw)
}
