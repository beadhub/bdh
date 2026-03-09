package commands

import (
	"os"
	"path/filepath"
	"testing"

	awconfig "github.com/awebai/aw/awconfig"
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
	if cfg.BasePrompt != nil || cfg.WorkPromptSuffix != nil || cfg.CommsPromptSuffix != nil || cfg.WaitSeconds != nil || cfg.IdleWaitSeconds != nil || cfg.CompactThreshold != nil {
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
	if err := os.WriteFile(path, []byte(`{"base_prompt":"coordinate with grace","work_prompt_suffix":"review before close","comms_prompt_suffix":"return to your current work after handling comms","wait_seconds":11,"idle_wait_seconds":44,"compact_threshold_pct":72}`), 0o600); err != nil {
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
	if cfg.CompactThreshold == nil || *cfg.CompactThreshold != 72 {
		t.Fatalf("expected compact_threshold_pct=72, got %#v", cfg.CompactThreshold)
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
	if err := os.WriteFile(globalPath, []byte(`{"base_prompt":"global base","work_prompt_suffix":"global work","wait_seconds":11,"idle_wait_seconds":44,"compact_threshold_pct":81,"services":[{"name":"backend","command":"make run-backend","description":"Backend API"}]}`), 0o600); err != nil {
		t.Fatalf("write global config failed: %v", err)
	}

	localPath := filepath.Join(workspaceRoot, ".beadhub-run.json")
	if err := os.WriteFile(localPath, []byte(`{"base_prompt":"local base","comms_prompt_suffix":"local comms","idle_wait_seconds":9,"compact_threshold_pct":65,"services":[{"name":"frontend","command":"make run-frontend","description":"Frontend UI"}]}`), 0o600); err != nil {
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
	if cfg.CompactThreshold == nil || *cfg.CompactThreshold != 65 {
		t.Fatalf("expected local compact threshold to win, got %#v", cfg.CompactThreshold)
	}
	if len(cfg.Services) != 1 || cfg.Services[0].Name != "frontend" {
		t.Fatalf("expected local services to override global, got %#v", cfg.Services)
	}
}

func TestLoadRunUserConfigUsesAWBaseThenBDHOverrides(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	oldPath := config.GetPath()
	config.SetPath("")
	t.Cleanup(func() { config.SetPath(oldPath) })

	workspaceRoot := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace failed: %v", err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(workspaceRoot); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	bdhConfigPath := filepath.Join(workspaceRoot, ".beadhub")
	if err := os.WriteFile(bdhConfigPath, []byte("workspace_id: 11111111-1111-1111-1111-111111111111\nbeadhub_url: \"https://app.beadhub.ai/api\"\nproject_slug: \"beadhub\"\nrepo_origin: \"git@github.com:beadhub/bdh.git\"\ncanonical_origin: \"github.com/beadhub/bdh\"\nalias: \"noah\"\nhuman_name: \"Juan\"\n"), 0o600); err != nil {
		t.Fatalf("write .beadhub failed: %v", err)
	}

	awContextPath := filepath.Join(workspaceRoot, awconfig.DefaultWorktreeContextRelativePath())
	if err := os.MkdirAll(filepath.Dir(awContextPath), 0o755); err != nil {
		t.Fatalf("mkdir aw context dir failed: %v", err)
	}
	if err := os.WriteFile(awContextPath, []byte("default_account: default\n"), 0o600); err != nil {
		t.Fatalf("write aw context failed: %v", err)
	}

	awGlobalPath := filepath.Join(dir, ".config", "aw", "run.json")
	if err := os.MkdirAll(filepath.Dir(awGlobalPath), 0o755); err != nil {
		t.Fatalf("mkdir aw global dir failed: %v", err)
	}
	if err := os.WriteFile(awGlobalPath, []byte(`{"base_prompt":"aw global base","work_prompt_suffix":"aw global work","wait_seconds":11,"idle_wait_seconds":41,"compact_threshold_pct":66,"services":[{"name":"aw-global","command":"make aw-global"}]}`), 0o600); err != nil {
		t.Fatalf("write aw global config failed: %v", err)
	}

	awLocalPath := filepath.Join(workspaceRoot, ".aw", "run.json")
	if err := os.WriteFile(awLocalPath, []byte(`{"base_prompt":"aw local base","comms_prompt_suffix":"aw local comms","wait_seconds":12,"services":[{"name":"aw-local","command":"make aw-local"}]}`), 0o600); err != nil {
		t.Fatalf("write aw local config failed: %v", err)
	}

	bdhGlobalPath := filepath.Join(dir, ".config", "beadhub", "run.json")
	if err := os.MkdirAll(filepath.Dir(bdhGlobalPath), 0o755); err != nil {
		t.Fatalf("mkdir bdh global dir failed: %v", err)
	}
	if err := os.WriteFile(bdhGlobalPath, []byte(`{"work_prompt_suffix":"bdh global work","wait_seconds":13}`), 0o600); err != nil {
		t.Fatalf("write bdh global config failed: %v", err)
	}

	bdhLocalPath := filepath.Join(workspaceRoot, ".beadhub-run.json")
	if err := os.WriteFile(bdhLocalPath, []byte(`{"comms_prompt_suffix":"bdh local comms","idle_wait_seconds":14,"services":[{"name":"bdh-local","command":"make bdh-local"}]}`), 0o600); err != nil {
		t.Fatalf("write bdh local config failed: %v", err)
	}

	cfg, err := loadRunUserConfig()
	if err != nil {
		t.Fatalf("loadRunUserConfig returned error: %v", err)
	}
	if cfg.BasePrompt == nil || *cfg.BasePrompt != "aw local base" {
		t.Fatalf("expected aw local base prompt, got %#v", cfg.BasePrompt)
	}
	if cfg.WorkPromptSuffix == nil || *cfg.WorkPromptSuffix != "bdh global work" {
		t.Fatalf("expected bdh global work suffix, got %#v", cfg.WorkPromptSuffix)
	}
	if cfg.CommsPromptSuffix == nil || *cfg.CommsPromptSuffix != "bdh local comms" {
		t.Fatalf("expected bdh local comms suffix, got %#v", cfg.CommsPromptSuffix)
	}
	if cfg.WaitSeconds == nil || *cfg.WaitSeconds != 13 {
		t.Fatalf("expected bdh global wait_seconds=13, got %#v", cfg.WaitSeconds)
	}
	if cfg.IdleWaitSeconds == nil || *cfg.IdleWaitSeconds != 14 {
		t.Fatalf("expected bdh local idle_wait_seconds=14, got %#v", cfg.IdleWaitSeconds)
	}
	if cfg.CompactThreshold == nil || *cfg.CompactThreshold != 66 {
		t.Fatalf("expected aw compact_threshold_pct=66, got %#v", cfg.CompactThreshold)
	}
	if len(cfg.Services) != 1 || cfg.Services[0].Name != "bdh-local" {
		t.Fatalf("expected bdh local services to win, got %#v", cfg.Services)
	}
}

func TestResolveRunSettingsPrecedence(t *testing.T) {
	wait := 9
	idleWait := 41
	compactThreshold := 77
	basePrompt := "config base"
	workSuffix := "config work suffix"
	commsSuffix := "config comms suffix"
	cfg := runUserConfig{
		BasePrompt:        &basePrompt,
		WorkPromptSuffix:  &workSuffix,
		CommsPromptSuffix: &commsSuffix,
		WaitSeconds:       &wait,
		IdleWaitSeconds:   &idleWait,
		CompactThreshold:  &compactThreshold,
	}

	settings, err := resolveRunSettings(cfg, false, "", false, "", false, "", true, 7, true, 13, false, 0)
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
	if settings.CompactThreshold != 77 {
		t.Fatalf("expected compact threshold from config, got %d", settings.CompactThreshold)
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
		true, 55,
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
	if settings.CompactThreshold != 55 {
		t.Fatalf("expected compact threshold from flag, got %d", settings.CompactThreshold)
	}
}

func TestResolveRunSettingsUsesBDHDefaultWorkPromptWhenUnset(t *testing.T) {
	settings, err := resolveRunSettings(runUserConfig{}, false, "", false, "", false, "", false, 0, false, 0, false, 0)
	if err != nil {
		t.Fatalf("resolveRunSettings returned error: %v", err)
	}
	if settings.WorkPromptSuffix != defaultRunWorkPromptSuffix {
		t.Fatalf("expected bdh work prompt default, got %q", settings.WorkPromptSuffix)
	}
}
