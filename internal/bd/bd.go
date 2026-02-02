// Package bd handles execution of the bd (beads) command.
//
// This package provides faithful argument passthrough to bd,
// ensuring all arguments are passed exactly as received.
package bd

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
)

// Runner executes bd commands.
type Runner struct {
	// BdPath is the path to the bd executable (defaults to "bd" in PATH).
	BdPath string
}

// New creates a new bd runner.
func New() *Runner {
	return &Runner{
		BdPath: "bd",
	}
}

// Result contains the result of running a bd command.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Run executes bd with the given arguments.
// Arguments are passed through faithfully without modification.
func (r *Runner) Run(ctx context.Context, args []string) (*Result, error) {
	cmd := exec.CommandContext(ctx, r.BdPath, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	result := &Result{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		return nil, err
	}

	result.ExitCode = 0
	return result, nil
}

func commandFromArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}

	i := 0
	for i < len(args) {
		arg := args[i]
		if arg == "--" {
			i++
			break
		}
		if !strings.HasPrefix(arg, "-") {
			break
		}

		if strings.HasPrefix(arg, "--db=") || strings.HasPrefix(arg, "--actor=") || strings.HasPrefix(arg, "--lock-timeout=") {
			i++
			continue
		}
		switch arg {
		case "--db", "--actor", "--lock-timeout":
			// Skip flag + its value (if present and not another flag).
			i++
			if i < len(args) && !strings.HasPrefix(args[i], "-") {
				i++
			}
			continue
		default:
			i++
			continue
		}
	}

	if i >= len(args) {
		return ""
	}
	return args[i]
}

// IsMutationCommand returns true if the command modifies state
// and should trigger a sync after execution.
func IsMutationCommand(args []string) bool {
	switch commandFromArgs(args) {
	case "create", "close", "update", "delete", "reopen":
		return true
	case "dep":
		// All dep commands trigger sync to be conservative.
		// Mutation subcommands (add/remove/relate/unrelate) need sync.
		// Read-only subcommands (list/tree/cycles) get synced but have no changes.
		return true
	case "sync":
		// `bd sync` updates the canonical JSONL export and may commit it; ensure
		// BeadHub sees the latest JSONL even if earlier uploads were skipped.
		return true
	default:
		return false
	}
}
