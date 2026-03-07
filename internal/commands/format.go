package commands

import (
	"fmt"
	"sort"
	"strings"
)

type runPresenterState struct {
	lastWasText bool
}

func formatRunDone(event *runEvent) string {
	parts := []string{"done"}
	duration := event.DurationMS
	if duration > 0 {
		parts = append(parts, fmt.Sprintf("%.1fs", float64(duration)/1000.0))
	}
	if event.CostUSD != nil {
		parts = append(parts, fmt.Sprintf("$%.4f", *event.CostUSD))
	}
	return strings.Join(parts, "  ")
}

func formatRunToolInput(data map[string]any) string {
	return strings.Join(formatRunToolInputLines(data), "  ")
}

func formatRunToolCallLines(call runToolCall) []string {
	summary, description := formatRunToolCall(call.Name, call.Input)
	if description == "" {
		return []string{summary}
	}
	return []string{summary, "      " + description}
}

func formatRunToolCall(name string, data map[string]any) (string, string) {
	description := formatRunToolDescription(data)
	args := formatRunToolSummaryArgs(data)
	if len(args) == 0 {
		return fmt.Sprintf("tool: %s", name), description
	}
	return fmt.Sprintf("tool: %s(%s)", name, strings.Join(args, ", ")), description
}

func formatRunToolDescription(data map[string]any) string {
	if len(data) == 0 {
		return ""
	}
	description, _ := data["description"].(string)
	return truncateRunText(description, 160)
}

func formatRunToolSummaryArgs(data map[string]any) []string {
	if len(data) == 0 {
		return nil
	}

	keys := make([]string, 0, len(data))
	for key := range data {
		if key == "description" {
			continue
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil
	}
	sort.SliceStable(keys, func(i, j int) bool {
		return runToolSummaryKeyRank(keys[i]) < runToolSummaryKeyRank(keys[j])
	})

	args := make([]string, 0, len(keys))
	for index, key := range keys {
		value := data[key]
		formattedValue := formatRunToolSummaryValue(value)
		if index == 0 && len(keys) == 1 && runToolSummaryCanOmitKey(key, value) {
			args = append(args, formattedValue)
			continue
		}
		if index == 0 && runToolSummaryCanOmitKey(key, value) {
			args = append(args, formattedValue)
			continue
		}
		args = append(args, fmt.Sprintf("%s=%s", key, formattedValue))
	}
	return args
}

func runToolSummaryCanOmitKey(key string, value any) bool {
	if _, ok := value.(string); !ok {
		return false
	}
	switch key {
	case "command", "cmd", "query", "path", "url":
		return true
	default:
		return false
	}
}

func runToolSummaryKeyRank(key string) int {
	switch key {
	case "command":
		return 0
	case "cmd":
		return 1
	case "query":
		return 2
	case "path":
		return 3
	case "url":
		return 4
	default:
		return 10
	}
}

func formatRunToolSummaryValue(value any) string {
	switch typed := value.(type) {
	case string:
		return fmt.Sprintf("%q", truncateRunText(typed, 160))
	default:
		return truncateRunText(fmt.Sprintf("%v", typed), 160)
	}
}

func formatRunToolInputLines(data map[string]any) []string {
	if len(data) == 0 {
		return nil
	}

	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := data[key]
		switch typed := value.(type) {
		case string:
			parts = append(parts, fmt.Sprintf("%s=%q", key, truncateRunText(typed, 160)))
		default:
			parts = append(parts, fmt.Sprintf("%s=%s", key, truncateRunText(fmt.Sprintf("%v", typed), 160)))
		}
	}
	return parts
}

func truncateRunText(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}
