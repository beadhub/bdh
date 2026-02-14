package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   ":version",
	Short: "Show bdh version",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Print(formatVersionOutput())
		checkLatestVersion(os.Stdout, "")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

// formatVersionOutput returns the version info string.
func formatVersionOutput() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("bdh %s\n", versionInfo.version))
	if versionInfo.commit != "" && versionInfo.commit != "none" {
		sb.WriteString(fmt.Sprintf("  commit: %s\n", versionInfo.commit))
	}
	if versionInfo.date != "" && versionInfo.date != "unknown" {
		sb.WriteString(fmt.Sprintf("  built:  %s\n", versionInfo.date))
	}
	return sb.String()
}
