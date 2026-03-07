package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	defaultRunWaitSeconds     = 20
	defaultRunIdleWaitSeconds = 30
)

type runUserConfig struct {
	WaitSeconds     *int `json:"wait_seconds"`
	IdleWaitSeconds *int `json:"idle_wait_seconds"`
}

type runResolvedSettings struct {
	WaitSeconds     int
	IdleWaitSeconds int
}

func loadRunUserConfig() (runUserConfig, error) {
	path, err := runUserConfigPath()
	if err != nil {
		return runUserConfig{}, err
	}
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

func runUserConfigPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory for run config: %w", err)
	}
	return filepath.Join(homeDir, ".config", "beadhub", "run.json"), nil
}

func resolveRunSettings(
	cfg runUserConfig,
	waitFlagSet bool,
	waitFlagValue int,
	idlePromptFlagSet bool,
	idlePromptFlagValue string,
	idleWaitFlagSet bool,
	idleWaitFlagValue int,
) (runResolvedSettings, error) {
	settings := runResolvedSettings{
		WaitSeconds:     defaultRunWaitSeconds,
		IdleWaitSeconds: defaultRunIdleWaitSeconds,
	}

	if cfg.WaitSeconds != nil {
		settings.WaitSeconds = *cfg.WaitSeconds
	}
	if cfg.IdleWaitSeconds != nil {
		settings.IdleWaitSeconds = *cfg.IdleWaitSeconds
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
