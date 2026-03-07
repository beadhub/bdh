package commands

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRunUserConfigMissingReturnsZero(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	cfg, err := loadRunUserConfig()
	if err != nil {
		t.Fatalf("loadRunUserConfig returned error: %v", err)
	}
	if cfg.WaitSeconds != nil || cfg.IdleWaitSeconds != nil {
		t.Fatalf("expected zero config, got %#v", cfg)
	}
}

func TestLoadRunUserConfigReadsFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	path := filepath.Join(dir, ".config", "beadhub", "run.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"wait_seconds":11,"idle_wait_seconds":44}`), 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	cfg, err := loadRunUserConfig()
	if err != nil {
		t.Fatalf("loadRunUserConfig returned error: %v", err)
	}
	if cfg.WaitSeconds == nil || *cfg.WaitSeconds != 11 {
		t.Fatalf("expected wait_seconds=11, got %#v", cfg.WaitSeconds)
	}
	if cfg.IdleWaitSeconds == nil || *cfg.IdleWaitSeconds != 44 {
		t.Fatalf("expected idle_wait_seconds=44, got %#v", cfg.IdleWaitSeconds)
	}
}

func TestResolveRunSettingsPrecedence(t *testing.T) {
	wait := 9
	idleWait := 41
	cfg := runUserConfig{
		WaitSeconds:     &wait,
		IdleWaitSeconds: &idleWait,
	}

	settings, err := resolveRunSettings(cfg, true, 7, false, "", true, 13)
	if err != nil {
		t.Fatalf("resolveRunSettings returned error: %v", err)
	}
	if settings.WaitSeconds != 7 {
		t.Fatalf("expected flag wait to win, got %d", settings.WaitSeconds)
	}
	if settings.IdleWaitSeconds != 13 {
		t.Fatalf("expected flag idle wait to win, got %d", settings.IdleWaitSeconds)
	}
}
