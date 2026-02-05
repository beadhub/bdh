package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

var helpCmd = &cobra.Command{
	Use:   ":help",
	Short: "Show bdh help",
	Long:  "Show bdh help (without bd help).",
	Run: func(cmd *cobra.Command, args []string) {
		_ = rootCmd.Help()
		fmt.Println("\nAll bd commands are also available (e.g., bdh ready, bdh create, bdh close).")
		fmt.Println("Run 'bdh --help' to see the full list.")
	},
}
