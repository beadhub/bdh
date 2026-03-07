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
