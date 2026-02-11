package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/beadhub/bdh/internal/config"
)

var listRolesCmd = &cobra.Command{
	Use:   ":list-roles",
	Short: "List available roles from project policy",
	RunE:  runListRoles,
}

func init() {
	rootCmd.AddCommand(listRolesCmd)
}

func runListRoles(cmd *cobra.Command, args []string) error {
	// Get beadhub URL: config > env > default
	beadhubURL := defaultBeadhubURL
	if cfg, err := config.Load(); err == nil {
		beadhubURL = cfg.BeadhubURL
	} else {
		beadhubURL = resolveConfig("", "BEADHUB_URL", defaultBeadhubURL)
	}

	roles, err := fetchAvailablePolicyRoles(beadhubURL)
	if err != nil {
		return err
	}

	if len(roles) == 0 {
		fmt.Println("No roles defined in project policy.")
		return nil
	}

	for _, r := range roles {
		fmt.Println(r)
	}
	return nil
}
