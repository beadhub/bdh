package commands

import (
	"fmt"

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
		return "", nil, fmt.Errorf("command %q is not available in native mode (bd is not initialized — run 'bd init' or use a supported command)", cmd)
	}

	remaining = args[idx+1:]
	return cmd, remaining, nil
}

// runNative executes a command in native mode using the aweb /v1/tasks API.
// This is called when bd is not initialized in the workspace.
func runNative(args []string) (*bd.Result, error) {
	cmd, _, err := parseNativeCommand(args)
	if err != nil {
		return nil, err
	}

	// TODO: dispatch to individual command handlers once aw SDK Task methods are available
	return nil, fmt.Errorf("native %q command not yet implemented (waiting for aw SDK task methods)", cmd)
}
