package commands

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRealRunCommandWritesProviderStderrLog(t *testing.T) {
	t.Parallel()

	logs, err := newRunLogSession(t.TempDir(), true, time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("newRunLogSession returned error: %v", err)
	}
	defer func() { _ = logs.Close() }()

	stderrSink, err := logs.OpenProviderStderr()
	if err != nil {
		t.Fatalf("OpenProviderStderr returned error: %v", err)
	}
	defer func() { _ = stderrSink.Close() }()

	err = realRunCommand(
		context.Background(),
		t.TempDir(),
		[]string{"/bin/sh", "-lc", `printf 'provider stderr\n' >&2; exit 1`},
		func(string) {},
		stderrSink,
	)
	if err == nil {
		t.Fatal("expected realRunCommand error")
	}
	if !strings.Contains(err.Error(), "provider stderr") {
		t.Fatalf("expected stderr in error, got %v", err)
	}

	data, readErr := os.ReadFile(filepath.Join(logs.Dir(), "provider.stderr.log"))
	if readErr != nil {
		t.Fatalf("read provider stderr log: %v", readErr)
	}
	if !strings.Contains(string(data), "provider stderr") {
		t.Fatalf("provider stderr log missing output: %q", string(data))
	}
}

func TestResolveRunDebugModeFromEnv(t *testing.T) {
	t.Setenv("BDH_RUN_DEBUG", "1")
	if !resolveRunDebugMode(false, false) {
		t.Fatal("expected env to enable debug mode")
	}
	if resolveRunDebugMode(true, false) {
		t.Fatal("expected explicit flag=false to override env")
	}
	if !resolveRunDebugMode(true, true) {
		t.Fatal("expected explicit flag=true to enable debug mode")
	}
}
