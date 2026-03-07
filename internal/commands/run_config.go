package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/beadhub/bdh/internal/config"
)

const (
	defaultRunWaitSeconds       = 20
	defaultRunIdleWaitSeconds   = 30
	defaultRunBasePrompt        = ""
	defaultRunWorkPromptSuffix  = "Before closing the bead, run a self-review or code-reviewer pass on your changes."
	defaultRunCommsPromptSuffix = ""
)

type runUserConfig struct {
	BasePrompt        *string `json:"base_prompt"`
	WorkPromptSuffix  *string `json:"work_prompt_suffix"`
	CommsPromptSuffix *string `json:"comms_prompt_suffix"`
	WaitSeconds       *int    `json:"wait_seconds"`
	IdleWaitSeconds   *int    `json:"idle_wait_seconds"`
}

type runResolvedSettings struct {
	BasePrompt        string
	WorkPromptSuffix  string
	CommsPromptSuffix string
	WaitSeconds       int
	IdleWaitSeconds   int
}

func loadRunUserConfig() (runUserConfig, error) {
	globalPath, err := runUserConfigPath()
	if err != nil {
		return runUserConfig{}, err
	}
	cfg, err := loadRunUserConfigFile(globalPath)
	if err != nil {
		return runUserConfig{}, err
	}

	localPath, err := runLocalUserConfigPath()
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return runUserConfig{}, err
	}
	localCfg, err := loadRunUserConfigFile(localPath)
	if err != nil {
		return runUserConfig{}, err
	}
	return mergeRunUserConfig(cfg, localCfg), nil
}

func runUserConfigPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory for run config: %w", err)
	}
	return filepath.Join(homeDir, ".config", "beadhub", "run.json"), nil
}

func runLocalUserConfigPath() (string, error) {
	root, err := config.WorkspaceRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, ".beadhub-run.json"), nil
}

func loadRunUserConfigFile(path string) (runUserConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return runUserConfig{}, nil
		}
		return runUserConfig{}, err
	}

	var cfg runUserConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return runUserConfig{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	return cfg, nil
}

func mergeRunUserConfig(base runUserConfig, override runUserConfig) runUserConfig {
	merged := base
	if override.BasePrompt != nil {
		merged.BasePrompt = override.BasePrompt
	}
	if override.WorkPromptSuffix != nil {
		merged.WorkPromptSuffix = override.WorkPromptSuffix
	}
	if override.CommsPromptSuffix != nil {
		merged.CommsPromptSuffix = override.CommsPromptSuffix
	}
	if override.WaitSeconds != nil {
		merged.WaitSeconds = override.WaitSeconds
	}
	if override.IdleWaitSeconds != nil {
		merged.IdleWaitSeconds = override.IdleWaitSeconds
	}
	return merged
}

func resolveRunSettings(
	cfg runUserConfig,
	basePromptFlagSet bool,
	basePromptFlagValue string,
	workPromptFlagSet bool,
	workPromptFlagValue string,
	commsPromptFlagSet bool,
	commsPromptFlagValue string,
	waitFlagSet bool,
	waitFlagValue int,
	idleWaitFlagSet bool,
	idleWaitFlagValue int,
) (runResolvedSettings, error) {
	settings := runResolvedSettings{
		BasePrompt:        defaultRunBasePrompt,
		WorkPromptSuffix:  defaultRunWorkPromptSuffix,
		CommsPromptSuffix: defaultRunCommsPromptSuffix,
		WaitSeconds:       defaultRunWaitSeconds,
		IdleWaitSeconds:   defaultRunIdleWaitSeconds,
	}

	if cfg.BasePrompt != nil {
		settings.BasePrompt = *cfg.BasePrompt
	}
	if cfg.WorkPromptSuffix != nil {
		settings.WorkPromptSuffix = *cfg.WorkPromptSuffix
	}
	if cfg.CommsPromptSuffix != nil {
		settings.CommsPromptSuffix = *cfg.CommsPromptSuffix
	}
	if cfg.WaitSeconds != nil {
		settings.WaitSeconds = *cfg.WaitSeconds
	}
	if cfg.IdleWaitSeconds != nil {
		settings.IdleWaitSeconds = *cfg.IdleWaitSeconds
	}
	if basePromptFlagSet {
		settings.BasePrompt = basePromptFlagValue
	}
	if workPromptFlagSet {
		settings.WorkPromptSuffix = workPromptFlagValue
	}
	if commsPromptFlagSet {
		settings.CommsPromptSuffix = commsPromptFlagValue
	}
	if waitFlagSet {
		settings.WaitSeconds = waitFlagValue
	}
	if idleWaitFlagSet {
		settings.IdleWaitSeconds = idleWaitFlagValue
	}

	if settings.WaitSeconds < 0 {
		return runResolvedSettings{}, fmt.Errorf("wait_seconds must be >= 0")
	}
	if settings.IdleWaitSeconds < 0 {
		return runResolvedSettings{}, fmt.Errorf("idle_wait_seconds must be >= 0")
	}

	return settings, nil
}
