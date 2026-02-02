package commands

import (
	"testing"
)

func TestNextAliasPrefixCmd_Flags(t *testing.T) {
	// Verify command has expected flags
	cmd := nextAliasPrefixCmd

	if cmd.Use != ":next-alias-prefix" {
		t.Errorf("expected Use to be ':next-alias-prefix', got %q", cmd.Use)
	}

	// Check flags exist
	jsonFlag := cmd.Flags().Lookup("json")
	if jsonFlag == nil {
		t.Error("expected 'json' flag to exist")
	}

	urlFlag := cmd.Flags().Lookup("beadhub-url")
	if urlFlag == nil {
		t.Error("expected 'beadhub-url' flag to exist")
	}
}
