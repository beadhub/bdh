// ABOUTME: Chat commands for bdh CLI using the aweb-go chat/ protocol package.
// ABOUTME: Handles alias resolution, formatting, and notification integration.

package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/awebai/aw/chat"
	"github.com/beadhub/bdh/internal/config"
)

// defaultChatWait mirrors chat.DefaultWait from the protocol package.
// The protocol package uses this value to detect "user didn't set --wait" and
// applies a 5-minute wait for --start-conversation.
const defaultChatWait = chat.DefaultWait

var (
	chatJSON              bool
	chatWait              int
	chatStartConversation bool
	chatLeaveConversation bool
)

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Persistent chat sessions",
	Long: `Persistent chat sessions.

Sessions are persistent per participant set - one session exists forever
between any pair (or group) of agents. Just send messages, no joining needed.

Agent name supports fuzzy matching:
  1. Exact match
  2. Unique prefix match (e.g., "coord" -> "coordinator")
  3. Unique substring match (e.g., "main" -> "claude-main")

Examples:
  bdh :aweb chat send bob "Can you help with the API design?" --start-conversation
  bdh :aweb chat send bob "Yes, here's my suggestion..."
  bdh :aweb chat send bob "Thanks, I'm done here." --leave-conversation
  bdh :aweb chat open bob
  bdh :aweb chat pending
  bdh :aweb chat history bob`,
}

var chatSendCmd = &cobra.Command{
	Use:   "send <alias> <message>",
	Short: "Send a message in a chat session",
	Long: `Send a message to one or more agents in a persistent chat session.

By default, waits 120 seconds for a reply. Use --start-conversation for
a 5-minute wait when initiating a new exchange. Use --leave-conversation
to send a final message and exit immediately.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("invalid .beadhub config: %w", err)
		}
		if err := validateRepoOriginMatchesCurrent(cfg); err != nil {
			return err
		}

		if chatStartConversation && chatLeaveConversation {
			return fmt.Errorf("--start-conversation and --leave-conversation are mutually exclusive")
		}

		if chatLeaveConversation {
			if cmd.Flags().Changed("wait") && chatWait != 0 {
				return fmt.Errorf("--leave-conversation cannot be combined with --wait (it always exits immediately)")
			}
			chatWait = 0
		}

		if chatStartConversation && cmd.Flags().Changed("wait") && chatWait == 0 {
			return fmt.Errorf("--start-conversation cannot be combined with --wait 0 (it is meant to wait for a reply)")
		}

		if strings.TrimSpace(args[1]) == "" {
			return fmt.Errorf("message cannot be empty")
		}

		baseCtx := cmd.Context()
		targetAgents, err := resolveTargetAliases(baseCtx, cfg, args[0])
		if err != nil {
			return err
		}
		for _, t := range targetAgents {
			SetExcludeChatAlias(t)
		}

		aw, err := newAwebClientRequired(cfg.BeadhubURL)
		if err != nil {
			return err
		}

		opts := chat.SendOptions{
			Wait:              chatWait,
			Leaving:           chatLeaveConversation,
			StartConversation: chatStartConversation,
		}

		ctx, cancel := context.WithTimeout(baseCtx, chat.MaxSendTimeout)
		defer cancel()

		result, err := chat.Send(ctx, aw, cfg.Alias, targetAgents, args[1], opts, chatStatusCallback)
		if err != nil {
			return err
		}
		fmt.Print(formatChatOutput(result, chatJSON))
		return nil
	},
}

var chatPendingCmd = &cobra.Command{
	Use:   "pending",
	Short: "List conversations with unread messages",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("invalid .beadhub config: %w", err)
		}
		if err := validateRepoOriginMatchesCurrent(cfg); err != nil {
			return err
		}

		aw, err := newAwebClientRequired(cfg.BeadhubURL)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), apiTimeout)
		defer cancel()

		result, err := chat.Pending(ctx, aw)
		if err != nil {
			return err
		}
		fmt.Print(formatPendingOutput(result, cfg.Alias, chatJSON))
		return nil
	},
}

var chatOpenCmd = &cobra.Command{
	Use:   "open <alias>",
	Short: "Read unread messages and mark as read",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("invalid .beadhub config: %w", err)
		}
		if err := validateRepoOriginMatchesCurrent(cfg); err != nil {
			return err
		}

		baseCtx := cmd.Context()
		targetAgent, err := resolveTargetAlias(baseCtx, cfg, args[0])
		if err != nil {
			return err
		}
		SetExcludeChatAlias(targetAgent)

		aw, err := newAwebClientRequired(cfg.BeadhubURL)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(baseCtx, apiTimeout)
		defer cancel()

		result, err := chat.Open(ctx, aw, targetAgent)
		if err != nil {
			return err
		}
		fmt.Print(formatChatOpenOutput(result, chatJSON))
		return nil
	},
}

var chatHistoryCmd = &cobra.Command{
	Use:   "history <alias>",
	Short: "Show conversation history",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("invalid .beadhub config: %w", err)
		}
		if err := validateRepoOriginMatchesCurrent(cfg); err != nil {
			return err
		}

		baseCtx := cmd.Context()
		targetAgent, err := resolveTargetAlias(baseCtx, cfg, args[0])
		if err != nil {
			return err
		}
		SetExcludeChatAlias(targetAgent)

		aw, err := newAwebClientRequired(cfg.BeadhubURL)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(baseCtx, apiTimeout)
		defer cancel()

		result, err := chat.History(ctx, aw, targetAgent)
		if err != nil {
			return err
		}
		fmt.Print(formatHistoryOutput(result, chatJSON))
		return nil
	},
}

var chatHangOnCmd = &cobra.Command{
	Use:   "hang-on <alias> <message>",
	Short: "Request more time before replying",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("invalid .beadhub config: %w", err)
		}
		if err := validateRepoOriginMatchesCurrent(cfg); err != nil {
			return err
		}

		if strings.TrimSpace(args[1]) == "" {
			return fmt.Errorf("message cannot be empty")
		}

		baseCtx := cmd.Context()
		targetAgent, err := resolveTargetAlias(baseCtx, cfg, args[0])
		if err != nil {
			return err
		}
		SetExcludeChatAlias(targetAgent)

		aw, err := newAwebClientRequired(cfg.BeadhubURL)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(baseCtx, apiTimeout)
		defer cancel()

		result, err := chat.HangOn(ctx, aw, targetAgent, args[1])
		if err != nil {
			return err
		}
		fmt.Print(formatHangOnOutput(result, chatJSON))
		return nil
	},
}

func init() {
	chatCmd.AddCommand(chatSendCmd)
	chatCmd.AddCommand(chatPendingCmd)
	chatCmd.AddCommand(chatOpenCmd)
	chatCmd.AddCommand(chatHistoryCmd)
	chatCmd.AddCommand(chatHangOnCmd)

	chatCmd.PersistentFlags().BoolVar(&chatJSON, "json", false, "Output in JSON format")

	chatSendCmd.Flags().IntVar(&chatWait, "wait", defaultChatWait, "Timeout in seconds (0 to not wait)")
	chatSendCmd.Flags().BoolVar(&chatStartConversation, "start-conversation", false, "Initiate a new exchange (5 min wait)")
	chatSendCmd.Flags().BoolVar(&chatLeaveConversation, "leave-conversation", false, "Send final message and exit (no wait)")
}

// resolveTargetAlias resolves a single alias with fuzzy matching and prevents self-chat.
func resolveTargetAlias(ctx context.Context, cfg *config.Config, target string) (string, error) {
	targets, err := resolveTargetAliases(ctx, cfg, target)
	if err != nil {
		return "", err
	}
	if len(targets) != 1 {
		return "", fmt.Errorf("expected exactly 1 target, got %d (use single target for this mode)", len(targets))
	}
	return targets[0], nil
}

// resolveTargetAliases resolves comma-separated aliases with fuzzy matching.
// Each part is resolved individually. Prevents chatting with self.
func resolveTargetAliases(ctx context.Context, cfg *config.Config, targetInput string) ([]string, error) {
	httpClient := newBeadHubClient(cfg.BeadhubURL)

	resolveCtx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	parts := strings.Split(targetInput, ",")
	var targets []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		resolution, err := resolveAlias(resolveCtx, cfg, httpClient, part)
		if err != nil {
			return nil, err
		}

		targetAgent := resolution.Alias
		if targetAgent == "" {
			targetAgent = part
		}

		if targetAgent == cfg.Alias {
			return nil, fmt.Errorf("cannot chat with yourself")
		}

		targets = append(targets, targetAgent)
	}

	if len(targets) == 0 {
		return nil, fmt.Errorf("no valid target agents specified")
	}

	return targets, nil
}

// chatStatusCallback writes chat protocol status updates to stderr.
func chatStatusCallback(kind, message string) {
	if !chatJSON {
		fmt.Fprintf(os.Stderr, "[chat] %s\n", message)
	}
}

// formatChatOutput formats the chat send result for display.
func formatChatOutput(result *chat.SendResult, asJSON bool) string {
	if asJSON {
		data, _ := json.MarshalIndent(result, "", "  ")
		return string(data) + "\n"
	}

	var sb strings.Builder

	writeChatLine := func(prefix, agent, ts string) {
		timeAgo := ""
		if ts != "" {
			timeAgo = formatTimeAgo(ts)
		}
		if timeAgo != "" {
			sb.WriteString(fmt.Sprintf("%s: %s — %s\n", prefix, agent, timeAgo))
		} else {
			sb.WriteString(fmt.Sprintf("%s: %s\n", prefix, agent))
		}
	}

	firstTimestamp := ""
	if len(result.Events) > 0 {
		firstTimestamp = result.Events[0].Timestamp
	}

	switch result.Status {
	case "replied":
		writeChatLine("Chat from", result.TargetAgent, firstTimestamp)
		sb.WriteString(fmt.Sprintf("Body: %s\n", result.Reply))
		return sb.String()

	case "sender_left":
		writeChatLine("Chat from", result.TargetAgent, firstTimestamp)
		sb.WriteString(fmt.Sprintf("Body: %s\n", result.Reply))
		sb.WriteString(fmt.Sprintf("Note: %s has left the exchange\n", result.TargetAgent))
		return sb.String()

	case "pending":
		lastFrom := result.TargetAgent
		if len(result.Events) > 0 && result.Events[0].FromAgent != "" {
			lastFrom = result.Events[0].FromAgent
		}

		if lastFrom == result.TargetAgent {
			writeChatLine("Chat from", result.TargetAgent, firstTimestamp)
			if result.SenderWaiting {
				sb.WriteString("Status: WAITING for your reply\n")
			}
			sb.WriteString(fmt.Sprintf("Body: %s\n", result.Reply))
			sb.WriteString(fmt.Sprintf("Next: Run \"bdh :aweb chat send %s \\\"your reply\\\"\"\n", result.TargetAgent))
		} else {
			writeChatLine("Chat to", result.TargetAgent, firstTimestamp)
			sb.WriteString(fmt.Sprintf("Body: %s\n", result.Reply))
			sb.WriteString(fmt.Sprintf("Awaiting reply from %s.\n", result.TargetAgent))
		}
		return sb.String()

	case "sent":
		sb.WriteString(fmt.Sprintf("Message sent to %s\n", result.TargetAgent))
		if result.TargetNotConnected {
			sb.WriteString(fmt.Sprintf("Note: %s was not connected.\n", result.TargetAgent))
		}
		if result.WaitedSeconds > 0 {
			sb.WriteString(fmt.Sprintf("Waited %ds — no reply\n", result.WaitedSeconds))
		}
		return sb.String()

	case "targets_left":
		sb.WriteString(fmt.Sprintf("Message sent to %s\n", result.TargetAgent))
		sb.WriteString(fmt.Sprintf("%s previously left the conversation.\n", result.TargetAgent))
		sb.WriteString(fmt.Sprintf("To start a new exchange, run: \"bdh :aweb chat send %s \\\"message\\\" --start-conversation\"\n", result.TargetAgent))
		return sb.String()
	}

	// Fallback: show message events.
	messageIndex := 0
	for _, event := range result.Events {
		if event.Type != "message" {
			continue
		}
		if messageIndex > 0 {
			sb.WriteString("\n---\n\n")
		}
		writeChatLine("Chat from", event.FromAgent, event.Timestamp)
		sb.WriteString(fmt.Sprintf("Body: %s\n", event.Body))
		messageIndex++
	}

	if sb.Len() == 0 {
		sb.WriteString("No chat events.\n")
	}

	return sb.String()
}

// formatPendingOutput formats the pending chats result for display.
// selfAlias is used to filter the current user from the participants list.
func formatPendingOutput(result *chat.PendingResult, selfAlias string, asJSON bool) string {
	if asJSON {
		// JSON output is "discovery only": do not include message bodies.
		// Agents should open a conversation to read messages (and mark them read).
		type pendingConversationSummary struct {
			SessionID     string   `json:"session_id"`
			Participants  []string `json:"participants"`
			LastFrom      string   `json:"last_from"`
			UnreadCount   int      `json:"unread_count"`
			LastActivity  string   `json:"last_activity"`
			SenderWaiting bool     `json:"sender_waiting"`
		}
		type pendingResultSummary struct {
			Pending []pendingConversationSummary `json:"pending"`
		}

		summary := pendingResultSummary{
			Pending: make([]pendingConversationSummary, 0, len(result.Pending)),
		}
		for _, p := range result.Pending {
			summary.Pending = append(summary.Pending, pendingConversationSummary{
				SessionID:     p.SessionID,
				Participants:  p.Participants,
				LastFrom:      p.LastFrom,
				UnreadCount:   p.UnreadCount,
				LastActivity:  p.LastActivity,
				SenderWaiting: p.SenderWaiting,
			})
		}

		data, _ := json.MarshalIndent(summary, "", "  ")
		return string(data) + "\n"
	}

	if len(result.Pending) == 0 {
		return "No pending conversations\n"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("CHATS: %d unread conversation(s)\n\n", len(result.Pending)))

	for _, p := range result.Pending {
		otherAliases := make([]string, 0, len(p.Participants))
		for _, participant := range p.Participants {
			if participant == selfAlias {
				continue
			}
			otherAliases = append(otherAliases, participant)
		}

		openTarget := p.LastFrom
		if openTarget == "" && len(otherAliases) > 0 {
			openTarget = otherAliases[0]
		}
		openHint := ""
		if openTarget != "" {
			openHint = fmt.Sprintf(" — Run \"bdh :aweb chat open %s\"", openTarget)
		}

		if p.SenderWaiting {
			timeInfo := ""
			if p.TimeRemainingSeconds != nil && *p.TimeRemainingSeconds < 60 && *p.TimeRemainingSeconds > 0 {
				timeInfo = fmt.Sprintf(" (%ds left)", *p.TimeRemainingSeconds)
			}
			sb.WriteString(fmt.Sprintf("  CHAT WAITING: %s%s (unread: %d)%s\n", p.LastFrom, timeInfo, p.UnreadCount, openHint))
		} else {
			sb.WriteString(fmt.Sprintf("  CHAT: %s (unread: %d)%s\n", p.LastFrom, p.UnreadCount, openHint))
		}
	}

	return sb.String()
}

// formatHistoryOutput formats the chat history result for display.
func formatHistoryOutput(result *chat.HistoryResult, asJSON bool) string {
	if asJSON {
		data, _ := json.MarshalIndent(result, "", "  ")
		return string(data) + "\n"
	}

	if len(result.Messages) == 0 {
		return "No messages in conversation\n"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Conversation history (%d messages):\n\n", len(result.Messages)))

	for _, m := range result.Messages {
		timestamp := ""
		if m.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339, m.Timestamp); err == nil {
				timestamp = t.Format("15:04:05")
			}
		}
		if timestamp != "" {
			sb.WriteString(fmt.Sprintf("[%s] %s: %s\n", timestamp, m.FromAgent, m.Body))
		} else {
			sb.WriteString(fmt.Sprintf("%s: %s\n", m.FromAgent, m.Body))
		}
	}

	return sb.String()
}

// formatChatOpenOutput formats the open result for display.
func formatChatOpenOutput(result *chat.OpenResult, asJSON bool) string {
	if asJSON {
		data, _ := json.MarshalIndent(result, "", "  ")
		return string(data) + "\n"
	}

	if len(result.Messages) == 0 {
		if result.UnreadWasEmpty {
			return fmt.Sprintf("No unread chat messages for %s\n", result.TargetAgent)
		}
		return "No unread chat messages\n"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Unread chat messages (%d marked as read):\n\n", result.MarkedRead))
	if result.SenderWaiting {
		sb.WriteString(fmt.Sprintf("Status: %s is WAITING for your reply\n\n", result.TargetAgent))
	}

	for i, m := range result.Messages {
		if i > 0 {
			sb.WriteString("\n---\n\n")
		}
		ts := ""
		if m.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339, m.Timestamp); err == nil {
				ts = t.Format("15:04:05")
			}
		}
		if ts != "" {
			sb.WriteString(fmt.Sprintf("[%s] %s: %s\n", ts, m.FromAgent, m.Body))
		} else {
			sb.WriteString(fmt.Sprintf("%s: %s\n", m.FromAgent, m.Body))
		}
	}

	sb.WriteString(fmt.Sprintf("\nNext: Run \"bdh :aweb chat send %s \\\"your reply\\\"\"", result.TargetAgent))
	if result.SenderWaiting {
		sb.WriteString(fmt.Sprintf(" or \"bdh :aweb chat hang-on %s \\\"message\\\"\"", result.TargetAgent))
	}
	sb.WriteString("\n")

	return sb.String()
}

// formatHangOnOutput formats the hang-on result for display.
func formatHangOnOutput(result *chat.HangOnResult, asJSON bool) string {
	if asJSON {
		data, _ := json.MarshalIndent(result, "", "  ")
		return string(data) + "\n"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Sent hang-on to %s\n", result.TargetAgent))
	sb.WriteString(fmt.Sprintf("Message: %s\n", result.Message))
	if result.ExtendsWaitSeconds > 0 {
		minutes := result.ExtendsWaitSeconds / 60
		sb.WriteString(fmt.Sprintf("%s's wait extended by %d min\n", result.TargetAgent, minutes))
	}
	return sb.String()
}
