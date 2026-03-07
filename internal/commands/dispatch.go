package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	aweb "github.com/awebai/aw"
	"github.com/beadhub/bdh/internal/bd"
	"github.com/beadhub/bdh/internal/beads"
	"github.com/beadhub/bdh/internal/config"
)

type runDispatcher interface {
	Next(ctx context.Context) (runDispatchDecision, error)
}

type runDispatchDecision struct {
	Prompt      string
	WaitSeconds int
	Skip        bool
}

type runDispatchSummary struct {
	PendingChat  *runPendingChat
	UnreadMail   []runUnreadMail
	CurrentClaim *ClaimInfo
	ReadyTask    *runReadyTask
}

type runPendingChat struct {
	Alias    string
	Messages []runCommsMessage
}

type runUnreadMail struct {
	From    string
	Subject string
	Body    string
}

type runCommsMessage struct {
	From string
	Body string
}

type runReadyTask struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type runDispatchDefaults struct {
	IdleWaitSeconds      int
	IgnoreBeads          bool
	WorkPromptSuffix     string
	CommsPromptSuffix    string
	HasWorkPromptSuffix  bool
	HasCommsPromptSuffix bool
}

const (
	runDispatchChatHistoryLimit = 5
	runDispatchMailLimit        = 3
	runDispatchBodyLimit        = 500
)

type beadhubRunDispatcher struct {
	cfg        *config.Config
	aw         *aweb.Client
	defaults   runDispatchDefaults
	readyTasks func(ctx context.Context) ([]runReadyTask, error)
}

func newBeadhubRunDispatcher(cfg *config.Config, aw *aweb.Client, defaults runDispatchDefaults) runDispatcher {
	dispatcher := &beadhubRunDispatcher{
		cfg:      cfg,
		aw:       aw,
		defaults: withRunDispatchDefaults(defaults),
	}
	dispatcher.readyTasks = dispatcher.fetchReadyTasks
	return dispatcher
}

func (d *beadhubRunDispatcher) Next(ctx context.Context) (runDispatchDecision, error) {
	summary, err := d.summary(ctx)
	if err != nil {
		return runDispatchDecision{}, err
	}
	return selectRunDispatch(summary, d.defaults), nil
}

func (d *beadhubRunDispatcher) summary(ctx context.Context) (runDispatchSummary, error) {
	summary := runDispatchSummary{}

	if pendingResp, err := d.aw.ChatPending(ctx); err == nil && len(pendingResp.Pending) > 0 {
		item := pendingResp.Pending[0]
		pendingChat := &runPendingChat{Alias: strings.TrimSpace(item.LastFrom)}
		if historyResp, err := d.aw.ChatHistory(ctx, aweb.ChatHistoryParams{
			SessionID: item.SessionID,
			Limit:     runDispatchChatHistoryLimit,
		}); err == nil {
			pendingChat.Messages = buildRunCommsMessages(historyResp.Messages)
		}
		if len(pendingChat.Messages) == 0 && strings.TrimSpace(item.LastMessage) != "" {
			pendingChat.Messages = []runCommsMessage{{
				From: pendingChat.Alias,
				Body: item.LastMessage,
			}}
		}
		summary.PendingChat = pendingChat
	}

	if inboxResp, err := d.aw.Inbox(ctx, aweb.InboxParams{
		UnreadOnly: true,
		Limit:      runDispatchMailLimit,
	}); err == nil {
		summary.UnreadMail = buildRunUnreadMail(inboxResp.Messages)
	}

	if status, err := fetchStatusWithConfig(d.cfg); err == nil && len(status.YourClaims) > 0 {
		if !d.defaults.IgnoreBeads {
			claim := status.YourClaims[0]
			summary.CurrentClaim = &claim
		}
	}

	if !d.defaults.IgnoreBeads {
		if readyTasks, err := d.readyTasks(ctx); err == nil && len(readyTasks) > 0 {
			task := readyTasks[0]
			summary.ReadyTask = &task
		}
	}

	return summary, nil
}

func (d *beadhubRunDispatcher) fetchReadyTasks(ctx context.Context) ([]runReadyTask, error) {
	var result *bd.Result
	var err error

	if beads.IsInitialized() {
		result, err = bd.New().Run(ctx, []string{"ready", "--json"})
	} else {
		result, err = runNative(d.aw, []string{"ready", "--json"})
	}
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, errors.New(strings.TrimSpace(result.Stderr))
	}

	var tasks []runReadyTask
	if err := json.Unmarshal([]byte(result.Stdout), &tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

func withRunDispatchDefaults(defaults runDispatchDefaults) runDispatchDefaults {
	if defaults.IdleWaitSeconds == 0 {
		defaults.IdleWaitSeconds = defaultRunIdleWaitSeconds
	}
	if !defaults.HasWorkPromptSuffix {
		defaults.WorkPromptSuffix = defaultRunWorkPromptSuffix
	}
	if !defaults.HasCommsPromptSuffix {
		defaults.CommsPromptSuffix = defaultRunCommsPromptSuffix
	}
	return defaults
}

func selectRunDispatch(summary runDispatchSummary, defaults runDispatchDefaults) runDispatchDecision {
	defaults = withRunDispatchDefaults(defaults)

	switch {
	case summary.PendingChat != nil:
		return runDispatchDecision{
			Prompt:      appendRunPromptSuffix(buildPendingChatPrompt(*summary.PendingChat), defaults.CommsPromptSuffix),
			WaitSeconds: 5,
		}
	case len(summary.UnreadMail) > 0:
		return runDispatchDecision{
			Prompt:      appendRunPromptSuffix(buildUnreadMailPrompt(summary.UnreadMail), defaults.CommsPromptSuffix),
			WaitSeconds: 5,
		}
	case !defaults.IgnoreBeads && summary.CurrentClaim != nil:
		return runDispatchDecision{
			Prompt:      appendRunPromptSuffix(buildClaimPrompt(*summary.CurrentClaim), defaults.WorkPromptSuffix),
			WaitSeconds: 20,
		}
	case !defaults.IgnoreBeads && summary.ReadyTask != nil:
		return runDispatchDecision{
			Prompt:      appendRunPromptSuffix(buildReadyTaskPrompt(*summary.ReadyTask), defaults.WorkPromptSuffix),
			WaitSeconds: 20,
		}
	default:
		return runDispatchDecision{
			WaitSeconds: defaults.IdleWaitSeconds,
			Skip:        true,
		}
	}
}

func buildClaimPrompt(claim ClaimInfo) string {
	title := strings.TrimSpace(claim.Title)
	if title == "" {
		return fmt.Sprintf("Continue working on %s.", claim.BeadID)
	}
	return fmt.Sprintf("Continue working on %s: %s.", claim.BeadID, title)
}

func buildReadyTaskPrompt(task runReadyTask) string {
	title := strings.TrimSpace(task.Title)
	if title == "" {
		return fmt.Sprintf("Pick up %s if it is still appropriate, then work on it.", task.ID)
	}
	return fmt.Sprintf("Pick up %s: %s. Claim it if appropriate, then work on it.", task.ID, title)
}

func buildRunCommsMessages(messages []aweb.ChatMessage) []runCommsMessage {
	result := make([]runCommsMessage, 0, len(messages))
	for _, message := range messages {
		from := strings.TrimSpace(message.FromAddress)
		if from == "" {
			from = strings.TrimSpace(message.FromAgent)
		}
		result = append(result, runCommsMessage{
			From: from,
			Body: truncateRunDispatchBody(message.Body),
		})
	}
	return result
}

func buildRunUnreadMail(messages []aweb.InboxMessage) []runUnreadMail {
	result := make([]runUnreadMail, 0, len(messages))
	for _, message := range messages {
		from := strings.TrimSpace(message.FromAddress)
		if from == "" {
			from = strings.TrimSpace(message.FromAlias)
		}
		result = append(result, runUnreadMail{
			From:    from,
			Subject: strings.TrimSpace(message.Subject),
			Body:    truncateRunDispatchBody(message.Body),
		})
	}
	return result
}

func buildPendingChatPrompt(pending runPendingChat) string {
	alias := strings.TrimSpace(pending.Alias)
	if alias == "" {
		alias = "the pending conversation"
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Respond to chat from %s. Here is your current comms status; no need to check again before replying:\n", alias))
	for _, message := range pending.Messages {
		from := strings.TrimSpace(message.From)
		if from == "" {
			from = "unknown"
		}
		builder.WriteString(fmt.Sprintf("- %s: %s\n", from, strings.TrimSpace(message.Body)))
	}
	builder.WriteString("Respond as needed, then clear the pending conversation before switching focus.")
	return builder.String()
}

func buildUnreadMailPrompt(messages []runUnreadMail) string {
	var builder strings.Builder
	builder.WriteString("Here is your current comms status; no need to check again before replying:\n")
	for _, message := range messages {
		from := strings.TrimSpace(message.From)
		if from == "" {
			from = "unknown"
		}
		subject := strings.TrimSpace(message.Subject)
		if subject == "" {
			subject = "(no subject)"
		}
		builder.WriteString(fmt.Sprintf("- From: %s\n  Subject: %s\n  Body: %s\n", from, subject, strings.TrimSpace(message.Body)))
	}
	builder.WriteString("Respond to the unread mail as needed, then continue coordinating work.")
	return builder.String()
}

func truncateRunDispatchBody(text string) string {
	text = strings.TrimSpace(text)
	if len(text) <= runDispatchBodyLimit {
		return text
	}
	return strings.TrimSpace(text[:runDispatchBodyLimit]) + "..."
}

func appendRunPromptSuffix(prompt string, suffix string) string {
	prompt = strings.TrimSpace(prompt)
	suffix = strings.TrimSpace(suffix)
	if prompt == "" {
		return suffix
	}
	if suffix == "" {
		return prompt
	}
	return prompt + "\n\n" + suffix
}
