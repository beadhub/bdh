package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/beadhub/bdh/internal/config"
)

func TestLoadRunUserConfigMissingReturnsZero(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	oldPath := config.GetPath()
	config.SetPath("")
	t.Cleanup(func() { config.SetPath(oldPath) })

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
	oldPath := config.GetPath()
	config.SetPath("")
	t.Cleanup(func() { config.SetPath(oldPath) })
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
	if len(cfg.Services) != 0 {
		t.Fatalf("expected no services, got %#v", cfg.Services)
	}
}

func TestLoadRunUserConfigLocalOverridesGlobal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	workspaceRoot := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace failed: %v", err)
	}

	configPath := filepath.Join(workspaceRoot, ".beadhub")
	if err := os.WriteFile(configPath, []byte("workspace_id: 11111111-1111-1111-1111-111111111111\nbeadhub_url: \"https://app.beadhub.ai/api\"\nproject_slug: \"beadhub\"\nrepo_origin: \"git@github.com:beadhub/bdh.git\"\ncanonical_origin: \"github.com/beadhub/bdh\"\nalias: \"noah\"\nhuman_name: \"Juan\"\n"), 0o600); err != nil {
		t.Fatalf("write .beadhub failed: %v", err)
	}
	oldPath := config.GetPath()
	config.SetPath(configPath)
	t.Cleanup(func() { config.SetPath(oldPath) })

	globalPath := filepath.Join(dir, ".config", "beadhub", "run.json")
	if err := os.MkdirAll(filepath.Dir(globalPath), 0o755); err != nil {
		t.Fatalf("mkdir global config failed: %v", err)
	}
	if err := os.WriteFile(globalPath, []byte(`{"base_prompt":"global base","work_prompt_suffix":"global work","wait_seconds":11,"idle_wait_seconds":44,"services":[{"name":"backend","command":"make run-backend","description":"Backend API"}]}`), 0o600); err != nil {
		t.Fatalf("write global config failed: %v", err)
	}

	localPath := filepath.Join(workspaceRoot, ".beadhub-run.json")
	if err := os.WriteFile(localPath, []byte(`{"base_prompt":"local base","comms_prompt_suffix":"local comms","idle_wait_seconds":9,"services":[{"name":"frontend","command":"make run-frontend","description":"Frontend UI"}]}`), 0o600); err != nil {
		t.Fatalf("write local config failed: %v", err)
	}

	cfg, err := loadRunUserConfig()
	if err != nil {
		t.Fatalf("loadRunUserConfig returned error: %v", err)
	}
	if cfg.BasePrompt == nil || *cfg.BasePrompt != "local base" {
		t.Fatalf("expected local base_prompt to win, got %#v", cfg.BasePrompt)
	}
	if cfg.WorkPromptSuffix == nil || *cfg.WorkPromptSuffix != "global work" {
		t.Fatalf("expected global work suffix to remain, got %#v", cfg.WorkPromptSuffix)
	}
	if cfg.CommsPromptSuffix == nil || *cfg.CommsPromptSuffix != "local comms" {
		t.Fatalf("expected local comms suffix, got %#v", cfg.CommsPromptSuffix)
	}
	if cfg.WaitSeconds == nil || *cfg.WaitSeconds != 11 {
		t.Fatalf("expected global wait_seconds to remain, got %#v", cfg.WaitSeconds)
	}
	if cfg.IdleWaitSeconds == nil || *cfg.IdleWaitSeconds != 9 {
		t.Fatalf("expected local idle_wait_seconds to win, got %#v", cfg.IdleWaitSeconds)
	}
	if len(cfg.Services) != 1 || cfg.Services[0].Name != "frontend" {
		t.Fatalf("expected local services to override global, got %#v", cfg.Services)
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
