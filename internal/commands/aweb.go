package commands

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	aweb "github.com/awebai/aw"
)

var awebCmd = &cobra.Command{
	Use:   ":aweb",
	Short: "aweb protocol commands (mail/chat/locks/identity)",
	Long: `Escape hatch to aweb protocol commands.

BeadHub implements the aweb protocol and serves the aweb routes in the same server.

Examples:
  bdh :aweb who
  bdh :aweb whoami
  bdh :aweb mail send alice "hello"
  bdh :aweb locks
  bdh :aweb lock src/api.py`,
}

func init() {
	awebCmd.AddCommand(awebWhoCmd)
	awebCmd.AddCommand(awebWhoamiCmd)
	awebCmd.AddCommand(awebMailCmd)
	awebCmd.AddCommand(chatCmd)
	awebCmd.AddCommand(awebLockCmd)
	awebCmd.AddCommand(awebUnlockCmd)
	awebCmd.AddCommand(awebLocksCmd)
}

func currentAgentIdentityForAweb() (*beadhubAuthSelection, error) {
	sel, err := resolveBeadhubAuth("")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(sel.APIKey) == "" {
		return nil, fmt.Errorf("missing beadhub API key (configure ~/.config/aw/config.yaml + .aw/context, or set BEADHUB_API_KEY)")
	}
	if strings.TrimSpace(sel.AgentID) == "" || strings.TrimSpace(sel.AgentAlias) == "" {
		return nil, fmt.Errorf("missing agent identity for aweb commands (agent_id/agent_alias not set in the selected account)")
	}
	return sel, nil
}

// =============================================================================
// whoami
// =============================================================================

var awebWhoamiJSON bool

var awebWhoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show the current aweb identity",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 0 {
			return fmt.Errorf("whoami takes no arguments")
		}

		client, err := newAwebClientRequired("")
		if err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(cmd.Context(), apiTimeout)
		defer cancel()

		resp, err := client.Introspect(ctx)
		if err != nil {
			return err
		}

		if awebWhoamiJSON {
			fmt.Print(marshalJSONOrFallback(resp))
			fmt.Print("\n")
			return nil
		}

		fmt.Printf("Project: %s\n", resp.ProjectID)
		if resp.AgentID != "" {
			fmt.Printf("Agent:   %s\n", resp.AgentID)
		}
		if resp.Alias != "" {
			fmt.Printf("Alias:   %s\n", resp.Alias)
		}
		if resp.HumanName != "" {
			fmt.Printf("Human:   %s\n", resp.HumanName)
		}
		if resp.AgentType != "" {
			fmt.Printf("Type:    %s\n", resp.AgentType)
		}
		return nil
	},
}

func init() {
	awebWhoamiCmd.Flags().BoolVar(&awebWhoamiJSON, "json", false, "Output as JSON")
}

// =============================================================================
// who
// =============================================================================

var (
	awebWhoJSON       bool
	awebWhoOnlineOnly bool
)

var awebWhoCmd = &cobra.Command{
	Use:   "who",
	Short: "List agents in the current project",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 0 {
			return fmt.Errorf("who takes no arguments")
		}

		client, err := newAwebClientRequired("")
		if err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(cmd.Context(), apiTimeout)
		defer cancel()

		resp, err := client.ListAgents(ctx)
		if err != nil {
			return err
		}

		agents := make([]aweb.AgentView, 0, len(resp.Agents))
		for _, agent := range resp.Agents {
			if awebWhoOnlineOnly && !agent.Online {
				continue
			}
			agents = append(agents, agent)
		}

		if awebWhoJSON {
			fmt.Print(marshalJSONOrFallback(struct {
				ProjectID string           `json:"project_id"`
				Agents    []aweb.AgentView `json:"agents"`
			}{
				ProjectID: resp.ProjectID,
				Agents:    agents,
			}))
			fmt.Print("\n")
			return nil
		}

		var online, offline []aweb.AgentView
		for _, agent := range agents {
			if agent.Online {
				online = append(online, agent)
			} else {
				offline = append(offline, agent)
			}
		}
		sort.Slice(online, func(i, j int) bool { return online[i].Alias < online[j].Alias })
		sort.Slice(offline, func(i, j int) bool { return offline[i].Alias < offline[j].Alias })

		fmt.Printf("Project: %s\n\n", resp.ProjectID)
		if len(online) > 0 {
			fmt.Println("ONLINE")
			for _, agent := range online {
				desc := strings.TrimSpace(agent.Status)
				if desc == "" {
					desc = "active"
				}
				fmt.Printf("  %s (%s) — %s\n", agent.Alias, agent.AgentType, desc)
			}
			fmt.Println()
		}
		if len(offline) > 0 && !awebWhoOnlineOnly {
			fmt.Println("OFFLINE")
			for _, agent := range offline {
				fmt.Printf("  %s (%s)\n", agent.Alias, agent.AgentType)
			}
		}
		return nil
	},
}

func init() {
	awebWhoCmd.Flags().BoolVar(&awebWhoJSON, "json", false, "Output as JSON")
	awebWhoCmd.Flags().BoolVar(&awebWhoOnlineOnly, "online-only", false, "Only show online agents")
}

// =============================================================================
// mail
// =============================================================================

var awebMailCmd = &cobra.Command{
	Use:   "mail",
	Short: "Async messages (mail)",
}

var (
	awebMailJSON     bool
	awebMailSubject  string
	awebMailPriority string
	awebMailAll      bool
	awebMailLimit    int
)

var awebMailSendCmd = &cobra.Command{
	Use:   "send <alias> <message>",
	Short: "Send a message",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		targetAlias := strings.TrimSpace(args[0])
		body := args[1]
		if targetAlias == "" {
			return fmt.Errorf("alias cannot be empty")
		}
		if strings.TrimSpace(body) == "" {
			return fmt.Errorf("message cannot be empty")
		}

		identity, err := currentAgentIdentityForAweb()
		if err != nil {
			return err
		}
		client, err := aweb.NewWithAPIKey(identity.BaseURL, identity.APIKey)
		if err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(cmd.Context(), apiTimeout)
		defer cancel()

		resp, err := client.SendMessage(ctx, &aweb.SendMessageRequest{
			ToAlias:  targetAlias,
			Subject:  strings.TrimSpace(awebMailSubject),
			Body:     body,
			Priority: aweb.MessagePriority(strings.TrimSpace(awebMailPriority)),
		})
		if err != nil {
			return err
		}

		if awebMailJSON {
			fmt.Print(marshalJSONOrFallback(resp))
			fmt.Print("\n")
			return nil
		}

		fmt.Printf("Sent mail to %s (message_id=%s)\n", targetAlias, resp.MessageID)
		return nil
	},
}

var awebMailListCmd = &cobra.Command{
	Use:   "list",
	Short: "List inbox messages",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 0 {
			return fmt.Errorf("mail list takes no arguments")
		}
		identity, err := currentAgentIdentityForAweb()
		if err != nil {
			return err
		}
		client, err := aweb.NewWithAPIKey(identity.BaseURL, identity.APIKey)
		if err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(cmd.Context(), apiTimeout)
		defer cancel()

		resp, err := client.Inbox(ctx, aweb.InboxParams{
			UnreadOnly: !awebMailAll,
			Limit:      awebMailLimit,
		})
		if err != nil {
			return err
		}

		if awebMailJSON {
			fmt.Print(marshalJSONOrFallback(resp))
			fmt.Print("\n")
			return nil
		}

		if len(resp.Messages) == 0 {
			if awebMailAll {
				fmt.Println("No messages.")
			} else {
				fmt.Println("No unread messages.")
			}
			return nil
		}

		fmt.Printf("MAILS: %d\n\n", len(resp.Messages))
		for _, msg := range resp.Messages {
			subj := strings.TrimSpace(msg.Subject)
			if subj != "" {
				subj = " — " + subj
			}
			fmt.Printf("- %s%s: %s\n", msg.FromAlias, subj, msg.Body)
		}
		return nil
	},
}

var awebMailOpenCmd = &cobra.Command{
	Use:   "open <alias>",
	Short: "Show unread messages from alias and acknowledge them",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		targetAlias := strings.TrimSpace(args[0])
		if targetAlias == "" {
			return fmt.Errorf("alias cannot be empty")
		}
		identity, err := currentAgentIdentityForAweb()
		if err != nil {
			return err
		}
		client, err := aweb.NewWithAPIKey(identity.BaseURL, identity.APIKey)
		if err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(cmd.Context(), apiTimeout)
		defer cancel()

		resp, err := client.Inbox(ctx, aweb.InboxParams{
			UnreadOnly: true,
			Limit:      200,
		})
		if err != nil {
			return err
		}

		var filtered []aweb.InboxMessage
		for _, msg := range resp.Messages {
			if msg.FromAlias == targetAlias {
				filtered = append(filtered, msg)
			}
		}
		if len(filtered) == 0 {
			fmt.Printf("No unread mail from %s.\n", targetAlias)
			return nil
		}

		for _, msg := range filtered {
			_, _ = client.AckMessage(ctx, msg.MessageID)
		}

		if awebMailJSON {
			fmt.Print(marshalJSONOrFallback(struct {
				From     string              `json:"from"`
				Messages []aweb.InboxMessage `json:"messages"`
			}{From: targetAlias, Messages: filtered}))
			fmt.Print("\n")
			return nil
		}

		fmt.Printf("Mail from %s (%d):\n\n", targetAlias, len(filtered))
		for _, msg := range filtered {
			fmt.Printf("%s\n\n", msg.Body)
		}
		return nil
	},
}

func init() {
	awebMailCmd.AddCommand(awebMailSendCmd)
	awebMailCmd.AddCommand(awebMailListCmd)
	awebMailCmd.AddCommand(awebMailOpenCmd)

	awebMailCmd.PersistentFlags().BoolVar(&awebMailJSON, "json", false, "Output as JSON")

	awebMailSendCmd.Flags().StringVar(&awebMailSubject, "subject", "", "Message subject")
	awebMailSendCmd.Flags().StringVar(&awebMailPriority, "priority", "normal", "Priority: low|normal|high|urgent")

	awebMailListCmd.Flags().BoolVar(&awebMailAll, "all", false, "Include read messages")
	awebMailListCmd.Flags().IntVar(&awebMailLimit, "limit", 50, "Max messages")
}

// =============================================================================
// chat (implementation in chat.go)
// =============================================================================

// =============================================================================
// locks
// =============================================================================

var (
	awebLocksJSON   bool
	awebLocksMine   bool
	awebLocksPrefix string
)

var awebLocksCmd = &cobra.Command{
	Use:   "locks",
	Short: "List active reservations",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 0 {
			return fmt.Errorf("locks takes no arguments")
		}
		identity, err := currentAgentIdentityForAweb()
		if err != nil {
			return err
		}
		client, err := aweb.NewWithAPIKey(identity.BaseURL, identity.APIKey)
		if err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(cmd.Context(), apiTimeout)
		defer cancel()

		resp, err := client.ReservationList(ctx, awebLocksPrefix)
		if err != nil {
			return err
		}

		res := resp.Reservations
		if awebLocksMine {
			filtered := res[:0]
			for _, r := range res {
				if r.HolderAlias == identity.AgentAlias {
					filtered = append(filtered, r)
				}
			}
			res = filtered
		}

		if awebLocksJSON {
			fmt.Print(marshalJSONOrFallback(struct {
				Reservations []aweb.ReservationView `json:"reservations"`
			}{Reservations: res}))
			fmt.Print("\n")
			return nil
		}

		if len(res) == 0 {
			if awebLocksMine {
				fmt.Println("You have no active locks.")
			} else {
				fmt.Println("No active locks.")
			}
			return nil
		}

		now := time.Now()
		for _, r := range res {
			fmt.Printf("- %s — %s (expires in %s)\n", r.ResourceKey, r.HolderAlias, formatDuration(ttlRemainingSeconds(r.ExpiresAt, now)))
		}
		return nil
	},
}

var (
	awebLockTTLSeconds int
	awebLockJSON       bool
)

var awebLockCmd = &cobra.Command{
	Use:   "lock <resource_key>",
	Short: "Acquire a reservation",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resourceKey := strings.TrimSpace(args[0])
		if resourceKey == "" {
			return fmt.Errorf("resource_key cannot be empty")
		}

		identity, err := currentAgentIdentityForAweb()
		if err != nil {
			return err
		}
		client, err := aweb.NewWithAPIKey(identity.BaseURL, identity.APIKey)
		if err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(cmd.Context(), apiTimeout)
		defer cancel()

		resp, err := client.ReservationAcquire(ctx, &aweb.ReservationAcquireRequest{
			ResourceKey: resourceKey,
			TTLSeconds:  awebLockTTLSeconds,
			Metadata: map[string]any{
				"reason": "manual",
			},
		})
		if err != nil {
			return err
		}

		if awebLockJSON {
			fmt.Print(marshalJSONOrFallback(resp))
			fmt.Print("\n")
			return nil
		}
		fmt.Printf("Locked %s\n", resourceKey)
		return nil
	},
}

var (
	awebUnlockJSON bool
)

var awebUnlockCmd = &cobra.Command{
	Use:   "unlock <resource_key>",
	Short: "Release a reservation",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resourceKey := strings.TrimSpace(args[0])
		if resourceKey == "" {
			return fmt.Errorf("resource_key cannot be empty")
		}

		identity, err := currentAgentIdentityForAweb()
		if err != nil {
			return err
		}
		client, err := aweb.NewWithAPIKey(identity.BaseURL, identity.APIKey)
		if err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(cmd.Context(), apiTimeout)
		defer cancel()

		resp, err := client.ReservationRelease(ctx, &aweb.ReservationReleaseRequest{
			ResourceKey: resourceKey,
		})
		if err != nil {
			return err
		}
		if awebUnlockJSON {
			fmt.Print(marshalJSONOrFallback(resp))
			fmt.Print("\n")
			return nil
		}
		fmt.Printf("Unlocked %s\n", resourceKey)
		return nil
	},
}

func init() {
	awebLocksCmd.Flags().BoolVar(&awebLocksMine, "mine", false, "Only show your locks")
	awebLocksCmd.Flags().StringVar(&awebLocksPrefix, "prefix", "", "Only show locks with this prefix")
	awebLocksCmd.Flags().BoolVar(&awebLocksJSON, "json", false, "Output as JSON")

	awebLockCmd.Flags().IntVar(&awebLockTTLSeconds, "ttl-seconds", reserveDefaultTTL, "TTL seconds")
	awebLockCmd.Flags().BoolVar(&awebLockJSON, "json", false, "Output as JSON")

	awebUnlockCmd.Flags().BoolVar(&awebUnlockJSON, "json", false, "Output as JSON")
}
