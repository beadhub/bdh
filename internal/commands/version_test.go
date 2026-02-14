package commands

import (
	"strings"
	"testing"
)

func TestFormatVersionOutput_DevVersion(t *testing.T) {
	old := versionInfo
	defer func() { versionInfo = old }()

	versionInfo.version = "dev"
	versionInfo.commit = "none"
	versionInfo.date = "unknown"

	out := formatVersionOutput()
	if !strings.Contains(out, "bdh dev") {
		t.Errorf("expected 'bdh dev' in output, got: %q", out)
	}
	// dev version should not show commit or date
	if strings.Contains(out, "commit:") {
		t.Errorf("dev version should not show commit, got: %q", out)
	}
	if strings.Contains(out, "built:") {
		t.Errorf("dev version should not show date, got: %q", out)
	}
}

func TestFormatVersionOutput_ReleaseVersion(t *testing.T) {
	old := versionInfo
	defer func() { versionInfo = old }()

	versionInfo.version = "1.2.3"
	versionInfo.commit = "abc1234"
	versionInfo.date = "2026-02-14"

	out := formatVersionOutput()
	if !strings.Contains(out, "bdh 1.2.3") {
		t.Errorf("expected 'bdh 1.2.3' in output, got: %q", out)
	}
	if !strings.Contains(out, "commit: abc1234") {
		t.Errorf("expected commit in output, got: %q", out)
	}
	if !strings.Contains(out, "built:  2026-02-14") {
		t.Errorf("expected date in output, got: %q", out)
	}
}
