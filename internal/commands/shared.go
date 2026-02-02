package commands

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// apiTimeout is the default timeout for API calls.
const apiTimeout = 10 * time.Second

// TTL constraints for reservations (used by auto-reserve).
const (
	reserveDefaultTTL = 300
)

// validatePath checks if a path is safe (no traversal, non-empty).
// Used by auto-reserve to filter git status paths.
func validatePath(path string) error {
	if path == "" {
		return fmt.Errorf("path is empty")
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("path traversal not allowed: %s", path)
	}
	return nil
}

// formatDuration formats seconds into a human-readable duration.
func formatDuration(seconds int) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	if seconds < 3600 {
		mins := seconds / 60
		secs := seconds % 60
		if secs == 0 {
			return fmt.Sprintf("%dm", mins)
		}
		return fmt.Sprintf("%dm%ds", mins, secs)
	}
	hours := seconds / 3600
	mins := (seconds % 3600) / 60
	if mins == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh%dm", hours, mins)
}

func ttlRemainingSeconds(expiresAt string, now time.Time) int {
	if expiresAt == "" {
		return 0
	}
	ts, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		ts, err = time.Parse(time.RFC3339, expiresAt)
		if err != nil {
			return 0
		}
	}
	secs := int(math.Ceil(ts.Sub(now).Seconds()))
	if secs < 0 {
		return 0
	}
	return secs
}
