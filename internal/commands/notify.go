package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/awebai/aw/chat"
	"github.com/beadhub/bdh/internal/config"
)

var notifyCmd = &cobra.Command{
	Use:   ":notify",
	Short: "Check for pending chat notifications (for hooks)",
	Long: `Check for pending chat notifications.

Silent if no pending chats; outputs JSON with additionalContext if there are
messages waiting. The JSON format is designed for Claude Code PostToolUse hooks
so the notification is surfaced to the agent.

Example hook configuration in .claude/settings.json:
  "hooks": {
    "PostToolUse": [{
      "matcher": ".*",
      "hooks": [{"type": "command", "command": "bdh :notify"}]
    }]
  }`,
	RunE: runNotify,
}

func runNotify(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		// Not initialized - silent exit
		return nil
	}
	if cfg.Validate() != nil {
		return nil
	}

	aw, err := newAwebClient(cfg.BeadhubURL)
	if err != nil || aw == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), apiTimeout)
	defer cancel()

	result, err := chat.Pending(ctx, aw)
	if err != nil {
		return nil
	}

	if len(result.Pending) == 0 {
		return nil
	}

	// Format output for Claude Code hook (JSON with additionalContext)
	output := formatNotifyOutput(result, cfg.Alias)
	hookOutput := formatHookOutput(output)
	fmt.Print(hookOutput)
	return nil
}

func formatNotifyOutput(result *chat.PendingResult, selfAlias string) string {
	var sb strings.Builder

	// Count urgent (sender waiting) vs regular
	var urgent, regular []string
	for _, p := range result.Pending {
		from := p.LastFrom
		if from == "" {
			for _, participant := range p.Participants {
				if participant != selfAlias {
					from = participant
					break
				}
			}
		}
		if from == "" {
			continue
		}

		if p.SenderWaiting {
			urgent = append(urgent, from)
		} else {
			regular = append(regular, from)
		}
	}

	if len(urgent) == 0 && len(regular) == 0 {
		return ""
	}

	sb.WriteString("\n")
	sb.WriteString("╔══════════════════════════════════════════════════════════════╗\n")
	sb.WriteString("║         📬 AGENT: YOU HAVE PENDING CHAT MESSAGES             ║\n")
	sb.WriteString("╠══════════════════════════════════════════════════════════════╣\n")

	for _, from := range urgent {
		line := fmt.Sprintf("║ ⚠️  URGENT: %s is WAITING for your reply", from)
		sb.WriteString(padLine(line, 65))
		sb.WriteString("║\n")
	}

	for _, from := range regular {
		line := fmt.Sprintf("║ 💬 Unread message from %s", from)
		sb.WriteString(padLine(line, 65))
		sb.WriteString("║\n")
	}

	sb.WriteString("╠══════════════════════════════════════════════════════════════╣\n")
	sb.WriteString("║ YOU MUST RUN: bdh :aweb chat pending                         ║\n")
	sb.WriteString("╚══════════════════════════════════════════════════════════════╝\n")
	sb.WriteString("\n")

	return sb.String()
}

// padLine pads a line to the target width with spaces.
func padLine(line string, width int) string {
	// Count visible characters (emoji are wider but we approximate)
	visible := len(line)
	if visible >= width {
		return line[:width]
	}
	return line + strings.Repeat(" ", width-visible)
}

// formatHookOutput wraps the notification in Claude Code hook JSON format.
// This ensures the output is surfaced to the agent via additionalContext.
func formatHookOutput(content string) string {
	output := map[string]interface{}{
		"hookSpecificOutput": map[string]interface{}{
			"hookEventName":     "PostToolUse",
			"additionalContext": content,
		},
	}
	data, _ := json.Marshal(output)
	return string(data)
}
