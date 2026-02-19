package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/beadhub/bdh/internal/beads"
)

var forceSyncCmd = &cobra.Command{
	Use:   ":force-sync",
	Short: "Force a full sync to BeadHub server",
	Long: `Clear the sync cache and upload all issues to BeadHub server.

Normally bdh uses incremental sync, only uploading changed issues.
Use this command when you suspect the cache is stale or want to
ensure all issues are synced.`,
	RunE: runForceSync,
}

func init() {
	rootCmd.AddCommand(forceSyncCmd)
}

func runForceSync(cmd *cobra.Command, args []string) error {
	cfg, err := loadAndValidateConfig()
	if err != nil {
		return err
	}

	// Clear the sync state cache
	syncStatePath := beads.SyncStatePath()
	if err := os.Remove(syncStatePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clear sync cache: %w", err)
	}

	fmt.Println("Sync cache cleared, performing full sync...")

	// Trigger full sync
	result := syncToBeadHub(cfg, nil, false)

	if result.Warning != "" {
		return fmt.Errorf("sync failed: %s", result.Warning)
	}

	if result.Stats != nil {
		fmt.Printf("SYNC: %d synced (%d added, %d updated)\n",
			result.Stats.Received,
			result.Stats.Inserted,
			result.Stats.Updated)
	} else if result.Synced {
		fmt.Printf("SYNC: %d issues uploaded\n", result.IssuesCount)
	} else {
		fmt.Println("SYNC: no issues to sync")
	}

	return nil
}
