package commands

import (
	"context"
	"errors"
	"fmt"
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

// runNative executes a command in native mode using the aweb /v1/tasks API.
// This is called when bd is not initialized in the workspace.
func runNative(aw *aweb.Client, args []string) (*bd.Result, error) {
	cmd, remaining, err := parseNativeCommand(args)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), nativeTimeout)
	defer cancel()

	var output string
	switch cmd {
	case "create":
		output, err = nativeCreate(ctx, aw, remaining)
	case "list":
		output, err = nativeList(ctx, aw, remaining)
	case "show":
		output, err = nativeShow(ctx, aw, remaining)
	case "update":
		output, err = nativeUpdate(ctx, aw, remaining)
	case "close":
		output, err = nativeClose(ctx, aw, remaining)
	case "ready":
		output, err = nativeReady(ctx, aw)
	case "blocked":
		output, err = nativeBlocked(ctx, aw)
	case "dep":
		output, err = nativeDep(ctx, aw, remaining)
	case "sync":
		output = "Native mode: tasks are stored server-side, no local sync needed.\n"
	case "stats":
		output, err = nativeStats(ctx, aw)
	case "reopen":
		output, err = nativeReopen(ctx, aw, remaining)
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

func nativeCreate(ctx context.Context, aw *aweb.Client, args []string) (string, error) {
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

	return fmt.Sprintf("✓ Created %s: %s\n", task.TaskRef, task.Title), nil
}

func nativeList(ctx context.Context, aw *aweb.Client, args []string) (string, error) {
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

	resp, err := aw.TaskList(ctx, params)
	if err != nil {
		return "", fmt.Errorf("listing tasks: %w", err)
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

func nativeShow(ctx context.Context, aw *aweb.Client, args []string) (string, error) {
	ref := positionalArg(args)
	if ref == "" {
		return "", fmt.Errorf("usage: show <ref>")
	}

	task, err := aw.TaskGet(ctx, ref)
	if err != nil {
		return "", fmt.Errorf("getting task %s: %w", ref, err)
	}

	return formatTaskDetail(task), nil
}

func nativeUpdate(ctx context.Context, aw *aweb.Client, args []string) (string, error) {
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

	output := fmt.Sprintf("✓ Updated %s: %s\n", resp.TaskRef, resp.Title)
	if len(resp.AutoClosed) > 0 {
		output += fmt.Sprintf("\nAuto-closed %d descendant(s):\n", len(resp.AutoClosed))
		for _, t := range resp.AutoClosed {
			output += fmt.Sprintf("  ✓ %s: %s\n", t.TaskRef, t.Title)
		}
	}
	return output, nil
}

func nativeClose(ctx context.Context, aw *aweb.Client, args []string) (string, error) {
	refs := allPositionalArgs(args)
	if len(refs) == 0 {
		return "", fmt.Errorf("usage: close <ref> [<ref2> ...]")
	}

	reason := parseFlagValue(args, "--reason")

	var sb strings.Builder
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

		sb.WriteString(fmt.Sprintf("✓ Closed %s: %s\n", resp.TaskRef, resp.Title))
		if len(resp.AutoClosed) > 0 {
			for _, t := range resp.AutoClosed {
				sb.WriteString(fmt.Sprintf("  ✓ Auto-closed %s: %s\n", t.TaskRef, t.Title))
			}
		}
	}

	if failures > 0 && failures == len(refs) {
		// All closes failed — report as error
		return "", fmt.Errorf("%s", strings.TrimSpace(sb.String()))
	}
	return sb.String(), nil
}

func nativeReady(ctx context.Context, aw *aweb.Client) (string, error) {
	resp, err := aw.TaskListReady(ctx)
	if err != nil {
		return "", fmt.Errorf("listing ready tasks: %w", err)
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

func nativeBlocked(ctx context.Context, aw *aweb.Client) (string, error) {
	// List all open tasks, then filter to those with open blockers
	resp, err := aw.TaskList(ctx, aweb.TaskListParams{Status: "open"})
	if err != nil {
		return "", fmt.Errorf("listing tasks: %w", err)
	}

	// For each open task, check if it has open blockers
	var blocked []string
	var truncated bool
	var checked int
	for _, t := range resp.Tasks {
		task, err := aw.TaskGet(ctx, t.TaskRef)
		if err != nil {
			if ctx.Err() != nil {
				truncated = true
				break
			}
			continue
		}
		checked++
		var openBlockers []string
		for _, dep := range task.BlockedBy {
			if dep.Status != "closed" {
				openBlockers = append(openBlockers, dep.TaskRef)
			}
		}
		if len(openBlockers) > 0 {
			icon := priorityIcon(t.Priority)
			blocked = append(blocked, fmt.Sprintf("%s %s [%s P%d] [%s] - %s (blocked by: %s)",
				icon, t.TaskRef, icon, t.Priority, t.TaskType, t.Title,
				strings.Join(openBlockers, ", ")))
		}
	}

	if len(blocked) == 0 && !truncated {
		return "No blocked tasks.\n", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Blocked tasks (%d):\n\n", len(blocked)))
	for _, line := range blocked {
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	if truncated {
		sb.WriteString(fmt.Sprintf("\nWarning: results truncated (checked %d of %d tasks before timeout)\n",
			checked, len(resp.Tasks)))
	}
	return sb.String(), nil
}

func nativeDep(ctx context.Context, aw *aweb.Client, args []string) (string, error) {
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
		return fmt.Sprintf("✓ Removed dependency: %s no longer depends on %s\n", ref, depRef), nil

	default:
		return "", fmt.Errorf("unknown dep subcommand %q (use add or remove)", subcmd)
	}
}

func nativeStats(ctx context.Context, aw *aweb.Client) (string, error) {
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

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Total: %d\n", total))
	sb.WriteString(fmt.Sprintf("  Open: %d\n", open))
	sb.WriteString(fmt.Sprintf("  In progress: %d\n", inProgress))
	sb.WriteString(fmt.Sprintf("  Closed: %d\n", closed))
	return sb.String(), nil
}

func nativeReopen(ctx context.Context, aw *aweb.Client, args []string) (string, error) {
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

	return fmt.Sprintf("✓ Reopened %s: %s\n", resp.TaskRef, resp.Title), nil
}

