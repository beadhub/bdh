package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	aweb "github.com/awebai/aw"
	"github.com/beadhub/bdh/internal/bd"
	"github.com/beadhub/bdh/internal/beads"
	"github.com/beadhub/bdh/internal/client"
	"github.com/beadhub/bdh/internal/config"
	"github.com/beadhub/bdh/internal/sync"
)

// parseLocalConfig parses the --:local-config flag from args.
// Returns:
//   - cleanArgs: args with --:local-config and its value removed
//   - path: the config path argument (empty if not provided)
//   - hasLocalConfig: true if --:local-config was present
//
// Supports both "--:local-config path" and "--:local-config=path" syntax.
func parseLocalConfig(args []string) (cleanArgs []string, path string, hasLocalConfig bool) {
	cleanArgs = make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		arg := args[i]

		// Check for --:local-config=value syntax
		if strings.HasPrefix(arg, "--:local-config=") {
			hasLocalConfig = true
			path = strings.TrimPrefix(arg, "--:local-config=")
			continue
		}

		// Check for --:local-config value syntax
		if arg == "--:local-config" {
			hasLocalConfig = true
			// Next arg is the path (if exists and doesn't look like a flag)
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				path = args[i+1]
				i++ // Skip the path arg
			}
			continue
		}

		cleanArgs = append(cleanArgs, arg)
	}

	return cleanArgs, path, hasLocalConfig
}

// parseJumpIn parses the --:jump-in flag from args.
// Returns:
//   - cleanArgs: args with --:jump-in and its value removed
//   - message: the message argument (empty if not provided)
//   - hasJumpIn: true if --:jump-in was present
//
// Supports both "--:jump-in message" and "--:jump-in=message" syntax.
func parseJumpIn(args []string) (cleanArgs []string, message string, hasJumpIn bool) {
	cleanArgs = make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		arg := args[i]

		// Check for --:jump-in=value syntax
		if strings.HasPrefix(arg, "--:jump-in=") {
			hasJumpIn = true
			message = strings.TrimPrefix(arg, "--:jump-in=")
			continue
		}

		// Check for --:jump-in value syntax
		if arg == "--:jump-in" {
			hasJumpIn = true
			// Next arg is the message (if exists and doesn't look like a flag)
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				message = args[i+1]
				i++ // Skip the message arg
			}
			continue
		}

		cleanArgs = append(cleanArgs, arg)
	}

	return cleanArgs, message, hasJumpIn
}

// RelatedWorkItem represents a bead being worked on that is related to the one just closed.
type RelatedWorkItem struct {
	BeadID      string // e.g., "bd-43"
	Title       string // Bead title
	Alias       string // Agent alias working on it
	HumanName   string // Human name of the agent
	WorkspaceID string // Workspace ID for sending messages
	Relation    string // How it's related (e.g., "blocked by bd-42", "same parent epic")
}

// PassthroughResult contains the result of running a bd command through bdh.
type PassthroughResult struct {
	// From bd execution
	Stdout   string
	Stderr   string
	ExitCode int
	JSONMode bool

	// From coordination
	Warning         string // Warning message (e.g., server unreachable)
	Rejected        bool   // True if server rejected the command
	RejectionReason string // Why the command was rejected
	BeadsInProgress []client.BeadInProgress

	// From sync
	SyncWarning string // Warning message from sync attempt
	SyncStats   *client.SyncStats
	SyncMode    string // "full" or "incremental"

	// From auto-reserve
	AutoReserveWarning   string
	AutoReserved         []string
	AutoRenewed          []string
	AutoReleased         []string
	AutoReserveConflicts []ReservationConflict

	// Ready command context (shown after bd ready output)
	IsReadyCommand   bool
	MyAlias          string         // Current agent's alias for filtering
	MyClaims         []client.Claim // My own active bead claims
	MyFocusApexID    string
	MyFocusApexTitle string
	MyFocusApexType  string
	TeamStatus       []client.Workspace // Other workspaces with their current beads
	TeamStatusLimit  int
	TeamStatusMore   bool
	ReadyLocks       []aweb.ReservationView

	// Close command context: related work in progress
	RelatedWork []RelatedWorkItem
}

// runPassthrough executes a bd command with pre-flight coordination check.
//
// Blocking on explicit rejections, non-blocking on server errors:
//   - If the server is unreachable, bd still runs with a warning (non-blocking).
//   - If the server returns an error (5xx), bd still runs with a warning (non-blocking).
//   - If the server rejects the command, bd does NOT run (blocking) - use --:jump-in to override.
//
// The --:jump-in flag allows overriding a rejection to join work on a bead
// that another agent is working on. When used:
//   - The rejection is overridden (result.Rejected will be false)
//   - A notification is sent to other agents working on the bead
//   - The command proceeds normally
func runPassthrough(args []string) (*PassthroughResult, error) {
	result := &PassthroughResult{}

	// Parse --:local-config flag first (affects config loading)
	args, configPath, hasLocalConfig := parseLocalConfig(args)
	if hasLocalConfig && configPath != "" {
		config.SetPath(configPath)
		defer config.SetPath("") // Reset after this command
	}

	// Validate args
	if len(args) == 0 {
		return nil, fmt.Errorf("no command provided")
	}

	// Parse --:jump-in flag (must be done before validation)
	cleanArgs, jumpInMessage, hasJumpIn := parseJumpIn(args)
	result.JSONMode = isJSONOutputRequested(cleanArgs)

	// Validate --:jump-in requires a message
	if hasJumpIn && jumpInMessage == "" {
		return nil, fmt.Errorf("--:jump-in requires a message explaining why you're joining")
	}

	// Load config
	cfg, err := config.Load()
	if err != nil {
		if os.IsNotExist(err) {
			if hasJumpIn {
				return nil, fmt.Errorf("--:jump-in requires a configured workspace - run 'bdh :init' first")
			}

			result.Warning = "No .beadhub config found - running without coordination"

			runner := bd.New()
			bdResult, runErr := runner.Run(context.Background(), cleanArgs)
			if runErr != nil {
				return nil, fmt.Errorf("running bd: %w", runErr)
			}

			result.Stdout = bdResult.Stdout
			result.Stderr = bdResult.Stderr
			result.ExitCode = bdResult.ExitCode
			return result, nil
		}
		return nil, fmt.Errorf("loading config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid .beadhub config: %w", err)
	}
	if err := validateRepoOriginMatchesCurrent(cfg); err != nil {
		return nil, err
	}

	// Set up coordination header for this agent (printed once before first coordination section)
	SetCoordinationHeaderAlias(cfg.Alias)

	// Build command line string for the server (without --:jump-in)
	commandLine := strings.Join(cleanArgs, " ")

	// Create client for BeadHub server
	c := newBeadHubClient(cfg.BeadhubURL)
	aw, _ := newAwebClient(cfg.BeadhubURL)

	// Pre-flight check with BeadHub server
	cmdCtx, cmdCancel := context.WithTimeout(context.Background(), apiTimeout)
	cmdResp, err := c.Command(cmdCtx, &client.CommandRequest{
		WorkspaceID: cfg.WorkspaceID,
		RepoID:      cfg.RepoID,
		Alias:       cfg.Alias,
		HumanName:   cfg.HumanName,
		RepoOrigin:  cfg.RepoOrigin,
		Role:        cfg.Role,
		CommandLine: commandLine,
	})
	cmdCancel()

	// Track if we need to notify other agents (when --:jump-in overrides rejection)
	var notifyAgents []client.BeadInProgress
	var notifyBeadID string

	if err != nil {
		// Server error - check for specific conditions
		var clientErr *client.Error
		if errors.As(err, &clientErr) {
			// HTTP 410 Gone = workspace was deleted
			if clientErr.StatusCode == 410 {
				return nil, fmt.Errorf("workspace was deleted. Run 'bdh :init' to re-register")
			}
			// Other HTTP errors (4xx, 5xx) - warn but continue
			result.Warning = fmt.Sprintf("BeadHub error (%d) - running without coordination", clientErr.StatusCode)
		} else {
			// Network error (connection refused, timeout, etc.)
			result.Warning = fmt.Sprintf("BeadHub unreachable at %s - running without coordination", cfg.BeadhubURL)
		}
	} else {
		// Server responded successfully
		if cmdResp.Context != nil {
			result.BeadsInProgress = cmdResp.Context.BeadsInProgress
		}

		if !cmdResp.Approved {
			if hasJumpIn {
				// --:jump-in overrides rejection
				// Find the bead we're claiming from the args (not the joined string)
				notifyBeadID = extractBeadIDFromArgs(cleanArgs)
				if notifyBeadID == "" {
					// Couldn't extract bead ID - warn but continue
					result.Warning = "--:jump-in used but couldn't extract bead ID from command"
				} else {
					// Find agents to notify (those working on this bead)
					for _, bip := range cmdResp.Context.BeadsInProgress {
						if bip.BeadID == notifyBeadID && bip.WorkspaceID != cfg.WorkspaceID {
							notifyAgents = append(notifyAgents, bip)
						}
					}
				}
				// Don't mark as rejected since we're overriding
			} else {
				result.Rejected = true
				result.RejectionReason = cmdResp.Reason
			}
		} else if isCloseCommandFromArgs(cleanArgs) {
			// For close commands, check if other agents have claims on this bead
			beadID := extractBeadIDFromArgs(cleanArgs)
			if beadID != "" && cmdResp.Context != nil {
				otherClaimants := hasOtherClaimants(beadID, cfg.WorkspaceID, cmdResp.Context.BeadsInProgress)
				if len(otherClaimants) > 0 {
					if hasJumpIn {
						// --:jump-in allows closing, notify others
						notifyBeadID = beadID
						notifyAgents = otherClaimants
					} else {
						// Require --:jump-in to close when others are working
						result.Rejected = true
						var names []string
						for _, c := range otherClaimants {
							names = append(names, fmt.Sprintf("%s (%s)", c.Alias, c.HumanName))
						}
						result.RejectionReason = fmt.Sprintf(
							"%s has active claims by: %s. Use --:jump-in \"reason\" to close anyway and notify them.",
							beadID, strings.Join(names, ", "))
						result.BeadsInProgress = cmdResp.Context.BeadsInProgress
					}
				}
			}
		}
	}

	// If rejected without --:jump-in, don't run bd - just return rejection info
	if result.Rejected {
		return result, nil
	}

	// For "ready" command, fetch additional context (team status)
	if len(cleanArgs) > 0 && cleanArgs[0] == "ready" {
		result.IsReadyCommand = true
		result.MyAlias = cfg.Alias

		// Use timeout context for non-blocking operations to avoid hanging
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		// Fetch team status (non-blocking - silently fail on errors)
		// Query all workspaces (not just those with claims) to show focus apex
		includeClaims := true
		includePresence := true
		onlyWithClaims := false
		teamLimit := defaultReadyTeamLimit
		queryLimit := teamLimit + readyTeamQueryOverflow
		workspacesResp, wsErr := c.TeamWorkspaces(ctx, &client.TeamWorkspacesRequest{
			IncludeClaims:            &includeClaims,
			IncludePresence:          &includePresence,
			OnlyWithClaims:           &onlyWithClaims,
			AlwaysIncludeWorkspaceID: cfg.WorkspaceID,
			Limit:                    queryLimit,
		})
		if wsErr == nil {
			// Find my own claims and filter team status
			// Include workspaces with focus OR claims that were recently active
			var activeTeam []client.Workspace
			activeThreshold := teamActivityThreshold()
			for _, ws := range workspacesResp.Workspaces {
				if ws.WorkspaceID == cfg.WorkspaceID {
					// This is my workspace - capture my claims
					result.MyClaims = ws.Claims
					result.MyFocusApexID = ws.FocusApexID
					result.MyFocusApexTitle = ws.FocusApexTitle
					result.MyFocusApexType = ws.FocusApexType
				} else if ws.FocusApexID != "" || len(ws.Claims) > 0 {
					// Other workspaces with focus or claims - check if recently active
					if isWorkspaceRecentlyActive(ws, activeThreshold) {
						activeTeam = append(activeTeam, ws)
					}
				}
			}
			result.TeamStatusLimit = teamLimit
			if len(activeTeam) > teamLimit {
				result.TeamStatusMore = true
				activeTeam = activeTeam[:teamLimit]
			} else if len(workspacesResp.Workspaces) >= queryLimit {
				result.TeamStatusMore = true
			}
			result.TeamStatus = activeTeam
		}

		// Fetch active locks (non-blocking - silently fail on errors)
		if aw != nil {
			locksResp, locksErr := aw.ReservationList(ctx, "")
			if locksErr == nil {
				result.ReadyLocks = locksResp.Reservations
				sort.Slice(result.ReadyLocks, func(i, j int) bool {
					if result.ReadyLocks[i].ResourceKey == result.ReadyLocks[j].ResourceKey {
						return result.ReadyLocks[i].HolderAlias < result.ReadyLocks[j].HolderAlias
					}
					return result.ReadyLocks[i].ResourceKey < result.ReadyLocks[j].ResourceKey
				})
			}
		}
	}

	// Auto-reserve modified files before running bd (non-blocking)
	if aw != nil {
		if autoResult := autoReserve(context.Background(), cfg, aw); autoResult != nil {
			result.AutoReserveWarning = autoResult.Warning
			result.AutoReserved = autoResult.Acquired
			result.AutoRenewed = autoResult.Renewed
			result.AutoReleased = autoResult.Released
			result.AutoReserveConflicts = autoResult.Conflicts
		}
	}

	// Run bd with cleaned args (without --:jump-in)
	runner := bd.New()
	bdResult, err := runner.Run(context.Background(), cleanArgs)
	if err != nil {
		return nil, fmt.Errorf("running bd: %w", err)
	}

	result.Stdout = bdResult.Stdout
	result.Stderr = bdResult.Stderr
	result.ExitCode = bdResult.ExitCode

	// Sync after mutation commands (non-blocking - just warn on failure)
	if bd.IsMutationCommand(cleanArgs) && bdResult.ExitCode == 0 {
		syncResult := syncToBeadHub(cfg, cleanArgs)
		if syncResult.Warning != "" {
			result.SyncWarning = syncResult.Warning
		}
		result.SyncStats = syncResult.Stats
		result.SyncMode = syncResult.SyncMode
	}

	// For successful close commands, find related work in progress
	if isCloseCommandFromArgs(cleanArgs) && bdResult.ExitCode == 0 {
		closedBeadID := extractBeadIDFromArgs(cleanArgs)
		if closedBeadID != "" {
			// Find related work in progress
			if cmdResp != nil && cmdResp.Context != nil {
				result.RelatedWork = findRelatedWorkInProgress(
					closedBeadID,
					cfg.WorkspaceID,
					cmdResp.Context.BeadsInProgress,
				)
			}
		}
	}

	// Send notifications to other agents when --:jump-in is used
	// We send regardless of bd exit code - the notification is about intent to join
	if len(notifyAgents) > 0 {
		notifyMessage := fmt.Sprintf("%s is joining work on %s: %s", cfg.Alias, notifyBeadID, jumpInMessage)
		for _, agent := range notifyAgents {
			// Non-blocking - silently ignore errors
			if aw == nil {
				continue
			}
			notifyCtx, notifyCancel := context.WithTimeout(context.Background(), apiTimeout)
			_, _ = aw.SendMessage(notifyCtx, &aweb.SendMessageRequest{
				ToAgentID: agent.WorkspaceID,
				Body:      notifyMessage,
			})
			notifyCancel()
		}
	}

	return result, nil
}

func isJSONOutputRequested(args []string) bool {
	for _, arg := range args {
		if arg == "--json" {
			return true
		}
	}
	return false
}

// extractBeadIDFromArgs extracts the bead ID from args like ["update", "bd-42", "--status", "in_progress"].
// Only extracts from update and close commands.
func extractBeadIDFromArgs(args []string) string {
	if len(args) >= 2 && (args[0] == "update" || args[0] == "close") {
		return args[1]
	}
	return ""
}

// Issue represents a bead issue from issues.jsonl.
type Issue struct {
	ID           string       `json:"id"`
	Title        string       `json:"title"`
	Status       string       `json:"status"`
	Dependencies []Dependency `json:"dependencies,omitempty"`
	Labels       []string     `json:"labels,omitempty"`
}

// Dependency represents a dependency relationship between issues.
type Dependency struct {
	IssueID     string `json:"issue_id"`
	DependsOnID string `json:"depends_on_id"`
	Type        string `json:"type"` // "blocks", "parent-child", "discovered-from"
}

// loadIssues parses issues.jsonl from the beads directory and returns all issues.
func loadIssues() ([]Issue, error) {
	content, err := os.ReadFile(beads.IssuesJSONLPath())
	if err != nil {
		return nil, err
	}

	var issues []Issue
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var issue Issue
		if err := json.Unmarshal([]byte(line), &issue); err != nil {
			continue // Skip malformed lines
		}
		issues = append(issues, issue)
	}
	return issues, nil
}

// findRelatedBeadIDs finds bead IDs that are related to the given bead ID.
// Related means: dependency relationship (blocks/blocked-by), same parent epic.
func findRelatedBeadIDs(closedBeadID string, issues []Issue) map[string]string {
	related := make(map[string]string) // beadID -> relation description

	// Find the closed issue
	var closedIssue *Issue
	for i := range issues {
		if issues[i].ID == closedBeadID {
			closedIssue = &issues[i]
			break
		}
	}
	if closedIssue == nil {
		return related
	}

	// Find the parent epic of the closed issue (if any)
	var parentEpicID string
	for _, dep := range closedIssue.Dependencies {
		if dep.Type == "parent-child" && dep.IssueID == closedBeadID {
			parentEpicID = dep.DependsOnID
			break
		}
	}

	// Check all issues for relationships
	for _, issue := range issues {
		if issue.ID == closedBeadID {
			continue // Skip the closed issue itself
		}

		// Check if this issue depends on the closed bead (closed bead blocks this one)
		for _, dep := range issue.Dependencies {
			if dep.Type == "blocks" && dep.DependsOnID == closedBeadID {
				related[issue.ID] = fmt.Sprintf("blocked by %s", closedBeadID)
			}
		}

		// Check if they share the same parent epic
		if parentEpicID != "" {
			for _, dep := range issue.Dependencies {
				if dep.Type == "parent-child" && dep.IssueID == issue.ID && dep.DependsOnID == parentEpicID {
					if _, exists := related[issue.ID]; !exists {
						related[issue.ID] = "same parent epic"
					}
				}
			}
		}
	}

	return related
}

// findRelatedWorkInProgress finds beads that are related to the closed bead
// and are currently being worked on by other agents.
func findRelatedWorkInProgress(closedBeadID, myWorkspaceID string, beadsInProgress []client.BeadInProgress) []RelatedWorkItem {
	// Load issues from local file
	issues, err := loadIssues()
	if err != nil {
		return nil // Silently fail - this is non-critical
	}

	// Find related bead IDs
	relatedBeadIDs := findRelatedBeadIDs(closedBeadID, issues)
	if len(relatedBeadIDs) == 0 {
		return nil
	}

	// Cross-reference with beads in progress (by other agents)
	var result []RelatedWorkItem
	for _, bip := range beadsInProgress {
		if bip.WorkspaceID == myWorkspaceID {
			continue // Skip our own work
		}
		if bip.WorkspaceID == "" || bip.Alias == "" {
			continue // Skip incomplete data
		}
		if relation, isRelated := relatedBeadIDs[bip.BeadID]; isRelated {
			result = append(result, RelatedWorkItem{
				BeadID:      bip.BeadID,
				Title:       bip.Title,
				Alias:       bip.Alias,
				HumanName:   bip.HumanName,
				WorkspaceID: bip.WorkspaceID,
				Relation:    relation,
			})
		}
	}

	return result
}

// isCloseCommandFromArgs checks if args represent a close command.
func isCloseCommandFromArgs(args []string) bool {
	return len(args) >= 1 && args[0] == "close"
}

// isClaimCommand checks if args represent a claim command (update --status in_progress).
func isClaimCommand(args []string) bool {
	if len(args) < 2 || args[0] != "update" {
		return false
	}

	for i := 1; i < len(args); i++ {
		arg := args[i]

		// Check for --status in_progress or -s in_progress
		if (arg == "--status" || arg == "-s") && i+1 < len(args) && args[i+1] == "in_progress" {
			return true
		}

		// Check for --status=in_progress
		if strings.HasPrefix(arg, "--status=") && strings.TrimPrefix(arg, "--status=") == "in_progress" {
			return true
		}
	}

	return false
}

// isWorkspaceRecentlyActive checks if a workspace was active after the given threshold.
// Returns true if EITHER FocusUpdatedAt OR LastSeen is recent (uses OR logic, not fallback).
// An agent may have set focus a while ago but is still actively working within that focus.
func isWorkspaceRecentlyActive(ws client.Workspace, threshold time.Time) bool {
	hasValidTimestamp := false

	// Check if FocusUpdatedAt is recent
	if ws.FocusUpdatedAt != "" {
		if t, err := time.Parse(time.RFC3339, ws.FocusUpdatedAt); err == nil {
			hasValidTimestamp = true
			if t.After(threshold) {
				return true
			}
		}
	}

	// Check if LastSeen is recent (even if FocusUpdatedAt was old)
	if ws.LastSeen != "" {
		if t, err := time.Parse(time.RFC3339, ws.LastSeen); err == nil {
			hasValidTimestamp = true
			if t.After(threshold) {
				return true
			}
		}
	}

	// If we have valid timestamps but both are old, exclude
	if hasValidTimestamp {
		return false
	}

	// If we can't parse any timestamp, include the workspace (conservative approach)
	return true
}

// hasOtherClaimants checks if the bead has claims from other workspaces.
func hasOtherClaimants(beadID, myWorkspaceID string, beadsInProgress []client.BeadInProgress) []client.BeadInProgress {
	var others []client.BeadInProgress
	for _, bip := range beadsInProgress {
		if bip.BeadID == beadID && bip.WorkspaceID != myWorkspaceID {
			others = append(others, bip)
		}
	}
	return others
}

// SyncResult contains the result of syncing to BeadHub.
type SyncResult struct {
	Synced      bool
	Warning     string
	IssuesCount int
	// Sync mode and stats
	SyncMode string // "full" or "incremental"
	Stats    *client.SyncStats
}

// syncToBeadHub reads issues.jsonl from the beads directory and syncs to BeadHub.
// Uses incremental sync when possible (only sending changed issues).
// Returns warning on failure but never errors (non-blocking design).
func syncToBeadHub(cfg *config.Config, bdArgs []string) *SyncResult {
	result := &SyncResult{}

	issuesPath, exportArgs := resolveIssuesPathAndExportArgs(bdArgs)

	// Force an explicit export before uploading so the JSONL reflects the latest
	// state even when bd is operating via the daemon (which may export async).
	exportRunner := bd.New()
	exportCtx, exportCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer exportCancel()
	exportResult, exportErr := exportRunner.Run(exportCtx, exportArgs)
	if exportErr != nil {
		result.Warning = "bd export failed - aborting sync to prevent stale data upload"
		return result
	}
	if exportResult.ExitCode != 0 {
		result.Warning = fmt.Sprintf("bd export failed (exit %d) - aborting sync to prevent stale data upload", exportResult.ExitCode)
		return result
	}

	// Read issues.jsonl
	content, err := os.ReadFile(issuesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return result // No file to sync
		}
		result.Warning = fmt.Sprintf("could not read %s: %v", issuesPath, err)
		return result
	}

	// Load sync state for incremental sync
	syncStatePath := beads.SyncStatePath()
	syncState, err := sync.LoadState(syncStatePath)
	if err != nil {
		// Can't load state - fall back to full sync
		syncState = &sync.SyncState{IssueHashes: make(map[string]string)}
	}

	// Compute current hashes
	currentHashes, err := sync.ComputeIssueHashes(content)
	if err != nil {
		result.Warning = fmt.Sprintf("could not compute issue hashes: %v", err)
		return result
	}

	// Determine sync mode and prepare request
	c := newBeadHubClient(cfg.BeadhubURL)
	syncCtx, syncCancel := context.WithTimeout(context.Background(), apiTimeout)
	defer syncCancel()

	var req *client.SyncRequest

	if sync.NeedsFullSync(syncState) {
		// Full sync: send everything
		result.SyncMode = "full"
		req = &client.SyncRequest{
			WorkspaceID: cfg.WorkspaceID,
			RepoID:      cfg.RepoID,
			Alias:       cfg.Alias,
			HumanName:   cfg.HumanName,
			RepoOrigin:  cfg.RepoOrigin,
			Role:        cfg.Role,
			CommandLine: strings.Join(bdArgs, " "),
			SyncMode:    "full",
			IssuesJSONL: string(content),
			SyncProtocolVersion: func() *int {
				v := syncState.ProtocolVersion
				return &v
			}(),
		}
	} else {
		// Incremental sync: only send changes
		result.SyncMode = "incremental"
		changedIDs := sync.FindChangedIssues(currentHashes, syncState.IssueHashes)
		deletedIDs := sync.FindDeletedIssues(currentHashes, syncState.IssueHashes)

		if len(changedIDs) == 0 && len(deletedIDs) == 0 {
			// Nothing to upload.
			return result
		}

		// Extract changed issues from JSONL
		changedIssues, err := sync.ExtractIssuesByID(content, changedIDs)
		if err != nil {
			result.Warning = fmt.Sprintf("could not extract changed issues: %v", err)
			return result
		}
		if strings.TrimSpace(changedIssues) == "" && len(deletedIDs) == 0 {
			// Avoid sending an invalid incremental request (server rejects it with 422).
			result.Warning = "sync skipped - no changed issues found in JSONL"
			return result
		}

		req = &client.SyncRequest{
			WorkspaceID:   cfg.WorkspaceID,
			RepoID:        cfg.RepoID,
			Alias:         cfg.Alias,
			HumanName:     cfg.HumanName,
			RepoOrigin:    cfg.RepoOrigin,
			Role:          cfg.Role,
			CommandLine:   strings.Join(bdArgs, " "),
			SyncMode:      "incremental",
			ChangedIssues: changedIssues,
			DeletedIDs:    deletedIDs,
			SyncProtocolVersion: func() *int {
				v := syncState.ProtocolVersion
				return &v
			}(),
		}
	}

	resp, err := c.Sync(syncCtx, req)
	if err != nil {
		var clientErr *client.Error
		if errors.As(err, &clientErr) && clientErr.StatusCode == 409 {
			// Protocol mismatch: retry once with full sync.
			result.SyncMode = "full"
				fullReq := &client.SyncRequest{
					WorkspaceID: cfg.WorkspaceID,
					RepoID:      cfg.RepoID,
					Alias:       cfg.Alias,
					HumanName:   cfg.HumanName,
					RepoOrigin:  cfg.RepoOrigin,
					Role:        cfg.Role,
					CommandLine: strings.Join(bdArgs, " "),
					SyncMode:    "full",
					IssuesJSONL: string(content),
					SyncProtocolVersion: func() *int {
						v := syncState.ProtocolVersion
						return &v
				}(),
			}

			resp, err = c.Sync(syncCtx, fullReq)
		}

		// Non-blocking: just warn on failure
		if err != nil {
			if errors.As(err, &clientErr) {
				result.Warning = fmt.Sprintf("sync failed (%d) - changes saved locally only", clientErr.StatusCode)
			} else {
				result.Warning = fmt.Sprintf("sync failed - changes saved locally only (%v)", err)
			}
			return result
		}
	}

	// Update sync state on success
	if resp.SyncProtocolVersion > 0 {
		syncState.ProtocolVersion = resp.SyncProtocolVersion
	}
	sync.UpdateState(syncState, currentHashes)
	if err := sync.SaveState(syncStatePath, syncState); err != nil {
		// Non-fatal: state save failed, next sync will be full
		result.Warning = "sync succeeded but could not save sync state"
	}

	result.Synced = resp.Synced
	result.IssuesCount = resp.IssuesCount
	result.Stats = resp.Stats

	return result
}

func resolveIssuesPathAndExportArgs(bdArgs []string) (issuesPath string, exportArgs []string) {
	var dbPath string
	noDaemon := false
	noDB := false

	for i := 0; i < len(bdArgs); i++ {
		arg := bdArgs[i]
		if arg == "--no-daemon" {
			noDaemon = true
			continue
		}
		if arg == "--no-db" {
			noDB = true
			continue
		}
		if strings.HasPrefix(arg, "--db=") {
			dbPath = strings.TrimPrefix(arg, "--db=")
			continue
		}
		if arg == "--db" && i+1 < len(bdArgs) {
			dbPath = bdArgs[i+1]
			i++
			continue
		}
	}

	if dbPath != "" {
		issuesPath = filepath.Join(filepath.Dir(dbPath), "issues.jsonl")
	} else {
		issuesPath = beads.IssuesJSONLPath()
	}

	exportArgs = []string{}
	if noDaemon {
		exportArgs = append(exportArgs, "--no-daemon")
	}
	if noDB {
		exportArgs = append(exportArgs, "--no-db")
	}
	if dbPath != "" {
		exportArgs = append(exportArgs, "--db", dbPath)
	}
	exportArgs = append(exportArgs, "export", "-o", issuesPath)

	return issuesPath, exportArgs
}

// formatPassthroughOutput formats the passthrough result for display.
// Coordination info (YOUR FOCUS, TEAM STATUS, NOTIFICATIONS) appears at the end
// for consistent output structure across all bdh commands.
func formatPassthroughOutput(result *PassthroughResult) string {
	if result.JSONMode {
		return formatPassthroughOutputJSON(result)
	}

	var sb strings.Builder

	// Show warning if any
	if result.Warning != "" {
		sb.WriteString(fmt.Sprintf("Warning: %s\n\n", result.Warning))
	}

	// Show rejection info if rejected
	if result.Rejected {
		sb.WriteString(fmt.Sprintf("REJECTED: %s\n\n", result.RejectionReason))
		if len(result.BeadsInProgress) > 0 {
			sb.WriteString("Beads in progress:\n")
			for _, b := range result.BeadsInProgress {
				if b.Title != "" {
					sb.WriteString(fmt.Sprintf("  %s — %s (%s) — \"%s\"\n", b.BeadID, b.Alias, b.HumanName, b.Title))
				} else {
					sb.WriteString(fmt.Sprintf("  %s — %s (%s)\n", b.BeadID, b.Alias, b.HumanName))
				}
			}
			sb.WriteString("\n")
		}
		sb.WriteString("Options:\n")
		sb.WriteString("  - Pick different work: bdh ready\n")
		sb.WriteString("  - Message them: bdh :aweb mail send <agent-name> \"message\"\n")
		sb.WriteString("  - Escalate: bdh :escalate \"subject\" \"situation\"\n")
		sb.WriteString("\n")
	}

	// Show bd output (normalize trailing newlines for consistent spacing)
	if result.Stdout != "" {
		stdout := strings.TrimRight(result.Stdout, "\n")
		stdout = rewriteBDHelpOutput(stdout)
		if stdout != "" {
			sb.WriteString(stdout)
			sb.WriteString("\n")
		}
	}
	if result.Stderr != "" {
		sb.WriteString(rewriteBDHelpOutput(result.Stderr))
	}

	// For "ready" command, show coordination context AFTER bd output
	if result.IsReadyCommand {
		// Show apex context (what epics/features we're working on)
		if len(result.MyClaims) > 0 {
			// Collect unique apexes (sorted for deterministic output)
			apexes := make(map[string]string) // apex_id -> apex_title
			for _, claim := range result.MyClaims {
				if claim.ApexID != "" {
					if claim.ApexTitle != "" {
						apexes[claim.ApexID] = claim.ApexTitle
					} else {
						apexes[claim.ApexID] = claim.ApexID
					}
				}
			}
			if len(apexes) > 0 {
				apexIDs := make([]string, 0, len(apexes))
				for apexID := range apexes {
					apexIDs = append(apexIDs, apexID)
				}
				sort.Strings(apexIDs)
				sb.WriteString(FormatCoordinationHeader())
				sb.WriteString("\n## Your Current Epics\n")
				for _, apexID := range apexIDs {
					sb.WriteString(fmt.Sprintf("- %s \"%s\"\n", apexID, apexes[apexID]))
				}
			}

			// Show claims with stale warnings
			sb.WriteString(FormatCoordinationHeader())
			sb.WriteString("\n## Your Claims\n")
			sb.WriteString("Issues you have claimed and should complete:\n")
			hasStale := false
			for _, claim := range result.MyClaims {
				claimAge := formatTimeAgo(claim.ClaimedAt)
				staleIndicator := ""
				if isClaimStale(claim.ClaimedAt) {
					staleIndicator = " ⚠️ stale"
					hasStale = true
				}
				if claim.Title != "" {
					sb.WriteString(fmt.Sprintf("- %s \"%s\" — %s%s\n", claim.BeadID, claim.Title, claimAge, staleIndicator))
				} else {
					sb.WriteString(fmt.Sprintf("- %s — %s%s\n", claim.BeadID, claimAge, staleIndicator))
				}
			}
			if hasStale {
				sb.WriteString("  → Release stale claims: `bdh close <id> --reason \"releasing stale claim\"`\n")
			}
		} else if strings.TrimSpace(result.MyFocusApexID) != "" {
			sb.WriteString(FormatCoordinationHeader())
			sb.WriteString("\n## Your Focus\n")
			if strings.TrimSpace(result.MyFocusApexTitle) != "" {
				sb.WriteString(fmt.Sprintf("- %s \"%s\"\n", result.MyFocusApexID, result.MyFocusApexTitle))
			} else {
				sb.WriteString(fmt.Sprintf("- %s\n", result.MyFocusApexID))
			}
		}

		// Show team status (who's working on what)
		if len(result.TeamStatus) > 0 {
			limit := result.TeamStatusLimit
			if limit == 0 {
				limit = defaultReadyTeamLimit
			}
			teamStatus := result.TeamStatus
			if limit > 0 && len(teamStatus) > limit {
				teamStatus = teamStatus[:limit]
			}
			sb.WriteString(FormatCoordinationHeader())
			sb.WriteString("\n## Team Status\n")
			sb.WriteString("Check before claiming work to avoid conflicts:\n")
			for _, ws := range teamStatus {
				// Show focus apex if available
				if ws.FocusApexID != "" {
					if ws.FocusApexTitle != "" {
						sb.WriteString(fmt.Sprintf("- %s — focused on %s \"%s\"\n", ws.Alias, ws.FocusApexID, ws.FocusApexTitle))
					} else {
						sb.WriteString(fmt.Sprintf("- %s — focused on %s\n", ws.Alias, ws.FocusApexID))
					}
				} else if len(ws.Claims) > 0 {
					// Fall back to showing claims if no focus apex
					for _, claim := range ws.Claims {
						if claim.Title != "" {
							sb.WriteString(fmt.Sprintf("- %s — working on %s \"%s\"\n", ws.Alias, claim.BeadID, claim.Title))
						} else {
							sb.WriteString(fmt.Sprintf("- %s — working on %s\n", ws.Alias, claim.BeadID))
						}
					}
				}
			}
			if result.TeamStatusMore {
				sb.WriteString("  → More agents: `bdh :aweb who`\n")
			}
		}

		// Show active locks from OTHER agents so this agent knows what to avoid
		// Filter out own locks - those are shown in "Your File Reservations"
		var othersLocks []aweb.ReservationView
		for _, lock := range result.ReadyLocks {
			if lock.HolderAlias != result.MyAlias {
				othersLocks = append(othersLocks, lock)
			}
		}
		if len(othersLocks) > 0 {
			maxLocks := defaultReadyLocksLimit
			if maxLocks <= 0 {
				maxLocks = defaultReadyLocksLimit
			}
			sb.WriteString(FormatCoordinationHeader())
			sb.WriteString("\n## File Reservations\n")
			sb.WriteString("These files are locked by other agents. Do not edit them:\n")
			shown := othersLocks
			if len(shown) > maxLocks {
				shown = shown[:maxLocks]
			}
			now := time.Now()
			for _, lock := range shown {
				expiresIn := formatDuration(ttlRemainingSeconds(lock.ExpiresAt, now))
				owner := lock.HolderAlias
				if owner == "" {
					owner = "unknown"
				}
				sb.WriteString(fmt.Sprintf("- `%s` — %s (expires in %s)", lock.ResourceKey, owner, expiresIn))
				if reason, ok := lock.Metadata["reason"].(string); ok && strings.TrimSpace(reason) != "" {
					sb.WriteString(fmt.Sprintf(" \"%s\"", reason))
				}
				sb.WriteString("\n")
			}
			if len(othersLocks) > maxLocks {
				sb.WriteString(fmt.Sprintf("  → %d more locks: `bdh :aweb locks`\n", len(othersLocks)-maxLocks))
			}
		}
	}

	// Show related work in progress (after close command)
	if len(result.RelatedWork) > 0 {
		sb.WriteString("\nRELATED WORK IN PROGRESS:\n")
		for _, rw := range result.RelatedWork {
			if rw.Title != "" {
				sb.WriteString(fmt.Sprintf("  %s — %s — \"%s\" (%s)\n", rw.BeadID, rw.Alias, rw.Title, rw.Relation))
			} else {
				sb.WriteString(fmt.Sprintf("  %s — %s (%s)\n", rw.BeadID, rw.Alias, rw.Relation))
			}
		}
		sb.WriteString("\nConsider notifying related agents:\n")
		for _, rw := range result.RelatedWork {
			sb.WriteString(fmt.Sprintf("  → bdh :aweb mail send %s \"Finished work on related bead. Details: ...\"\n", rw.Alias))
		}
	}

	// Show sync stats (only if something was synced)
	if result.SyncStats != nil {
		stats := result.SyncStats
		if stats.Inserted > 0 || stats.Updated > 0 || stats.Deleted > 0 {
			sb.WriteString("\nSYNC: ")
			parts := []string{}
			if stats.Inserted > 0 {
				parts = append(parts, fmt.Sprintf("+%d inserted", stats.Inserted))
			}
			if stats.Updated > 0 {
				parts = append(parts, fmt.Sprintf("~%d updated", stats.Updated))
			}
			if stats.Deleted > 0 {
				parts = append(parts, fmt.Sprintf("-%d deleted", stats.Deleted))
			}
			sb.WriteString(strings.Join(parts, ", "))
			sb.WriteString("\n")
		}
	}

	// Show sync warning if any
	if result.SyncWarning != "" {
		sb.WriteString(fmt.Sprintf("\nWarning: %s\n", result.SyncWarning))
	}

	// YOUR RESERVED FILES section - show lock changes from this command
	reservedFiles := formatReservedFiles(result)
	if reservedFiles != "" {
		sb.WriteString(reservedFiles)
	}

	// NOTIFICATIONS are now printed by main.go at the end of every command

	return sb.String()
}

// formatReservedFiles formats the file reservation updates section.
// Shows lock changes from this command: locked, renewed, released, conflicts.
func formatReservedFiles(result *PassthroughResult) string {
	hasContent := result.AutoReserveWarning != "" ||
		len(result.AutoReserved) > 0 ||
		len(result.AutoRenewed) > 0 ||
		len(result.AutoReleased) > 0 ||
		len(result.AutoReserveConflicts) > 0

	if !hasContent {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(FormatCoordinationHeader())
	sb.WriteString("\n## Your File Reservations\n")

	if result.AutoReserveWarning != "" {
		sb.WriteString(fmt.Sprintf("⚠️ Warning: %s\n", result.AutoReserveWarning))
	}
	if len(result.AutoReserved) > 0 {
		sb.WriteString(fmt.Sprintf("You locked %d path(s):\n", len(result.AutoReserved)))
		for _, path := range result.AutoReserved {
			sb.WriteString(fmt.Sprintf("- `%s`\n", path))
		}
	}
	if len(result.AutoRenewed) > 0 {
		sb.WriteString(fmt.Sprintf("You renewed %d path(s):\n", len(result.AutoRenewed)))
		for _, path := range result.AutoRenewed {
			sb.WriteString(fmt.Sprintf("- `%s`\n", path))
		}
	}
	if len(result.AutoReleased) > 0 {
		sb.WriteString(fmt.Sprintf("You released %d path(s):\n", len(result.AutoReleased)))
		for _, path := range result.AutoReleased {
			sb.WriteString(fmt.Sprintf("- `%s`\n", path))
		}
	}
	if len(result.AutoReserveConflicts) > 0 {
		sb.WriteString("\n**CONFLICT: Do not edit these files** — held by other agents:\n")
		for _, conflict := range result.AutoReserveConflicts {
			expiresIn := formatDuration(conflict.RetryAfterSeconds)
			sb.WriteString(fmt.Sprintf("- `%s` — %s (expires in %s)\n", conflict.ResourceKey, conflict.HeldBy, expiresIn))
		}
		sb.WriteString("\nYour options:\n")
		sb.WriteString("- Coordinate: `bdh :aweb chat send <alias> \"Need <path>...\"`\n")
		sb.WriteString("- Stash your changes: `git stash`\n")
		sb.WriteString("- Wait for expiry\n")
	}

	return sb.String()
}

func rewriteBDHelpOutput(output string) string {
	if !strings.Contains(output, "\nUsage:\n  bd ") && !strings.Contains(output, "Usage:\n  bd ") {
		return output
	}

	return strings.NewReplacer(
		"Usage:\n  bd ", "Usage:\n  bdh ",
		"\nUsage:\n  bd ", "\nUsage:\n  bdh ",
		"\n  bd ", "\n  bdh ",
		"`bd ", "`bdh ",
		"'bd ", "'bdh ",
		"\"bd ", "\"bdh ",
		"Run `bd ", "Run `bdh ",
		"run `bd ", "run `bdh ",
		"Run 'bd ", "Run 'bdh ",
		"run 'bd ", "run 'bdh ",
	).Replace(output)
}

type passthroughJSON struct {
	Rejected        bool              `json:"rejected"`
	RejectionReason string            `json:"rejection_reason,omitempty"`
	Warning         string            `json:"warning,omitempty"`
	SyncWarning     string            `json:"sync_warning,omitempty"`
	SyncStats       *client.SyncStats `json:"sync_stats,omitempty"`
	SyncMode        string            `json:"sync_mode,omitempty"`

	BeadsInProgress []client.BeadInProgress `json:"beads_in_progress,omitempty"`

	AutoReserve *passthroughAutoReserveJSON `json:"auto_reserve,omitempty"`

	BDExitCode int             `json:"bd_exit_code"`
	BDStdout   json.RawMessage `json:"bd_stdout,omitempty"`
	BDText     string          `json:"bd_stdout_text,omitempty"`
	BDStderr   string          `json:"bd_stderr,omitempty"`

	ReadyContext *passthroughReadyContextJSON `json:"ready_context,omitempty"`
}

type passthroughAutoReserveJSON struct {
	Warning   string                `json:"warning,omitempty"`
	Reserved  []string              `json:"reserved,omitempty"`
	Renewed   []string              `json:"renewed,omitempty"`
	Released  []string              `json:"released,omitempty"`
	Conflicts []ReservationConflict `json:"conflicts,omitempty"`
}

type passthroughReadyContextJSON struct {
	MyClaims         []client.Claim         `json:"my_claims,omitempty"`
	MyFocusApexID    string                 `json:"my_focus_apex_id,omitempty"`
	MyFocusApexTitle string                 `json:"my_focus_apex_title,omitempty"`
	MyFocusApexType  string                 `json:"my_focus_apex_type,omitempty"`
	TeamStatus       []client.Workspace     `json:"team_status,omitempty"`
	TeamStatusLimit  int                    `json:"team_status_limit,omitempty"`
	TeamStatusMore   bool                   `json:"team_status_more,omitempty"`
	ActiveLocks      []aweb.ReservationView `json:"active_locks,omitempty"`
}

func formatPassthroughOutputJSON(result *PassthroughResult) string {
	stdoutTrimmed := strings.TrimSpace(result.Stdout)
	var bdJSON json.RawMessage
	var bdText string
	if stdoutTrimmed != "" && json.Valid([]byte(stdoutTrimmed)) {
		bdJSON = json.RawMessage(stdoutTrimmed)
	} else if stdoutTrimmed != "" {
		bdText = stdoutTrimmed
	}

	var autoReserve *passthroughAutoReserveJSON
	if result.AutoReserveWarning != "" || len(result.AutoReserved) > 0 || len(result.AutoRenewed) > 0 || len(result.AutoReleased) > 0 || len(result.AutoReserveConflicts) > 0 {
		autoReserve = &passthroughAutoReserveJSON{
			Warning:   result.AutoReserveWarning,
			Reserved:  result.AutoReserved,
			Renewed:   result.AutoRenewed,
			Released:  result.AutoReleased,
			Conflicts: result.AutoReserveConflicts,
		}
	}

	var readyContext *passthroughReadyContextJSON
	if result.IsReadyCommand {
		readyContext = &passthroughReadyContextJSON{
			MyClaims:         result.MyClaims,
			MyFocusApexID:    result.MyFocusApexID,
			MyFocusApexTitle: result.MyFocusApexTitle,
			MyFocusApexType:  result.MyFocusApexType,
			TeamStatus:       result.TeamStatus,
			TeamStatusLimit:  result.TeamStatusLimit,
			TeamStatusMore:   result.TeamStatusMore,
			ActiveLocks:      result.ReadyLocks,
		}
	}

	output := passthroughJSON{
		Rejected:        result.Rejected,
		RejectionReason: result.RejectionReason,
		Warning:         result.Warning,
		SyncWarning:     result.SyncWarning,
		SyncStats:       result.SyncStats,
		SyncMode:        result.SyncMode,
		BeadsInProgress: result.BeadsInProgress,
		AutoReserve:     autoReserve,
		BDExitCode:      result.ExitCode,
		BDStdout:        bdJSON,
		BDText:          bdText,
		BDStderr:        strings.TrimSpace(result.Stderr),
		ReadyContext:    readyContext,
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		// Last-resort fallback: keep stdout JSON-only even on marshal failures.
		return "{}\n"
	}
	return string(data) + "\n"
}
