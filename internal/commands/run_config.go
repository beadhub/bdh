package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	awrun "github.com/awebai/aw/run"
	"github.com/beadhub/bdh/internal/config"
)

const (
	defaultRunWaitSeconds       = awrun.DefaultWaitSeconds
	defaultRunIdleWaitSeconds   = awrun.DefaultIdleWaitSeconds
	defaultRunCompactThreshold  = awrun.DefaultCompactThreshold
	defaultRunBasePrompt        = awrun.DefaultBasePrompt
	defaultRunWorkPromptSuffix  = "Before closing the bead, run a self-review or code-reviewer pass on your changes."
	defaultRunCommsPromptSuffix = awrun.DefaultCommsPrompt
)

type runUserConfig struct {
	BasePrompt        *string            `json:"base_prompt"`
	WorkPromptSuffix  *string            `json:"work_prompt_suffix"`
	CommsPromptSuffix *string            `json:"comms_prompt_suffix"`
	WaitSeconds       *int               `json:"wait_seconds"`
	IdleWaitSeconds   *int               `json:"idle_wait_seconds"`
	CompactThreshold  *int               `json:"compact_threshold_pct"`
	Services          []runServiceConfig `json:"services"`
}

type runResolvedSettings struct {
	BasePrompt        string
	WorkPromptSuffix  string
	CommsPromptSuffix string
	WaitSeconds       int
	IdleWaitSeconds   int
	CompactThreshold  int
	Services          []runServiceConfig
}

func loadRunUserConfig() (runUserConfig, error) {
	startDir, err := os.Getwd()
	if err != nil {
		return runUserConfig{}, err
	}
	// Precedence: aw defaults/global/local first, then bdh overlays.
	cfg := fromAWRunUserConfig(awrun.UserConfig{})
	awCfg, err := awrun.LoadUserConfig(startDir)
	if err != nil {
		return runUserConfig{}, err
	}
	cfg = mergeRunUserConfig(cfg, fromAWRunUserConfig(awCfg))

	globalPath, err := runUserConfigPath()
	if err != nil {
		return runUserConfig{}, err
	}
	globalCfg, err := loadRunUserConfigFile(globalPath)
	if err != nil {
		return runUserConfig{}, err
	}
	cfg = mergeRunUserConfig(cfg, globalCfg)

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
	if override.CompactThreshold != nil {
		merged.CompactThreshold = override.CompactThreshold
	}
	if override.Services != nil {
		merged.Services = append([]runServiceConfig(nil), override.Services...)
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
	compactThresholdFlagSet bool,
	compactThresholdFlagValue int,
) (runResolvedSettings, error) {
	overrides := awrun.SettingOverrides{
		BasePrompt:        changedRunSettingString(basePromptFlagSet, basePromptFlagValue),
		WorkPromptSuffix:  changedRunSettingString(workPromptFlagSet, workPromptFlagValue),
		CommsPromptSuffix: changedRunSettingString(commsPromptFlagSet, commsPromptFlagValue),
		WaitSeconds:       changedRunSettingInt(waitFlagSet, waitFlagValue),
		IdleWaitSeconds:   changedRunSettingInt(idleWaitFlagSet, idleWaitFlagValue),
		CompactThreshold:  changedRunSettingInt(compactThresholdFlagSet, compactThresholdFlagValue),
	}
	// Keep bdh-specific work guidance unless config/flags explicitly set it.
	if !workPromptFlagSet && cfg.WorkPromptSuffix == nil {
		overrides.WorkPromptSuffix = changedRunSettingString(true, defaultRunWorkPromptSuffix)
	}

	settings, err := awrun.ResolveSettings(toAWRunUserConfig(cfg), overrides)
	if err != nil {
		return runResolvedSettings{}, err
	}
	return fromAWRunSettings(settings), nil
}

func changedRunSettingString(flagSet bool, value string) *string {
	if !flagSet {
		return nil
	}
	result := value
	return &result
}

func changedRunSettingInt(flagSet bool, value int) *int {
	if !flagSet {
		return nil
	}
	result := value
	return &result
}

func toAWRunUserConfig(cfg runUserConfig) awrun.UserConfig {
	return awrun.UserConfig{
		BasePrompt:        cfg.BasePrompt,
		WorkPromptSuffix:  cfg.WorkPromptSuffix,
		CommsPromptSuffix: cfg.CommsPromptSuffix,
		WaitSeconds:       cfg.WaitSeconds,
		IdleWaitSeconds:   cfg.IdleWaitSeconds,
		CompactThreshold:  cfg.CompactThreshold,
		Services:          toAWRunServices(cfg.Services),
	}
}

func fromAWRunUserConfig(cfg awrun.UserConfig) runUserConfig {
	return runUserConfig{
		BasePrompt:        cfg.BasePrompt,
		WorkPromptSuffix:  cfg.WorkPromptSuffix,
		CommsPromptSuffix: cfg.CommsPromptSuffix,
		WaitSeconds:       cfg.WaitSeconds,
		IdleWaitSeconds:   cfg.IdleWaitSeconds,
		CompactThreshold:  cfg.CompactThreshold,
		Services:          fromAWRunServices(cfg.Services),
	}
}

func fromAWRunSettings(settings awrun.Settings) runResolvedSettings {
	return runResolvedSettings{
		BasePrompt:        settings.BasePrompt,
		WorkPromptSuffix:  settings.WorkPromptSuffix,
		CommsPromptSuffix: settings.CommsPromptSuffix,
		WaitSeconds:       settings.WaitSeconds,
		IdleWaitSeconds:   settings.IdleWaitSeconds,
		CompactThreshold:  settings.CompactThreshold,
		Services:          fromAWRunServices(settings.Services),
	}
}

func toAWRunServices(services []runServiceConfig) []awrun.ServiceConfig {
	if services == nil {
		return nil
	}
	converted := make([]awrun.ServiceConfig, 0, len(services))
	for _, service := range services {
		converted = append(converted, awrun.ServiceConfig{
			Name:        service.Name,
			Command:     service.Command,
			Description: service.Description,
		})
	}
	return converted
}

func fromAWRunServices(services []awrun.ServiceConfig) []runServiceConfig {
	if services == nil {
		return nil
	}
	converted := make([]runServiceConfig, 0, len(services))
	for _, service := range services {
		converted = append(converted, runServiceConfig{
			Name:        service.Name,
			Command:     service.Command,
			Description: service.Description,
		})
	}
	return converted
}
