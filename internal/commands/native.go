package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	aweb "github.com/awebai/aw"
	"github.com/beadhub/bdh/internal/bd"
)

// nativeCommands lists the bd commands that have native implementations
// backed by the aweb /v1/tasks API.
var nativeCommands = map[string]bool{
	"create":  true,
	"list":    true,
	"show":    true,
	"update":  true,
	"close":   true,
	"ready":   true,
	"blocked": true,
	"dep":     true,
	"comment": true,
	"sync":    true,
	"stats":   true,
	"reopen":  true,
}

// isNativeCommand returns true if the command name has a native implementation.
func isNativeCommand(cmd string) bool {
	return nativeCommands[cmd]
}

// parseNativeCommand extracts the command name and remaining args.
// Returns an error if the command is not supported in native mode.
func parseNativeCommand(args []string) (cmd string, remaining []string, err error) {
	if len(args) == 0 {
		return "", nil, fmt.Errorf("no command provided")
	}

	// Use bd's arg parser to skip global flags (--db, --actor, etc.)
	// and find the actual command at the correct index.
	cmd, idx := bd.CommandIndexFromArgs(args)
	if cmd == "" || idx < 0 {
		return "", nil, fmt.Errorf("could not determine command from args")
	}

	if !isNativeCommand(cmd) {
		return "", nil, fmt.Errorf("command %q is not available in native mode (bd is not initialized — run 'bdh :init' or use a supported command)", cmd)
	}

	remaining = args[idx+1:]
	return cmd, remaining, nil
}

const nativeTimeout = 10 * time.Second

// nativeAuth carries authentication for direct HTTP calls to endpoints
// not yet available in the aweb Go SDK.
type nativeAuth struct {
	baseURL string
	apiKey  string
}

// blockedTaskEntry represents a task with its blockers, as returned by
// GET /v1/tasks/blocked.
type blockedTaskEntry struct {
	TaskRef   string             `json:"task_ref"`
	Title     string             `json:"title"`
	Status    string             `json:"status"`
	Priority  int                `json:"priority"`
	TaskType  string             `json:"task_type"`
	BlockedBy []aweb.TaskDepView `json:"blocked_by"`
}

type blockedTasksResponse struct {
	Tasks []blockedTaskEntry `json:"tasks"`
}

// runNative executes a command in native mode using the aweb /v1/tasks API.
// This is called when bd is not initialized in the workspace.
func runNative(aw *aweb.Client, args []string) (*bd.Result, error) {
	return runNativeWithAuth(aw, args, nil)
}

// runNativeWithAuth is like runNative but also accepts auth info for direct
// HTTP calls to endpoints not yet in the aweb SDK.
func runNativeWithAuth(aw *aweb.Client, args []string, auth *nativeAuth) (*bd.Result, error) {
	jsonMode := hasFlag(args, "--json")

	cmd, remaining, err := parseNativeCommand(args)
	if err != nil {
		return nil, err
	}

	if jsonMode {
		remaining = removeFlag(remaining, "--json")
	}

	ctx, cancel := context.WithTimeout(context.Background(), nativeTimeout)
	defer cancel()

	var output string
	switch cmd {
	case "create":
		output, err = nativeCreate(ctx, aw, remaining, jsonMode)
	case "list":
		output, err = nativeList(ctx, aw, remaining, jsonMode)
	case "show":
		output, err = nativeShow(ctx, aw, remaining, jsonMode)
	case "update":
		output, err = nativeUpdate(ctx, aw, remaining, jsonMode)
	case "close":
		output, err = nativeClose(ctx, aw, remaining, jsonMode)
	case "ready":
		output, err = nativeReady(ctx, aw, jsonMode)
	case "blocked":
		output, err = nativeBlocked(ctx, aw, auth, jsonMode)
	case "dep":
		output, err = nativeDep(ctx, aw, remaining, jsonMode)
	case "comment":
		output, err = nativeComment(ctx, aw, remaining, jsonMode)
	case "sync":
		if jsonMode {
			output, err = jsonOutput(map[string]string{"message": "tasks are stored server-side, no local sync needed"})
		} else {
			output = "Native mode: tasks are stored server-side, no local sync needed.\n"
		}
	case "stats":
		output, err = nativeStats(ctx, aw, jsonMode)
	case "reopen":
		output, err = nativeReopen(ctx, aw, remaining, jsonMode)
	default:
		return nil, fmt.Errorf("native %q command not implemented", cmd)
	}

	if err != nil {
		return &bd.Result{
			Stderr:   err.Error() + "\n",
			ExitCode: 1,
		}, nil
	}

	return &bd.Result{
		Stdout:   output,
		ExitCode: 0,
	}, nil
}

// --- Arg parsing helpers ---

// parseFlagValue extracts the value of a flag from args.
// Supports --flag=value and --flag value syntax.
func parseFlagValue(args []string, flag string) string {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(arg, flag+"=") {
			return strings.TrimPrefix(arg, flag+"=")
		}
	}
	return ""
}

// hasFlag returns true if a boolean flag is present in args.
func hasFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag {
			return true
		}
	}
	return false
}

// removeFlag returns args with all occurrences of flag removed.
func removeFlag(args []string, flag string) []string {
	var result []string
	for _, arg := range args {
		if arg != flag {
			result = append(result, arg)
		}
	}
	return result
}

// jsonOutput marshals data as compact JSON with a trailing newline.
func jsonOutput(data any) (string, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

// knownValueFlags lists flags that take a subsequent value argument.
// Boolean flags (--json, --verbose, etc.) are NOT listed here.
var knownValueFlags = map[string]bool{
	"--title": true, "--description": true, "--notes": true, "--design": true,
	"--type": true, "--priority": true, "--status": true, "--assignee": true,
	"--labels": true, "--reason": true, "--limit": true,
	"--db": true, "--actor": true, "--lock-timeout": true,
}

// positionalArg returns the first non-flag argument (the ref/id).
func positionalArg(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			// Skip flag value only for known value-taking flags
			if !strings.Contains(arg, "=") && knownValueFlags[arg] && i+1 < len(args) {
				i++
			}
			continue
		}
		return arg
	}
	return ""
}

// allPositionalArgs returns all non-flag arguments.
func allPositionalArgs(args []string) []string {
	var result []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			if !strings.Contains(arg, "=") && knownValueFlags[arg] && i+1 < len(args) {
				i++
			}
			continue
		}
		result = append(result, arg)
	}
	return result
}

// --- Output formatting helpers ---

func priorityIcon(p int) string {
	if p <= 2 {
		return "●"
	}
	return "○"
}

func formatTaskLine(t aweb.TaskSummary) string {
	icon := priorityIcon(t.Priority)
	return fmt.Sprintf("%s %s [%s P%d] [%s] - %s",
		icon, t.TaskRef, icon, t.Priority, t.TaskType, t.Title)
}

func formatTaskDetail(t *aweb.Task) string {
	var sb strings.Builder

	icon := priorityIcon(t.Priority)
	statusLabel := strings.ToUpper(t.Status)
	sb.WriteString(fmt.Sprintf("%s %s · %s   [%s P%d · %s]\n",
		icon, t.TaskRef, t.Title, icon, t.Priority, statusLabel))
	sb.WriteString(fmt.Sprintf("Type: %s\n", t.TaskType))
	sb.WriteString(fmt.Sprintf("Created: %s · Updated: %s\n", formatDate(t.CreatedAt), formatDate(t.UpdatedAt)))

	if t.Description != "" {
		sb.WriteString(fmt.Sprintf("\nDESCRIPTION\n%s\n", t.Description))
	}

	if t.Notes != "" {
		sb.WriteString(fmt.Sprintf("\nNOTES\n%s\n", t.Notes))
	}

	if len(t.BlockedBy) > 0 {
		sb.WriteString("\nBLOCKED BY\n")
		for _, dep := range t.BlockedBy {
			sb.WriteString(fmt.Sprintf("  → ○ %s: %s [%s]\n", dep.TaskRef, dep.Title, dep.Status))
		}
	}

	if len(t.Blocks) > 0 {
		sb.WriteString("\nBLOCKS\n")
		for _, dep := range t.Blocks {
			sb.WriteString(fmt.Sprintf("  ← ○ %s: %s [%s]\n", dep.TaskRef, dep.Title, dep.Status))
		}
	}

	return sb.String()
}

func formatDate(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return t.Format("2006-01-02")
}

// --- Command implementations ---

func nativeCreate(ctx context.Context, aw *aweb.Client, args []string, jsonMode bool) (string, error) {
	title := parseFlagValue(args, "--title")
	if title == "" {
		return "", fmt.Errorf("--title is required")
	}

	req := &aweb.TaskCreateRequest{
		Title:       title,
		Description: parseFlagValue(args, "--description"),
		Notes:       parseFlagValue(args, "--notes"),
		TaskType:    parseFlagValue(args, "--type"),
	}

	if p := parseFlagValue(args, "--priority"); p != "" {
		// Strip P/p prefix if present (e.g., "P2" -> "2")
		p = strings.TrimPrefix(strings.TrimPrefix(p, "P"), "p")
		if pv, err := strconv.Atoi(p); err == nil {
			req.Priority = pv
		}
	}

	if labels := parseFlagValue(args, "--labels"); labels != "" {
		req.Labels = strings.Split(labels, ",")
	}

	task, err := aw.TaskCreate(ctx, req)
	if err != nil {
		return "", fmt.Errorf("creating task: %w", err)
	}

	if jsonMode {
		return jsonOutput(task)
	}
	return fmt.Sprintf("✓ Created %s: %s\n", task.TaskRef, task.Title), nil
}

func nativeList(ctx context.Context, aw *aweb.Client, args []string, jsonMode bool) (string, error) {
	params := aweb.TaskListParams{
		Status:   parseFlagValue(args, "--status"),
		TaskType: parseFlagValue(args, "--type"),
	}
	if assignee := parseFlagValue(args, "--assignee"); assignee != "" {
		params.AssigneeAgentID = assignee
	}
	if p := parseFlagValue(args, "--priority"); p != "" {
		p = strings.TrimPrefix(strings.TrimPrefix(p, "P"), "p")
		if pv, err := strconv.Atoi(p); err == nil {
			params.Priority = &pv
		}
	}
	if labels := parseFlagValue(args, "--labels"); labels != "" {
		params.Labels = strings.Split(labels, ",")
	}

	resp, err := aw.TaskList(ctx, params)
	if err != nil {
		return "", fmt.Errorf("listing tasks: %w", err)
	}

	if jsonMode {
		return jsonOutput(resp)
	}

	if len(resp.Tasks) == 0 {
		return "No tasks found.\n", nil
	}

	var sb strings.Builder
	for _, t := range resp.Tasks {
		sb.WriteString(formatTaskLine(t))
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

func nativeShow(ctx context.Context, aw *aweb.Client, args []string, jsonMode bool) (string, error) {
	ref := positionalArg(args)
	if ref == "" {
		return "", fmt.Errorf("usage: show <ref>")
	}

	task, err := aw.TaskGet(ctx, ref)
	if err != nil {
		return "", fmt.Errorf("getting task %s: %w", ref, err)
	}

	if jsonMode {
		return jsonOutput(task)
	}
	return formatTaskDetail(task), nil
}

func nativeUpdate(ctx context.Context, aw *aweb.Client, args []string, jsonMode bool) (string, error) {
	ref := positionalArg(args)
	if ref == "" {
		return "", fmt.Errorf("usage: update <ref> [--status=...] [--title=...] [--description=...] [--notes=...]")
	}

	req := &aweb.TaskUpdateRequest{}
	hasUpdate := false

	if v := parseFlagValue(args, "--status"); v != "" {
		req.Status = &v
		hasUpdate = true
	}
	if v := parseFlagValue(args, "--title"); v != "" {
		req.Title = &v
		hasUpdate = true
	}
	if v := parseFlagValue(args, "--description"); v != "" {
		req.Description = &v
		hasUpdate = true
	}
	if v := parseFlagValue(args, "--notes"); v != "" {
		req.Notes = &v
		hasUpdate = true
	}
	if v := parseFlagValue(args, "--type"); v != "" {
		req.TaskType = &v
		hasUpdate = true
	}
	if v := parseFlagValue(args, "--assignee"); v != "" {
		req.AssigneeAgentID = &v
		hasUpdate = true
	}
	if p := parseFlagValue(args, "--priority"); p != "" {
		p = strings.TrimPrefix(strings.TrimPrefix(p, "P"), "p")
		if pv, err := strconv.Atoi(p); err == nil {
			req.Priority = &pv
			hasUpdate = true
		}
	}

	if !hasUpdate {
		return "", fmt.Errorf("no fields to update — use --status, --title, --description, --notes, --type, --priority, or --assignee")
	}

	resp, err := aw.TaskUpdate(ctx, ref, req)
	if err != nil {
		var held *aweb.TaskHeldError
		if errors.As(err, &held) {
			return "", fmt.Errorf("task %s is held by another agent: %s", ref, held.Detail)
		}
		return "", fmt.Errorf("updating task %s: %w", ref, err)
	}

	if jsonMode {
		return jsonOutput(resp)
	}

	output := fmt.Sprintf("✓ Updated %s: %s\n", resp.TaskRef, resp.Title)
	if len(resp.AutoClosed) > 0 {
		output += fmt.Sprintf("\nAuto-closed %d descendant(s):\n", len(resp.AutoClosed))
		for _, t := range resp.AutoClosed {
			output += fmt.Sprintf("  ✓ %s: %s\n", t.TaskRef, t.Title)
		}
	}
	return output, nil
}

func nativeClose(ctx context.Context, aw *aweb.Client, args []string, jsonMode bool) (string, error) {
	refs := allPositionalArgs(args)
	if len(refs) == 0 {
		return "", fmt.Errorf("usage: close <ref> [<ref2> ...]")
	}

	reason := parseFlagValue(args, "--reason")

	var sb strings.Builder
	var jsonResults []aweb.TaskUpdateResponse
	var failures int
	for _, ref := range refs {
		status := "closed"
		req := &aweb.TaskUpdateRequest{Status: &status}
		if reason != "" {
			req.Notes = &reason
		}

		resp, err := aw.TaskUpdate(ctx, ref, req)
		if err != nil {
			sb.WriteString(fmt.Sprintf("✗ Failed to close %s: %v\n", ref, err))
			failures++
			continue
		}

		if jsonMode {
			jsonResults = append(jsonResults, *resp)
		}
		sb.WriteString(fmt.Sprintf("✓ Closed %s: %s\n", resp.TaskRef, resp.Title))
		if len(resp.AutoClosed) > 0 {
			for _, t := range resp.AutoClosed {
				sb.WriteString(fmt.Sprintf("  ✓ Auto-closed %s: %s\n", t.TaskRef, t.Title))
			}
		}
	}

	if failures > 0 && failures == len(refs) {
		return "", fmt.Errorf("%s", strings.TrimSpace(sb.String()))
	}

	if jsonMode {
		return jsonOutput(jsonResults)
	}
	return sb.String(), nil
}

func nativeReady(ctx context.Context, aw *aweb.Client, jsonMode bool) (string, error) {
	resp, err := aw.TaskListReady(ctx)
	if err != nil {
		return "", fmt.Errorf("listing ready tasks: %w", err)
	}

	if jsonMode {
		return jsonOutput(resp)
	}

	if len(resp.Tasks) == 0 {
		return "No ready tasks (all tasks are blocked or closed).\n", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📋 Ready work (%d issues with no blockers):\n\n", len(resp.Tasks)))
	for i, t := range resp.Tasks {
		sb.WriteString(fmt.Sprintf("%d. [%s P%d] [%s] %s: %s\n",
			i+1, priorityIcon(t.Priority), t.Priority, t.TaskType, t.TaskRef, t.Title))
	}
	return sb.String(), nil
}

func nativeBlocked(ctx context.Context, aw *aweb.Client, auth *nativeAuth, jsonMode bool) (string, error) {
	resp, err := fetchBlockedTasks(ctx, auth)
	if err != nil {
		return "", fmt.Errorf("listing blocked tasks: %w", err)
	}

	if jsonMode {
		return jsonOutput(resp)
	}

	if len(resp.Tasks) == 0 {
		return "No blocked tasks.\n", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Blocked tasks (%d):\n\n", len(resp.Tasks)))
	for _, t := range resp.Tasks {
		var blockerRefs []string
		for _, dep := range t.BlockedBy {
			blockerRefs = append(blockerRefs, dep.TaskRef)
		}
		icon := priorityIcon(t.Priority)
		sb.WriteString(fmt.Sprintf("%s %s [%s P%d] [%s] - %s (blocked by: %s)\n",
			icon, t.TaskRef, icon, t.Priority, t.TaskType, t.Title,
			strings.Join(blockerRefs, ", ")))
	}
	return sb.String(), nil
}

// fetchBlockedTasks calls GET /v1/tasks/blocked directly.
// This is a temporary workaround until the aw Go SDK adds TaskListBlocked.
func fetchBlockedTasks(ctx context.Context, auth *nativeAuth) (*blockedTasksResponse, error) {
	if auth == nil {
		return nil, fmt.Errorf("authentication required for blocked tasks endpoint")
	}

	url := strings.TrimRight(auth.baseURL, "/") + "/v1/tasks/blocked"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+auth.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var out blockedTasksResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &out, nil
}

func nativeDep(ctx context.Context, aw *aweb.Client, args []string, jsonMode bool) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("usage: dep add|remove <ref> <depends-on>")
	}

	subcmd := args[0]
	switch subcmd {
	case "add":
		if len(args) < 3 {
			return "", fmt.Errorf("usage: dep add <ref> <depends-on>")
		}
		ref, dependsOn := args[1], args[2]
		err := aw.TaskAddDep(ctx, ref, &aweb.TaskAddDepRequest{DependsOn: dependsOn})
		if err != nil {
			return "", fmt.Errorf("adding dependency: %w", err)
		}
		if jsonMode {
			return jsonOutput(map[string]string{"ref": ref, "depends_on": dependsOn})
		}
		return fmt.Sprintf("✓ %s now depends on %s\n", ref, dependsOn), nil

	case "remove":
		if len(args) < 3 {
			return "", fmt.Errorf("usage: dep remove <ref> <dep-ref>")
		}
		ref, depRef := args[1], args[2]
		err := aw.TaskRemoveDep(ctx, ref, depRef)
		if err != nil {
			return "", fmt.Errorf("removing dependency: %w", err)
		}
		if jsonMode {
			return jsonOutput(map[string]string{"ref": ref, "removed": depRef})
		}
		return fmt.Sprintf("✓ Removed dependency: %s no longer depends on %s\n", ref, depRef), nil

	case "list":
		if len(args) < 2 {
			return "", fmt.Errorf("usage: dep list <ref>")
		}
		ref := args[1]
		task, err := aw.TaskGet(ctx, ref)
		if err != nil {
			return "", fmt.Errorf("getting task %s: %w", ref, err)
		}
		if jsonMode {
			return jsonOutput(map[string][]aweb.TaskDepView{
				"blocked_by": task.BlockedBy,
				"blocks":     task.Blocks,
			})
		}
		if len(task.BlockedBy) == 0 && len(task.Blocks) == 0 {
			return "No dependencies.\n", nil
		}
		var sb strings.Builder
		if len(task.BlockedBy) > 0 {
			sb.WriteString("Blocked by:\n")
			for _, dep := range task.BlockedBy {
				sb.WriteString(fmt.Sprintf("  → %s: %s [%s]\n", dep.TaskRef, dep.Title, dep.Status))
			}
		}
		if len(task.Blocks) > 0 {
			sb.WriteString("Blocks:\n")
			for _, dep := range task.Blocks {
				sb.WriteString(fmt.Sprintf("  ← %s: %s [%s]\n", dep.TaskRef, dep.Title, dep.Status))
			}
		}
		return sb.String(), nil

	default:
		return "", fmt.Errorf("unknown dep subcommand %q (use add, remove, or list)", subcmd)
	}
}

func nativeComment(ctx context.Context, aw *aweb.Client, args []string, jsonMode bool) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("usage: comment add|list <ref> [body]")
	}

	subcmd := args[0]
	switch subcmd {
	case "add":
		if len(args) < 3 {
			return "", fmt.Errorf("usage: comment add <ref> <body>")
		}
		ref, body := args[1], args[2]
		comment, err := aw.TaskCommentCreate(ctx, ref, &aweb.TaskCommentCreateRequest{Body: body})
		if err != nil {
			return "", fmt.Errorf("adding comment to %s: %w", ref, err)
		}
		if jsonMode {
			return jsonOutput(comment)
		}
		return fmt.Sprintf("✓ Added comment to %s\n", ref), nil

	case "list":
		if len(args) < 2 {
			return "", fmt.Errorf("usage: comment list <ref>")
		}
		ref := args[1]
		resp, err := aw.TaskCommentList(ctx, ref)
		if err != nil {
			return "", fmt.Errorf("listing comments for %s: %w", ref, err)
		}
		if jsonMode {
			return jsonOutput(resp)
		}
		if len(resp.Comments) == 0 {
			return "No comments.\n", nil
		}
		var sb strings.Builder
		for _, c := range resp.Comments {
			author := "(unknown)"
			if c.AuthorAgentID != nil {
				author = *c.AuthorAgentID
			}
			sb.WriteString(fmt.Sprintf("[%s] %s:\n  %s\n", formatDate(c.CreatedAt), author, c.Body))
		}
		return sb.String(), nil

	default:
		return "", fmt.Errorf("unknown comment subcommand %q (use add or list)", subcmd)
	}
}

func nativeStats(ctx context.Context, aw *aweb.Client, jsonMode bool) (string, error) {
	// Fetch all tasks to compute stats
	allResp, err := aw.TaskList(ctx, aweb.TaskListParams{})
	if err != nil {
		return "", fmt.Errorf("listing tasks: %w", err)
	}

	var open, inProgress, closed, total int
	for _, t := range allResp.Tasks {
		total++
		switch t.Status {
		case "open":
			open++
		case "in_progress":
			inProgress++
		case "closed":
			closed++
		}
	}

	if jsonMode {
		return jsonOutput(map[string]int{
			"total":       total,
			"open":        open,
			"in_progress": inProgress,
			"closed":      closed,
		})
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Total: %d\n", total))
	sb.WriteString(fmt.Sprintf("  Open: %d\n", open))
	sb.WriteString(fmt.Sprintf("  In progress: %d\n", inProgress))
	sb.WriteString(fmt.Sprintf("  Closed: %d\n", closed))
	return sb.String(), nil
}

func nativeReopen(ctx context.Context, aw *aweb.Client, args []string, jsonMode bool) (string, error) {
	ref := positionalArg(args)
	if ref == "" {
		return "", fmt.Errorf("usage: reopen <ref>")
	}

	status := "open"
	resp, err := aw.TaskUpdate(ctx, ref, &aweb.TaskUpdateRequest{Status: &status})
	if err != nil {
		var held *aweb.TaskHeldError
		if errors.As(err, &held) {
			return "", fmt.Errorf("task %s is held by another agent: %s", ref, held.Detail)
		}
		return "", fmt.Errorf("reopening task %s: %w", ref, err)
	}

	if jsonMode {
		return jsonOutput(resp)
	}
	return fmt.Sprintf("✓ Reopened %s: %s\n", resp.TaskRef, resp.Title), nil
}

