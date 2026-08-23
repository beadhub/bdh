package commands

import (
	"strings"
	"testing"
)

func TestProjectsCommandFailsLocallyWithActionableDashboardPath(t *testing.T) {
	if !projectsCmd.Hidden {
		t.Fatal(":projects must not be advertised as a supported CLI command")
	}

	err := projectsCmd.RunE(projectsCmd, []string{"list"})
	if err == nil {
		t.Fatal("expected unsupported-command error")
	}
	message := err.Error()
	for _, expected := range []string{"no longer supported", "signed-in human session", projectsDashboardURL} {
		if !strings.Contains(message, expected) {
			t.Fatalf("error %q does not contain %q", message, expected)
		}
	}
}
