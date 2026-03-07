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
	if cfg.BasePrompt != nil || cfg.WorkPromptSuffix != nil || cfg.CommsPromptSuffix != nil || cfg.WaitSeconds != nil || cfg.IdleWaitSeconds != nil {
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
	if err := os.WriteFile(path, []byte(`{"base_prompt":"coordinate with grace","work_prompt_suffix":"review before close","comms_prompt_suffix":"return to your current work after handling comms","wait_seconds":11,"idle_wait_seconds":44}`), 0o600); err != nil {
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
	if cfg.BasePrompt == nil || *cfg.BasePrompt != "coordinate with grace" {
		t.Fatalf("expected base_prompt, got %#v", cfg.BasePrompt)
	}
	if cfg.WorkPromptSuffix == nil || *cfg.WorkPromptSuffix != "review before close" {
		t.Fatalf("expected work_prompt_suffix, got %#v", cfg.WorkPromptSuffix)
	}
	if cfg.CommsPromptSuffix == nil || *cfg.CommsPromptSuffix != "return to your current work after handling comms" {
		t.Fatalf("expected comms_prompt_suffix, got %#v", cfg.CommsPromptSuffix)
	}
}

func TestResolveRunSettingsPrecedence(t *testing.T) {
	wait := 9
	idleWait := 41
	basePrompt := "config base"
	workSuffix := "config work suffix"
	commsSuffix := "config comms suffix"
	cfg := runUserConfig{
		BasePrompt:        &basePrompt,
		WorkPromptSuffix:  &workSuffix,
		CommsPromptSuffix: &commsSuffix,
		WaitSeconds:       &wait,
		IdleWaitSeconds:   &idleWait,
	}

	settings, err := resolveRunSettings(cfg, false, "", false, "", false, "", true, 7, true, 13)
	if err != nil {
		t.Fatalf("resolveRunSettings returned error: %v", err)
	}
	if settings.WaitSeconds != 7 {
		t.Fatalf("expected flag wait to win, got %d", settings.WaitSeconds)
	}
	if settings.IdleWaitSeconds != 13 {
		t.Fatalf("expected flag idle wait to win, got %d", settings.IdleWaitSeconds)
	}
	if settings.BasePrompt != "config base" {
		t.Fatalf("expected base prompt from config, got %q", settings.BasePrompt)
	}
	if settings.WorkPromptSuffix != "config work suffix" {
		t.Fatalf("expected work suffix from config, got %q", settings.WorkPromptSuffix)
	}
	if settings.CommsPromptSuffix != "config comms suffix" {
		t.Fatalf("expected comms suffix from config, got %q", settings.CommsPromptSuffix)
	}
}

func TestResolveRunSettingsPromptFlagsOverrideConfig(t *testing.T) {
	basePrompt := "config base"
	workSuffix := "config work suffix"
	commsSuffix := "config comms suffix"
	cfg := runUserConfig{
		BasePrompt:        &basePrompt,
		WorkPromptSuffix:  &workSuffix,
		CommsPromptSuffix: &commsSuffix,
	}

	settings, err := resolveRunSettings(
		cfg,
		true, "flag base",
		true, "flag work",
		true, "flag comms",
		false, 0,
		false, 0,
	)
	if err != nil {
		t.Fatalf("resolveRunSettings returned error: %v", err)
	}
	if settings.BasePrompt != "flag base" {
		t.Fatalf("expected base prompt from flag, got %q", settings.BasePrompt)
	}
	if settings.WorkPromptSuffix != "flag work" {
		t.Fatalf("expected work suffix from flag, got %q", settings.WorkPromptSuffix)
	}
	if settings.CommsPromptSuffix != "flag comms" {
		t.Fatalf("expected comms suffix from flag, got %q", settings.CommsPromptSuffix)
	}
}
