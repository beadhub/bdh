package commands

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultRunInitBasePrompt  = "Coordinate through BeadHub. Prioritize pending chat and unread mail first, then continue the best available work. Keep teammates informed, make concrete progress, and leave changes in a reviewable state."
	defaultRunInitCommsSuffix = "After handling the communication, return to the best available work if more work remains."
)

func initRunUserConfig(in io.Reader, out io.Writer, existing runUserConfig) error {
	reader := bufio.NewReader(in)
	current, err := resolveRunSettings(existing, false, "", false, "", false, "", false, 0, false, 0, false, 0)
	if err != nil {
		return err
	}
	current = applySuggestedRunInitDefaults(existing, current)

	fmt.Fprintln(out, "Configuring bdh :run. Press Enter to keep the current value. Enter '-' to clear a string field.")

	basePrompt, err := promptRunConfigString(reader, out, "base_prompt", current.BasePrompt)
	if err != nil {
		return err
	}
	workSuffix, err := promptRunConfigString(reader, out, "work_prompt_suffix", current.WorkPromptSuffix)
	if err != nil {
		return err
	}
	commsSuffix, err := promptRunConfigString(reader, out, "comms_prompt_suffix", current.CommsPromptSuffix)
	if err != nil {
		return err
	}
	waitSeconds, err := promptRunConfigInt(reader, out, "wait_seconds", current.WaitSeconds)
	if err != nil {
		return err
	}
	idleWaitSeconds, err := promptRunConfigInt(reader, out, "idle_wait_seconds", current.IdleWaitSeconds)
	if err != nil {
		return err
	}
	compactThreshold, err := promptRunConfigInt(reader, out, "compact_threshold_pct", current.CompactThreshold)
	if err != nil {
		return err
	}

	cfg := runUserConfig{
		BasePrompt:        basePrompt,
		WorkPromptSuffix:  workSuffix,
		CommsPromptSuffix: commsSuffix,
		WaitSeconds:       waitSeconds,
		IdleWaitSeconds:   idleWaitSeconds,
		CompactThreshold:  compactThreshold,
	}
	path, err := writeRunUserConfig(cfg)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Wrote %s\n", path)
	return nil
}

func applySuggestedRunInitDefaults(existing runUserConfig, current runResolvedSettings) runResolvedSettings {
	if existing.BasePrompt == nil && strings.TrimSpace(current.BasePrompt) == "" {
		current.BasePrompt = defaultRunInitBasePrompt
	}
	if existing.CommsPromptSuffix == nil && strings.TrimSpace(current.CommsPromptSuffix) == "" {
		current.CommsPromptSuffix = defaultRunInitCommsSuffix
	}
	return current
}

func promptRunConfigString(reader *bufio.Reader, out io.Writer, key string, current string) (*string, error) {
	label := current
	if strings.TrimSpace(label) == "" {
		label = "(empty)"
	}
	fmt.Fprintf(out, "%s [%s]: ", key, label)

	value, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return nil, err
	}
	value = strings.TrimRight(value, "\r\n")

	switch value {
	case "":
		value = current
	case "-":
		value = ""
	}

	result := value
	return &result, nil
}

func promptRunConfigInt(reader *bufio.Reader, out io.Writer, key string, current int) (*int, error) {
	fmt.Fprintf(out, "%s [%d]: ", key, current)

	value, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return nil, err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		result := current
		return &result, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return nil, fmt.Errorf("%s must be an integer", key)
	}
	if parsed < 0 {
		return nil, fmt.Errorf("%s must be >= 0", key)
	}
	return &parsed, nil
}

func writeRunUserConfig(cfg runUserConfig) (string, error) {
	path, err := runUserConfigPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}
