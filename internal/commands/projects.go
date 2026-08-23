package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

const projectsDashboardURL = "https://app.beadhub.ai"

// Project administration in BeadHub Cloud requires a signed-in human session.
// A bdh project API key is intentionally not a substitute for that session, so
// retaining the old HTTP implementation only produced a misleading 401.
// Keep a hidden compatibility tombstone so existing invocations fail locally
// with an actionable explanation instead of making an unauthenticated request.
var projectsCmd = &cobra.Command{
	Use:                ":projects",
	Hidden:             true,
	DisableFlagParsing: true,
	Args:               cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return projectsUnsupportedError()
	},
}

func projectsUnsupportedError() error {
	return fmt.Errorf(
		"bdh :projects is no longer supported: hosted project management requires "+
			"a signed-in human session; use the BeadHub dashboard at %s",
		projectsDashboardURL,
	)
}
