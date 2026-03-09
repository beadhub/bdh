package commands

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitRunUserConfigWritesConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	input := strings.NewReader("coordinate with grace\nreview before close\nreturn to work\n15\n45\n70\n")
	var output bytes.Buffer

	if err := initRunUserConfig(input, &output, runUserConfig{}); err != nil {
		t.Fatalf("initRunUserConfig returned error: %v", err)
	}

	cfg, err := loadRunUserConfig()
	if err != nil {
		t.Fatalf("loadRunUserConfig returned error: %v", err)
	}
	if cfg.BasePrompt == nil || *cfg.BasePrompt != "coordinate with grace" {
		t.Fatalf("expected base prompt, got %#v", cfg.BasePrompt)
	}
	if cfg.WorkPromptSuffix == nil || *cfg.WorkPromptSuffix != "review before close" {
		t.Fatalf("expected work suffix, got %#v", cfg.WorkPromptSuffix)
	}
	if cfg.CommsPromptSuffix == nil || *cfg.CommsPromptSuffix != "return to work" {
		t.Fatalf("expected comms suffix, got %#v", cfg.CommsPromptSuffix)
	}
	if cfg.WaitSeconds == nil || *cfg.WaitSeconds != 15 {
		t.Fatalf("expected wait_seconds=15, got %#v", cfg.WaitSeconds)
	}
	if cfg.IdleWaitSeconds == nil || *cfg.IdleWaitSeconds != 45 {
		t.Fatalf("expected idle_wait_seconds=45, got %#v", cfg.IdleWaitSeconds)
	}
	if cfg.CompactThreshold == nil || *cfg.CompactThreshold != 70 {
		t.Fatalf("expected compact_threshold_pct=70, got %#v", cfg.CompactThreshold)
	}
	if !strings.Contains(output.String(), filepath.Join(dir, ".config", "beadhub", "run.json")) {
		t.Fatalf("expected output to mention config path, got %q", output.String())
	}
}

func TestInitRunUserConfigSeedsSuggestedDefaultsWhenUnset(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	input := strings.NewReader("\n\n\n\n\n\n")
	var output bytes.Buffer

	if err := initRunUserConfig(input, &output, runUserConfig{}); err != nil {
		t.Fatalf("initRunUserConfig returned error: %v", err)
	}

	cfg, err := loadRunUserConfig()
	if err != nil {
		t.Fatalf("loadRunUserConfig returned error: %v", err)
	}
	if cfg.BasePrompt == nil || *cfg.BasePrompt != defaultRunInitBasePrompt {
		t.Fatalf("expected suggested base prompt, got %#v", cfg.BasePrompt)
	}
	if cfg.WorkPromptSuffix == nil || *cfg.WorkPromptSuffix != defaultRunWorkPromptSuffix {
		t.Fatalf("expected default work suffix, got %#v", cfg.WorkPromptSuffix)
	}
	if cfg.CommsPromptSuffix == nil || *cfg.CommsPromptSuffix != defaultRunInitCommsSuffix {
		t.Fatalf("expected suggested comms suffix, got %#v", cfg.CommsPromptSuffix)
	}
	if cfg.WaitSeconds == nil || *cfg.WaitSeconds != defaultRunWaitSeconds {
		t.Fatalf("expected default wait_seconds, got %#v", cfg.WaitSeconds)
	}
	if cfg.IdleWaitSeconds == nil || *cfg.IdleWaitSeconds != defaultRunIdleWaitSeconds {
		t.Fatalf("expected default idle_wait_seconds, got %#v", cfg.IdleWaitSeconds)
	}
	if cfg.CompactThreshold == nil || *cfg.CompactThreshold != defaultRunCompactThreshold {
		t.Fatalf("expected default compact threshold, got %#v", cfg.CompactThreshold)
	}
}

func TestPromptRunConfigStringClearsOnDash(t *testing.T) {
	reader := strings.NewReader("-\n")
	var output bytes.Buffer

	value, err := promptRunConfigString(bufio.NewReader(reader), &output, "base_prompt", "current")
	if err != nil {
		t.Fatalf("promptRunConfigString returned error: %v", err)
	}
	if value == nil || *value != "" {
		t.Fatalf("expected cleared string, got %#v", value)
	}
}

func TestInitRunUserConfigPreservesGlobalServices(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	path := filepath.Join(dir, ".config", "beadhub", "run.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	seed := runUserConfig{
		Services: []runServiceConfig{
			{
				Name:        "api",
				Command:     "make run-api",
				Description: "HTTP API server",
			},
		},
	}
	data, err := json.Marshal(seed)
	if err != nil {
		t.Fatalf("marshal seed config: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write seed config: %v", err)
	}

	input := strings.NewReader("\n\n\n\n\n\n")
	var output bytes.Buffer
	if err := initRunUserConfig(input, &output, runUserConfig{}); err != nil {
		t.Fatalf("initRunUserConfig returned error: %v", err)
	}

	cfg, err := loadRunUserConfigFile(path)
	if err != nil {
		t.Fatalf("loadRunUserConfigFile returned error: %v", err)
	}
	if len(cfg.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(cfg.Services))
	}
	if cfg.Services[0].Name != "api" || cfg.Services[0].Command != "make run-api" {
		t.Fatalf("expected preserved service, got %#v", cfg.Services[0])
	}
}
