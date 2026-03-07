package commands

import (
	"bufio"
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitRunUserConfigWritesConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	input := strings.NewReader("coordinate with grace\nreview before close\nreturn to work\n15\n45\n")
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
	if !strings.Contains(output.String(), filepath.Join(dir, ".config", "beadhub", "run.json")) {
		t.Fatalf("expected output to mention config path, got %q", output.String())
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
