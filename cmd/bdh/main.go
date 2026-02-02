// bdh - BeadHub wrapper for bd (beads)
//
// Wraps the bd command with coordination:
// 1. Notifies BeadHub what you're doing (for visibility)
// 2. Runs bd with all provided arguments
// 3. Syncs .beads/issues.jsonl to BeadHub after mutation commands
//
// bd always runs, even if the server is down. The one exception: claiming
// a bead another agent has (use --:jump-in to override).
package main

import (
	"fmt"
	"os"

	"github.com/beadhub/bdh/internal/commands"
)

// Version information (set by goreleaser)
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	commands.SetVersionInfo(version, commit, date)

	err := commands.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	// Print notifications at the end of every command
	commands.PrintNotifications(os.Stderr)
	if err != nil {
		os.Exit(1)
	}
}
