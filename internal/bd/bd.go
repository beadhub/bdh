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

// CommandFromArgs extracts the bd command name from args, skipping global
// flags like --db, --actor, and --lock-timeout.
func CommandFromArgs(args []string) string {
	cmd, _ := CommandIndexFromArgs(args)
	return cmd
}

// CommandIndexFromArgs extracts the bd command name and its index in args,
// skipping global flags like --db, --actor, and --lock-timeout.
// Returns ("", -1) if no command is found.
func CommandIndexFromArgs(args []string) (string, int) {
	if len(args) == 0 {
		return "", -1
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
		return "", -1
	}
	return args[i], i
}

// IsMutationCommand returns true if the command modifies state
// and should trigger a sync after execution.
func IsMutationCommand(args []string) bool {
	switch CommandFromArgs(args) {
	case "create", "close", "update", "delete", "reopen":
		return true
	case "dep", "comment":
		// All dep/comment commands trigger sync to be conservative.
		return true
	case "sync":
		// `bd sync` updates the canonical JSONL export and may commit it; ensure
		// BeadHub sees the latest JSONL even if earlier uploads were skipped.
		return true
	default:
		return false
	}
}
