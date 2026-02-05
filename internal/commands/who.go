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

var whoJSON bool
var whoLimit int
var whoAll bool
var whoOnlyWithClaims bool

var whoCmd = &cobra.Command{
	Use:   ":who",
	Short: "Show who's working on what",
	Long: `Show active workspaces and their agents.

Lists a bounded set of registered workspaces (default 50) with their alias,
human owner, and status.

Examples:
  bdh :who           # List workspaces (bounded)
  bdh :who --json    # Output as JSON`,
	RunE: runWho,
}

func init() {
	whoCmd.Flags().BoolVar(&whoJSON, "json", false, "Output as JSON")
	whoCmd.Flags().IntVar(&whoLimit, "limit", defaultWhoLimit, "Maximum workspaces to show (max 200)")
	whoCmd.Flags().BoolVar(&whoAll, "all", false, "Show up to 200 workspaces (debugging)")
	whoCmd.Flags().BoolVar(&whoOnlyWithClaims, "only-with-claims", false, "Only show workspaces with active claims")
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

// WorkspaceInfo contains information about a workspace.
type WorkspaceInfo struct {
	WorkspaceID       string        `json:"workspace_id"`
	Alias             string        `json:"alias"`
	HumanName         string        `json:"human_name"`
	Role              string        `json:"role,omitempty"`
	ApexID            string        `json:"apex_id,omitempty"`
	ApexTitle         string        `json:"apex_title,omitempty"`
	ApexType          string        `json:"apex_type,omitempty"`
	FocusApexID       string        `json:"focus_apex_id,omitempty"`
	FocusApexTitle    string        `json:"focus_apex_title,omitempty"`
	FocusApexType     string        `json:"focus_apex_type,omitempty"`
	FocusApexRepoName string        `json:"focus_apex_repo_name,omitempty"`
	FocusApexBranch   string        `json:"focus_apex_branch,omitempty"`
	FocusUpdatedAt    string        `json:"focus_updated_at,omitempty"`
	Status            string        `json:"status"`
	LastSeen          string        `json:"last_seen"`
	Claims            []ClaimInfo   `json:"claims"`
	Locks             []LockSummary `json:"locks,omitempty"`
}

// WhoOptions controls query behavior for :who.
type WhoOptions struct {
	Limit          int
	OnlyWithClaims bool
	All            bool
}

// WhoResult contains the result of the who command.
type WhoResult struct {
	Workspaces []WorkspaceInfo
	Limit      int
	MaybeMore  bool
	OnlyClaims bool
}

func runWho(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err == nil && cfg.Validate() == nil {
		if err := validateRepoOriginMatchesCurrent(cfg); err != nil {
			return err
		}
	}

	result, err := fetchWho()
	if err != nil {
		return err
	}

	output := formatWhoOutput(result, whoJSON)
	fmt.Print(output)
	return nil
}

// fetchWho fetches workspace information from the BeadHub server.
func fetchWho() (*WhoResult, error) {
	cfg, err := config.Load()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no .beadhub file found - run 'bdh :init' first")
		}
		return nil, fmt.Errorf("loading config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid .beadhub config: %w", err)
	}
	if err := validateRepoOriginMatchesCurrent(cfg); err != nil {
		return nil, err
	}

	opts := WhoOptions{
		Limit:          whoLimit,
		OnlyWithClaims: whoOnlyWithClaims,
		All:            whoAll,
	}

	return fetchWhoWithConfig(cfg, opts)
}

// fetchWhoWithConfig fetches workspace information using the provided config (for testing).
func fetchWhoWithConfig(cfg *config.Config, opts WhoOptions) (*WhoResult, error) {
	c, err := newBeadHubClientRequired(cfg.BeadhubURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()

	limit := opts.Limit
	if opts.All {
		limit = defaultWhoMaxLimit
	}
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be greater than 0")
	}
	if limit > defaultWhoMaxLimit {
		return nil, fmt.Errorf("limit must be <= %d", defaultWhoMaxLimit)
	}

	// Scope to this project with bounded team view
	includeClaims := true
	includePresence := true
	onlyWithClaims := opts.OnlyWithClaims
	resp, err := c.TeamWorkspaces(ctx, &client.TeamWorkspacesRequest{
		IncludeClaims:   &includeClaims,
		IncludePresence: &includePresence,
		OnlyWithClaims:  &onlyWithClaims,
		Limit:           limit,
	})
	if err != nil {
		var clientErr *client.Error
		if errors.As(err, &clientErr) {
			return nil, fmt.Errorf("BeadHub error (%d): %s", clientErr.StatusCode, clientErr.Body)
		}
		return nil, fmt.Errorf("failed to fetch workspaces: %w", err)
	}

	result := &WhoResult{
		Workspaces: make([]WorkspaceInfo, 0, len(resp.Workspaces)),
		Limit:      limit,
		MaybeMore:  len(resp.Workspaces) >= limit && !opts.All,
		OnlyClaims: onlyWithClaims,
	}

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

	for _, ws := range resp.Workspaces {
		claims := make([]ClaimInfo, 0, len(ws.Claims))
		for _, c := range ws.Claims {
			claims = append(claims, ClaimInfo{
				BeadID:    c.BeadID,
				Title:     c.Title,
				ClaimedAt: c.ClaimedAt,
			})
		}
		result.Workspaces = append(result.Workspaces, WorkspaceInfo{
			WorkspaceID:       ws.WorkspaceID,
			Alias:             ws.Alias,
			HumanName:         ws.HumanName,
			Role:              ws.Role,
			ApexID:            ws.ApexID,
			ApexTitle:         ws.ApexTitle,
			ApexType:          ws.ApexType,
			FocusApexID:       ws.FocusApexID,
			FocusApexTitle:    ws.FocusApexTitle,
			FocusApexType:     ws.FocusApexType,
			FocusApexRepoName: ws.FocusApexRepoName,
			FocusApexBranch:   ws.FocusApexBranch,
			FocusUpdatedAt:    ws.FocusUpdatedAt,
			Status:            ws.Status,
			LastSeen:          ws.LastSeen,
			Claims:            claims,
			Locks:             locksByWorkspace[ws.WorkspaceID],
		})
	}

	return result, nil
}

// formatWhoOutput formats the who result for display.
func formatWhoOutput(result *WhoResult, asJSON bool) string {
	if asJSON {
		output := struct {
			Workspaces []WorkspaceInfo `json:"workspaces"`
			Count      int             `json:"count"`
		}{
			Workspaces: result.Workspaces,
			Count:      len(result.Workspaces),
		}
		return marshalJSONOrFallback(output)
	}

	if len(result.Workspaces) == 0 {
		return "No active workspaces.\n"
	}

	var sb strings.Builder
	header := "Active workspaces"
	if result.OnlyClaims {
		header = "Active workspaces with claims"
	}
	if result.Limit > 0 {
		sb.WriteString(fmt.Sprintf("%s (showing up to %d):\n\n", header, result.Limit))
	} else {
		sb.WriteString(fmt.Sprintf("%s:\n\n", header))
	}

	for _, ws := range result.Workspaces {
		timeAgo := formatTimeAgo(ws.LastSeen)
		sb.WriteString(fmt.Sprintf("  %s (%s)\n", ws.Alias, ws.HumanName))
		apexID := strings.TrimSpace(ws.ApexID)
		apexTitle := strings.TrimSpace(ws.ApexTitle)
		if apexID != "" {
			prefix := "    Working on: "
			if ws.ApexType == "epic" {
				prefix = "    Working on epic: "
			}
			if apexTitle != "" {
				sb.WriteString(fmt.Sprintf("%s%s %s\n", prefix, apexID, apexTitle))
			} else {
				sb.WriteString(fmt.Sprintf("%s%s\n", prefix, apexID))
			}
		} else if len(ws.Claims) == 0 {
			focusID := strings.TrimSpace(ws.FocusApexID)
			focusTitle := strings.TrimSpace(ws.FocusApexTitle)
			if focusID != "" {
				if focusTitle != "" {
					sb.WriteString(fmt.Sprintf("    Recent focus: %s %s\n", focusID, focusTitle))
				} else {
					sb.WriteString(fmt.Sprintf("    Recent focus: %s\n", focusID))
				}
			}
		}
		if len(ws.Claims) > 0 {
			sb.WriteString("    Claims:\n")
			for _, claim := range ws.Claims {
				claimAge := formatTimeAgo(claim.ClaimedAt)
				staleIndicator := ""
				if isClaimStale(claim.ClaimedAt) {
					staleIndicator = " ⚠️"
				}
				if claim.Title != "" {
					sb.WriteString(fmt.Sprintf("      %s \"%s\" — %s%s\n", claim.BeadID, claim.Title, claimAge, staleIndicator))
				} else {
					sb.WriteString(fmt.Sprintf("      %s — %s%s\n", claim.BeadID, claimAge, staleIndicator))
				}
			}
		}
		if len(ws.Locks) > 0 {
			sb.WriteString("    Reservations:\n")
			maxLocks := defaultWhoLocksLimit
			if maxLocks <= 0 {
				maxLocks = defaultWhoLocksLimit
			}
			for i, lock := range ws.Locks {
				if i >= maxLocks {
					break
				}
				expiresIn := formatDuration(lock.TTLRemainingSeconds)
				sb.WriteString(fmt.Sprintf("      %s (expires in %s)\n", lock.Path, expiresIn))
			}
			if len(ws.Locks) > maxLocks {
				sb.WriteString(fmt.Sprintf("      ...%d more (run \"bdh :reservations\")\n", len(ws.Locks)-maxLocks))
			}
		}
		sb.WriteString(fmt.Sprintf("    Status: %s — %s\n\n", ws.Status, timeAgo))
	}

	if result.MaybeMore {
		sb.WriteString(fmt.Sprintf("  …more workspaces not shown (use \"bdh :who --limit %d\")\n", defaultWhoLimit))
	}

	return sb.String()
}
