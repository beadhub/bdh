package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	aweb "github.com/awebai/aw"
	"github.com/awebai/aw/chat"
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
	PendingChatAlias string
	UnreadMailCount  int
	UnreadMailFrom   string
	CurrentClaim     *ClaimInfo
	ReadyTask        *runReadyTask
}

type runReadyTask struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type runDispatchDefaults struct {
	IdleWaitSeconds int
}

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

	if pendingResp, err := chat.Pending(ctx, d.aw); err == nil && len(pendingResp.Pending) > 0 {
		summary.PendingChatAlias = pendingResp.Pending[0].LastFrom
	}

	if inboxResp, err := d.aw.Inbox(ctx, aweb.InboxParams{
		UnreadOnly: true,
		Limit:      10,
	}); err == nil {
		summary.UnreadMailCount = len(inboxResp.Messages)
		if len(inboxResp.Messages) > 0 {
			summary.UnreadMailFrom = inboxResp.Messages[0].FromAlias
		}
	}

	if status, err := fetchStatusWithConfig(d.cfg); err == nil && len(status.YourClaims) > 0 {
		claim := status.YourClaims[0]
		summary.CurrentClaim = &claim
	}

	if readyTasks, err := d.readyTasks(ctx); err == nil && len(readyTasks) > 0 {
		task := readyTasks[0]
		summary.ReadyTask = &task
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
	return defaults
}

func selectRunDispatch(summary runDispatchSummary, defaults runDispatchDefaults) runDispatchDecision {
	defaults = withRunDispatchDefaults(defaults)

	switch {
	case strings.TrimSpace(summary.PendingChatAlias) != "":
		return runDispatchDecision{
			Prompt:      fmt.Sprintf("Respond to chat from %s. Read the unread exchange, reply if needed, and clear the pending conversation before switching focus.", summary.PendingChatAlias),
			WaitSeconds: 5,
		}
	case summary.UnreadMailCount > 0:
		prompt := "Check and respond to unread mail. Triage the inbox, reply where needed, and coordinate any blockers."
		if strings.TrimSpace(summary.UnreadMailFrom) != "" {
			prompt = fmt.Sprintf("Check unread mail from %s first, then triage the rest of the inbox and reply where needed.", summary.UnreadMailFrom)
		}
		return runDispatchDecision{
			Prompt:      prompt,
			WaitSeconds: 5,
		}
	case summary.CurrentClaim != nil:
		return runDispatchDecision{
			Prompt:      buildClaimPrompt(*summary.CurrentClaim),
			WaitSeconds: 20,
		}
	case summary.ReadyTask != nil:
		return runDispatchDecision{
			Prompt:      buildReadyTaskPrompt(*summary.ReadyTask),
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
		return fmt.Sprintf("Continue working on %s. Before closing the bead, run a self-review or code-reviewer pass on your changes.", claim.BeadID)
	}
	return fmt.Sprintf("Continue working on %s: %s. Before closing the bead, run a self-review or code-reviewer pass on your changes.", claim.BeadID, title)
}

func buildReadyTaskPrompt(task runReadyTask) string {
	title := strings.TrimSpace(task.Title)
	if title == "" {
		return fmt.Sprintf("Pick up %s if it is still appropriate, work on it, and before closing the bead run a self-review or code-reviewer pass on your changes.", task.ID)
	}
	return fmt.Sprintf("Pick up %s: %s. Claim it if appropriate, work on it, and before closing the bead run a self-review or code-reviewer pass on your changes.", task.ID, title)
}
