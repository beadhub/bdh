package commands

import (
	"fmt"
	"time"
)

const staleClaimThreshold = 24 * time.Hour

func parseTimeBestEffort(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	// Accept RFC3339 and RFC3339Nano (most server timestamps).
	if ts, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return ts, true
	}
	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		return ts, true
	}
	return time.Time{}, false
}

// formatTimeAgo formats a timestamp string as "X ago" for human-friendly display.
// If parsing fails, it falls back to the raw timestamp.
func formatTimeAgo(timestamp string) string {
	ts, ok := parseTimeBestEffort(timestamp)
	if !ok {
		return timestamp
	}
	d := time.Since(ts)
	if d < 0 {
		d = 0
	}
	secs := int(d.Seconds())
	if secs < 60 {
		return fmt.Sprintf("%ds ago", secs)
	}
	mins := secs / 60
	if mins < 60 {
		return fmt.Sprintf("%dm ago", mins)
	}
	hours := mins / 60
	if hours < 48 {
		return fmt.Sprintf("%dh ago", hours)
	}
	days := hours / 24
	return fmt.Sprintf("%dd ago", days)
}

func isClaimStale(claimedAt string) bool {
	ts, ok := parseTimeBestEffort(claimedAt)
	if !ok {
		return false
	}
	return time.Since(ts) > staleClaimThreshold
}
