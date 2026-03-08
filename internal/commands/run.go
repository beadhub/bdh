package commands

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

type runCommandRunner func(ctx context.Context, dir string, argv []string, onLine func(string), stderrSink io.Writer) error
type runSleepFunc func(ctx context.Context, d time.Duration) error
type runWakeStream interface {
	Stream(ctx context.Context, deadline time.Time) (<-chan runWakeEvent, <-chan error)
}

type runLoop struct {
	provider          runProvider
	runner            runCommandRunner
	sleep             runSleepFunc
	wakeStream        runWakeStream
	serviceSupervisor runServiceSupervisor
	now               func() time.Time
	out               io.Writer
	control           runInputController
	dispatch          runDispatcher
	defaults          runDispatchDefaults
	screen            *runScreenManager
	inputPromptLabel  string
	logs              *runLogSession
	writeMu           sync.Mutex
}

type runLoopOptions struct {
	InitialPrompt       string
	Prompt              string
	WaitSeconds         int
	MaxRuns             int
	AutofeedBeads       bool
	ContinueMode        bool
	WorkingDir          string
	AllowedTools        string
	Model               string
	CompactThresholdPct int
	Services            []runServiceConfig
	Debug               bool
}

type runCycleDecision struct {
	MissionPrompt string
	Prompt        string
	WaitSeconds   int
	Skip          bool
}

type runState struct {
	Run                int
	SessionID          string
	RanOnce            bool
	RunInterrupted     bool
	PauseAfterRun      bool
	PauseNoticeShown   bool
	StopRequested      bool
	Paused             bool
	AutofeedBeads      bool
	ExitConfirmPending bool
	NextPrompt         string
	PendingInput       bool
	InputBuffer        string
	TextProbe          string
	SuppressText       bool
	StructuredOut      bool
	LastRunError       string
	LastRunUsage       runUsageStats
	HasRunUsage        bool
}

const (
	runPausedNoticeText = "paused. use /resume, /quit, or type a prompt to continue."
	runPausedStatusText = "paused: /resume, /quit, or type a prompt"
	runExitStatusText   = "exit bdh :run? [y/N]"
)

var (
	runWaitSeconds   int
	runContinueMode  bool
	runMaxRuns       int
	runIdleWait      int
	runBasePrompt    string
	runWorkPrompt    string
	runCommsPrompt   string
	runWorkingDir    string
	runAllowedTools  string
	runModel         string
	runCompactPct    int
	runProviderName  string
	runAutofeedBeads bool
	runInitConfig    bool
	runDebugMode     bool
)

var runCmd = &cobra.Command{
	Use:   ":run [prompt]",
	Short: "Run an AI coding agent in a loop",
	Long: `Run an AI coding agent in a loop.

Current implementation includes:
  - repeated provider invocations (currently Claude and Codex)
  - stream-json parsing and formatted output
  - provider session continuity when --continue is requested
  - /stop, /wait, /resume, /autofeed on|off, /quit, and prompt override controls
  - bdh-driven dispatch between runs (chat, mail, claims, ready work)
  - adaptive wait behavior based on dispatch priority

Future provider work will add more backends on top of the same loop.`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if runMaxRuns < 0 {
			return fmt.Errorf("--max-runs must be >= 0")
		}

		runCfg, err := loadRunUserConfig()
		if err != nil {
			return err
		}
		if runInitConfig {
			return initRunUserConfig(cmd.InOrStdin(), cmd.OutOrStdout(), runCfg)
		}
		settings, err := resolveRunSettings(
			runCfg,
			cmd.Flags().Changed("base-prompt"), runBasePrompt,
			cmd.Flags().Changed("work-prompt-suffix"), runWorkPrompt,
			cmd.Flags().Changed("comms-prompt-suffix"), runCommsPrompt,
			cmd.Flags().Changed("wait"), runWaitSeconds,
			cmd.Flags().Changed("idle-wait"), runIdleWait,
			cmd.Flags().Changed("compact-threshold-pct"), runCompactPct,
		)
		if err != nil {
			return err
		}

		provider, err := newRunProvider(runProviderName)
		if err != nil {
			return err
		}

		dispatchDefaults := runDispatchDefaults{
			IdleWaitSeconds:      settings.IdleWaitSeconds,
			WorkPromptSuffix:     settings.WorkPromptSuffix,
			CommsPromptSuffix:    settings.CommsPromptSuffix,
			HasWorkPromptSuffix:  true,
			HasCommsPromptSuffix: true,
		}

		var dispatcher runDispatcher
		var wakeStream runWakeStream
		inputPromptLabel := defaultRunInputPromptLabel
		cfg, cfgErr := loadAndValidateConfig()
		if cfgErr == nil {
			inputPromptLabel = runIdentityPromptLabel(cfg.ProjectSlug, cfg.CanonicalOrigin, cfg.RepoOrigin, cfg.Alias)
			if aw, awErr := newAwebClientRequired(cfg.BeadhubURL); awErr == nil {
				dispatcher = newBeadhubRunDispatcher(cfg, aw, dispatchDefaults)
			}
			if stream, streamErr := newRunEventStreamClient(cfg.BeadhubURL); streamErr == nil {
				wakeStream = stream
			}
		}

		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
		defer stop()

		screen := newRunScreenManager(cmd.InOrStdin(), cmd.OutOrStdout())
		if screen != nil {
			screen.promptLabel = inputPromptLabel
			screen.inputLine = inputPromptLabel
		}

		loop := &runLoop{
			provider:         provider,
			runner:           realRunCommand,
			sleep:            sleepWithContext,
			wakeStream:       wakeStream,
			now:              time.Now,
			out:              cmd.OutOrStdout(),
			control:          screen,
			dispatch:         dispatcher,
			defaults:         dispatchDefaults,
			screen:           screen,
			inputPromptLabel: inputPromptLabel,
		}

		opts := runLoopOptions{
			InitialPrompt:       strings.TrimSpace(strings.Join(args, " ")),
			Prompt:              settings.BasePrompt,
			WaitSeconds:         settings.WaitSeconds,
			MaxRuns:             runMaxRuns,
			AutofeedBeads:       runAutofeedBeads,
			ContinueMode:        runContinueMode,
			WorkingDir:          runWorkingDir,
			AllowedTools:        runAllowedTools,
			Model:               runModel,
			CompactThresholdPct: settings.CompactThreshold,
			Services:            settings.Services,
			Debug:               resolveRunDebugMode(cmd.Flags().Changed("debug"), runDebugMode),
		}

		err = loop.Run(ctx, opts)
		if err == nil || err == context.Canceled {
			return nil
		}
		return err
	},
}

func init() {
	runCmd.Flags().StringVar(&runBasePrompt, "base-prompt", "", "Override the configured base mission prompt for this run")
	runCmd.Flags().StringVar(&runWorkPrompt, "work-prompt-suffix", "", "Override the configured work cycle prompt suffix for this run")
	runCmd.Flags().StringVar(&runCommsPrompt, "comms-prompt-suffix", "", "Override the configured comms cycle prompt suffix for this run")
	runCmd.Flags().IntVar(&runWaitSeconds, "wait", defaultRunWaitSeconds, "Idle seconds between runs")
	runCmd.Flags().IntVar(&runIdleWait, "idle-wait", defaultRunIdleWaitSeconds, "Idle seconds between runs when nothing needs attention")
	runCmd.Flags().IntVar(&runCompactPct, "compact-threshold-pct", defaultRunCompactThreshold, "Run /compact after a successful cycle when context usage exceeds this percent (0 disables)")
	runCmd.Flags().BoolVar(&runContinueMode, "continue", false, "Continue the most recent provider session across runs")
	runCmd.Flags().BoolVar(&runContinueMode, "session", false, "Deprecated alias for --continue")
	_ = runCmd.Flags().MarkDeprecated("session", "use --continue instead")
	runCmd.Flags().IntVar(&runMaxRuns, "max-runs", 0, "Stop after N runs (0 means infinite)")
	runCmd.Flags().StringVar(&runWorkingDir, "dir", "", "Working directory for the agent process")
	runCmd.Flags().StringVar(&runAllowedTools, "allowed-tools", "", "Provider-specific allowed tools string")
	runCmd.Flags().StringVar(&runModel, "model", "", "Provider-specific model override")
	runCmd.Flags().StringVar(&runProviderName, "provider", "claude", "Agent provider to run")
	runCmd.Flags().BoolVar(&runAutofeedBeads, "autofeed-beads", false, "Wake for claimed or ready beads in addition to incoming comms")
	runCmd.Flags().BoolVar(&runInitConfig, "init", false, "Prompt for ~/.config/beadhub/run.json values and write them")
	runCmd.Flags().BoolVar(&runDebugMode, "debug", false, "Enable detailed bdh :run debug logging (or set BDH_RUN_DEBUG=1)")
}

func (l *runLoop) Run(ctx context.Context, opts runLoopOptions) error {
	if l.dispatch == nil && strings.TrimSpace(opts.Prompt) == "" && strings.TrimSpace(opts.InitialPrompt) == "" {
		return fmt.Errorf("prompt cannot be empty when dispatch is unavailable")
	}

	if l.control != nil {
		if err := l.control.Start(); err != nil {
			return err
		}
		defer func() { _ = l.control.Stop() }()
	}
	if l.screen != nil {
		if screenControl, ok := l.control.(*runScreenManager); ok && screenControl == l.screen {
			// Bubble Tea screen manager also owns input; avoid starting it twice.
		} else {
			if err := l.screen.Start(); err != nil {
				return err
			}
			defer func() { _ = l.screen.Stop() }()
		}
	}

	state := &runState{AutofeedBeads: opts.AutofeedBeads}
	logs, err := newRunLogSession(opts.WorkingDir, opts.Debug, l.now())
	if err != nil {
		return err
	}
	l.logs = logs
	defer func() {
		l.logf("run loop stop")
		_ = logs.Close()
	}()
	l.logf("run loop start provider=%s continue=%t max_runs=%d wait=%d services=%d", l.provider.Name(), opts.ContinueMode, opts.MaxRuns, opts.WaitSeconds, len(opts.Services))
	if opts.Debug {
		l.println(fmt.Sprintf("info: debug logs %s", logs.RunLogPath()))
	}
	serviceSupervisor := l.serviceSupervisor
	if serviceSupervisor == nil && len(opts.Services) > 0 {
		serviceSupervisor = newRunServiceManager(l.println)
	}
	if manager, ok := serviceSupervisor.(*runServiceManager); ok {
		manager.logs = logs
	}
	if serviceSupervisor != nil && len(opts.Services) > 0 {
		if err := serviceSupervisor.Start(ctx, opts.Services, opts.WorkingDir); err != nil {
			return err
		}
		defer func() { _ = serviceSupervisor.Stop() }()
	}

	for {
		decision, err := l.nextPrompt(ctx, opts, state)
		if err != nil {
			return err
		}
		if decision.Skip {
			l.logf("cycle decision skip wait=%d", decision.WaitSeconds)
			if err := l.waitForWork(ctx, decision.WaitSeconds, state); err != nil {
				if state.StopRequested && errors.Is(err, context.Canceled) {
					return nil
				}
				return err
			}
			continue
		}

		baseMissionPrompt := strings.TrimSpace(opts.Prompt)
		missionPrompt := resolveRunMissionPrompt(baseMissionPrompt, decision.MissionPrompt)
		prompt := composeRunPromptWithServices(missionPrompt, decision.Prompt, opts.Services)
		displayPrompt := runDisplayPrompt(missionPrompt, decision.Prompt)
		l.logf("cycle decision run wait=%d mission=%q cycle=%q", decision.WaitSeconds, truncateRunText(missionPrompt, 120), truncateRunText(decision.Prompt, 120))
		if strings.TrimSpace(prompt) == "" {
			if l.dispatch == nil && state.Run > 0 && strings.TrimSpace(opts.Prompt) == "" && strings.TrimSpace(opts.InitialPrompt) != "" {
				l.println("done: initial prompt consumed; use --base-prompt for a persistent mission.")
				return nil
			}
			return fmt.Errorf("prompt cannot be empty")
		}
		state.Run++
		if err := l.runOnce(ctx, opts, state, prompt, displayPrompt); err != nil {
			if state.StopRequested && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
				return nil
			}
			return err
		}
		if state.ExitConfirmPending {
			if err := l.waitForExitConfirmation(ctx, state); err != nil {
				if state.StopRequested && errors.Is(err, context.Canceled) {
					return nil
				}
				return err
			}
		}
		compacted, err := l.maybeAutoCompact(ctx, opts, state)
		if err != nil {
			if state.StopRequested && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
				return nil
			}
			return err
		}
		if compacted {
			if opts.MaxRuns > 0 && state.Run >= opts.MaxRuns {
				l.printf("\ndone: reached max-runs (%d)\n", opts.MaxRuns)
				return nil
			}
			continue
		}

		if opts.MaxRuns > 0 && state.Run >= opts.MaxRuns {
			l.printf("\ndone: reached max-runs (%d)\n", opts.MaxRuns)
			return nil
		}

		if err := l.waitForNextCycle(ctx, decision.WaitSeconds, state); err != nil {
			if state.StopRequested && errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
	}
}

func (l *runLoop) nextPrompt(ctx context.Context, opts runLoopOptions, state *runState) (runCycleDecision, error) {
	queuedMissionPrompt := strings.TrimSpace(state.NextPrompt)
	if queuedMissionPrompt != "" {
		state.NextPrompt = ""
	}
	explicitMissionPrompt := queuedMissionPrompt
	if explicitMissionPrompt == "" && state.Run == 0 {
		explicitMissionPrompt = strings.TrimSpace(opts.InitialPrompt)
	}
	if explicitMissionPrompt != "" {
		l.logf("next prompt explicit queued=%t initial=%t", queuedMissionPrompt != "", state.Run == 0 && queuedMissionPrompt == "")
		return runCycleDecision{
			MissionPrompt: explicitMissionPrompt,
			WaitSeconds:   opts.WaitSeconds,
		}, nil
	}
	if l.dispatch != nil {
		decision, err := l.dispatch.Next(ctx, state.AutofeedBeads)
		if err != nil {
			l.logf("dispatch error: %v", err)
			l.printf("info: dispatch failed: %v\n", err)
			defaults := withRunDispatchDefaults(l.defaults)
			l.println("info: waiting for dispatch recovery before starting a run.")
			return runCycleDecision{WaitSeconds: defaults.IdleWaitSeconds, Skip: true}, nil
		}
		cycle := runCycleDecision{
			Prompt:      decision.Prompt,
			WaitSeconds: decision.WaitSeconds,
			Skip:        decision.Skip,
		}
		l.logf("dispatch decision skip=%t wait=%d prompt=%q", cycle.Skip, cycle.WaitSeconds, truncateRunText(cycle.Prompt, 120))
		return cycle, nil
	}
	return runCycleDecision{
		MissionPrompt: explicitMissionPrompt,
		Prompt:        "",
		WaitSeconds:   opts.WaitSeconds,
	}, nil
}

func (l *runLoop) runOnce(ctx context.Context, opts runLoopOptions, state *runState, prompt string, displayPrompt string) error {
	header := fmt.Sprintf("run #%d", state.Run)
	return l.executeRun(ctx, opts, state, prompt, displayPrompt, header)
}

func (l *runLoop) executeRun(ctx context.Context, opts runLoopOptions, state *runState, prompt string, displayPrompt string, header string) error {
	state.LastRunError = ""
	state.LastRunUsage = runUsageStats{}
	state.HasRunUsage = false
	expectedSessionID := strings.TrimSpace(state.SessionID)
	followUpRun := state.RanOnce
	buildOpts := runBuildOptions{
		AllowedTools: opts.AllowedTools,
		Model:        opts.Model,
	}
	if followUpRun {
		if expectedSessionID == "" {
			return fmt.Errorf("provider %s did not report a session id for the previous run; cannot guarantee continuity", l.provider.Name())
		}
		buildOpts.SessionID = expectedSessionID
		buildOpts.ContinueSession = true
	} else if opts.ContinueMode {
		buildOpts.ContinueSession = true
	}

	argv, err := l.provider.BuildCommand(prompt, buildOpts)
	if err != nil {
		return err
	}
	l.logf("provider command mode=%s argv=%s", formatRunProviderMode(l.provider, buildOpts), strings.Join(argv, " "))

	l.printf("\n%s  %s  >  %s\n\n", header, l.now().Format("15:04:05"), truncateRunText(displayPrompt, 80))
	l.println(formatRunProviderMode(l.provider, buildOpts))
	l.println("type /wait, /autofeed off, /stop, /quit, or start typing to queue a prompt.")
	l.setStatusLine("running")
	l.renderInputPrompt(state)

	presenter := &runPresenterState{}
	state.TextProbe = ""
	state.SuppressText = false
	state.StructuredOut = false
	observedSessionID := ""
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() {
		if !state.Paused && !state.ExitConfirmPending {
			l.clearStatusLine()
		}
	}()

	errCh := make(chan error, 1)
	wakeControls := l.startWakeControlRelay(runCtx)
	var stderrSink io.WriteCloser
	if l.logs != nil {
		stderrSink, err = l.logs.OpenProviderStderr()
		if err != nil {
			return err
		}
		defer func() { _ = stderrSink.Close() }()
	}
	go func() {
		errCh <- l.runner(runCtx, opts.WorkingDir, argv, func(line string) {
			l.handleOutputLine(line, presenter, state, &observedSessionID)
		}, stderrSink)
	}()

	for {
		select {
		case err := <-errCh:
			l.drainPendingControlEvents(state, true)
			state.RanOnce = true
			if state.RunInterrupted {
				state.Paused = true
				state.PauseAfterRun = true
				state.RunInterrupted = false
				return nil
			}
			state.RunInterrupted = false
			if strings.TrimSpace(state.LastRunError) != "" {
				return errors.New(state.LastRunError)
			}
			if followUpRun {
				switch {
				case strings.TrimSpace(observedSessionID) == "":
					return fmt.Errorf("provider %s did not report a session id for follow-up run", l.provider.Name())
				case observedSessionID != expectedSessionID:
					return fmt.Errorf("provider %s switched sessions unexpectedly: expected %s, got %s", l.provider.Name(), expectedSessionID, observedSessionID)
				}
			}
			return err
		case event := <-l.controlEvents():
			l.applyControlEvent(event, state, true, cancel)
		case event, ok := <-wakeControls:
			if !ok {
				wakeControls = nil
				continue
			}
			l.applyControlEvent(event, state, true, cancel)
		case <-ctx.Done():
			cancel()
			state.StopRequested = true
			return ctx.Err()
		}
	}
}

func (l *runLoop) maybeAutoCompact(ctx context.Context, opts runLoopOptions, state *runState) (bool, error) {
	if opts.CompactThresholdPct <= 0 || state == nil || !state.HasRunUsage {
		return false, nil
	}
	pct := state.LastRunUsage.ContextPct()
	if pct <= float64(opts.CompactThresholdPct) {
		return false, nil
	}
	l.printf("\ninfo: context %.1f%% exceeds %d%%; running /compact\n", pct, opts.CompactThresholdPct)
	l.logf("auto-compact context_pct=%.1f threshold=%d", pct, opts.CompactThresholdPct)
	if err := l.executeRun(ctx, opts, state, "/compact", "/compact", "compact"); err != nil {
		return false, err
	}
	return true, nil
}

func (l *runLoop) startWakeControlRelay(ctx context.Context) <-chan runControlEvent {
	if l.wakeStream == nil {
		return nil
	}

	relay := make(chan runControlEvent, 8)
	go func() {
		defer close(relay)
		for ctx.Err() == nil {
			deadline := l.now().Add(5 * time.Minute)
			events, errs := l.wakeStream.Stream(ctx, deadline)
			streamOpen := true
			for streamOpen && ctx.Err() == nil {
				select {
				case evt, ok := <-events:
					if !ok {
						events = nil
						if errs == nil {
							streamOpen = false
						}
						continue
					}
					control, ok := runControlEventFromWakeEvent(evt)
					if !ok {
						continue
					}
					select {
					case <-ctx.Done():
						return
					case relay <- control:
					}
				case err, ok := <-errs:
					if !ok {
						errs = nil
						if events == nil {
							streamOpen = false
						}
						continue
					}
					if err != nil && ctx.Err() == nil {
						l.logf("active wake control stream failed: %v", err)
						time.Sleep(500 * time.Millisecond)
					}
					streamOpen = false
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return relay
}

func runControlEventFromWakeEvent(evt runWakeEvent) (runControlEvent, bool) {
	switch evt.Type {
	case runWakeEventControlPause:
		return runControlEvent{Type: runControlWait}, true
	case runWakeEventControlResume:
		return runControlEvent{Type: runControlResume}, true
	case runWakeEventControlInterrupt:
		return runControlEvent{Type: runControlStop}, true
	default:
		return runControlEvent{}, false
	}
}

func (l *runLoop) drainPendingControlEvents(state *runState, activeRun bool) {
	for {
		select {
		case event := <-l.controlEvents():
			l.applyControlEvent(event, state, activeRun, nil)
		default:
			return
		}
	}
}

func (l *runLoop) handleOutputLine(line string, presenter *runPresenterState, state *runState, observedSessionID *string) {
	event, err := l.provider.ParseOutput(line)
	if err != nil {
		l.logf("provider parse fallback line=%q err=%v", truncateRunText(line, 120), err)
		l.runPresenterEnsureTextSpacing(presenter)
		l.println(line)
		presenter.lastWasStructured = false
		presenter.lastWasText = false
		presenter.lastTextEndedWithNewline = true
		return
	}

	if sid := l.provider.SessionID(event); sid != "" {
		state.SessionID = sid
		if observedSessionID != nil {
			*observedSessionID = sid
		}
	}
	if event != nil && event.Usage != nil {
		state.LastRunUsage = *event.Usage
		state.HasRunUsage = true
	}

	switch event.Type {
	case runEventText:
		if l.shouldSuppressText(state, event.Text) {
			return
		}
		l.runPresenterEnsureTextSpacing(presenter)
		l.print(event.Text)
		presenter.lastWasText = true
		presenter.lastWasStructured = false
		presenter.lastTextEndedWithNewline = strings.HasSuffix(event.Text, "\n")
	case runEventToolCall:
		state.StructuredOut = true
		l.runPresenterEnsureStructuredSpacing(presenter)
		for _, call := range event.ToolCalls {
			for _, line := range formatRunToolCallLines(call) {
				l.printf("%s\n", line)
			}
		}
		presenter.lastWasStructured = true
	case runEventToolResult:
		state.StructuredOut = true
		l.runPresenterEnsureStructuredSpacing(presenter)
		if text := strings.TrimSpace(event.Text); text != "" {
			l.printf("  -> %s\n", truncateRunText(text, 150))
		}
		presenter.lastWasStructured = true
	case runEventDone:
		state.StructuredOut = true
		if event.IsError && strings.TrimSpace(event.Text) != "" {
			state.LastRunError = strings.TrimSpace(event.Text)
			l.logf("provider done error=%q", state.LastRunError)
		}
		l.runPresenterEnsureStructuredSpacing(presenter)
		l.printf("%s\n", formatRunDone(event))
		presenter.lastWasStructured = true
	case runEventSystem:
		state.StructuredOut = true
		l.runPresenterEnsureStructuredSpacing(presenter)
		if text := strings.TrimSpace(event.Text); text != "" {
			l.printf("info: %s\n", text)
		}
		presenter.lastWasStructured = true
	}
	l.renderInputPrompt(state)
}

func (l *runLoop) runPresenterEnsureTextSpacing(presenter *runPresenterState) {
	if presenter == nil {
		return
	}
	if presenter.lastWasStructured {
		l.print("\n")
		presenter.lastWasStructured = false
	}
}

func (l *runLoop) runPresenterEnsureStructuredSpacing(presenter *runPresenterState) {
	if presenter == nil {
		return
	}
	if presenter.lastWasText {
		if presenter.lastTextEndedWithNewline {
			l.print("\n")
		} else {
			l.print("\n\n")
		}
		presenter.lastWasText = false
		presenter.lastTextEndedWithNewline = false
	}
}

func (l *runLoop) shouldSuppressText(state *runState, text string) bool {
	if state == nil || state.StructuredOut {
		return false
	}
	if state.SuppressText {
		return true
	}

	state.TextProbe += text
	if len(state.TextProbe) > 4000 {
		state.TextProbe = state.TextProbe[len(state.TextProbe)-4000:]
	}

	probe := state.TextProbe
	if len(probe) < 200 {
		return false
	}

	if strings.Contains(probe, "BeadHub Coordination Rules") ||
		strings.Contains(probe, "Project Context") ||
		strings.Contains(probe, "Start Here (Every Session)") ||
		strings.Contains(probe, "bdh :policy") ||
		strings.Contains(probe, "AGENTS.md instructions") {
		state.SuppressText = true
		l.println("[suppressed prompt/policy echo]")
		return true
	}

	return false
}

func (l *runLoop) idle(ctx context.Context, seconds int) error {
	if seconds <= 0 {
		return nil
	}

	for remaining := seconds; remaining > 0; remaining-- {
		l.renderIdleLine("next run", remaining, nil)
		if err := l.sleep(ctx, time.Second); err != nil {
			l.clearStatusLine()
			return err
		}
	}
	l.clearStatusLine()
	return nil
}

func (l *runLoop) waitForNextCycle(ctx context.Context, waitSeconds int, state *runState) error {
	if state.StopRequested {
		return context.Canceled
	}
	if l.control == nil {
		return l.idle(ctx, waitSeconds)
	}
	if strings.TrimSpace(state.NextPrompt) != "" {
		return nil
	}

	if state.PauseAfterRun || state.Paused {
		state.Paused = true
		state.PauseAfterRun = false
		if !state.PendingInput && !state.PauseNoticeShown {
			l.println(runPausedNoticeText)
			state.PauseNoticeShown = true
		}
		return l.waitWhilePaused(ctx, state)
	}

	return l.idleWithControls(ctx, waitSeconds, state)
}

func (l *runLoop) waitForWork(ctx context.Context, waitSeconds int, state *runState) error {
	if l.wakeStream != nil {
		return l.waitForWorkEvents(ctx, waitSeconds, state)
	}
	return l.idleWithControlsLabel(ctx, waitSeconds, state, "waiting for work")
}

func (l *runLoop) waitForWorkEvents(ctx context.Context, waitSeconds int, state *runState) error {
	if waitSeconds <= 0 {
		return nil
	}
	if state.StopRequested {
		return context.Canceled
	}
	if strings.TrimSpace(state.NextPrompt) != "" {
		return nil
	}

	deadline := l.now().Add(time.Duration(waitSeconds) * time.Second)
	events, errs := l.wakeStream.Stream(ctx, deadline)
	l.setStatusLine("waiting for work")
	defer l.clearStatusLine()

	for {
		select {
		case event := <-l.controlEvents():
			l.applyControlEvent(event, state, false, nil)
			if state.StopRequested {
				return context.Canceled
			}
			if state.ExitConfirmPending {
				if err := l.waitForExitConfirmation(ctx, state); err != nil {
					return err
				}
			}
			if strings.TrimSpace(state.NextPrompt) != "" {
				return nil
			}
			if state.Paused {
				return l.waitWhilePaused(ctx, state)
			}
		case evt, ok := <-events:
			if !ok {
				events = nil
				if errs == nil {
					return nil
				}
				continue
			}
			if !l.shouldWakeForEvent(evt, state) {
				l.logf("wake event ignored type=%s autofeed=%t", evt.Type, state.AutofeedBeads)
				continue
			}
			l.logf("wake event type=%s task=%s from=%s", evt.Type, evt.TaskID, evt.FromAlias)
			if l.handleImmediateWakeEvent(ctx, evt, state) {
				return nil
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				if events == nil {
					return nil
				}
				continue
			}
			if err == nil || ctx.Err() != nil {
				return nil
			}
			l.logf("event stream wait failed: %v", err)
			l.println(fmt.Sprintf("info: event stream failed: %v", err))
			return l.idleWithControlsLabel(ctx, waitSeconds, state, "waiting for work")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (l *runLoop) shouldWakeForEvent(evt runWakeEvent, state *runState) bool {
	switch evt.Type {
	case runWakeEventConnected:
		return false
	case runWakeEventMailMessage, runWakeEventChatMessage:
		return true
	case runWakeEventWorkAvailable, runWakeEventClaimUpdate, runWakeEventClaimRemoved:
		return state != nil && state.AutofeedBeads
	case runWakeEventControlPause, runWakeEventControlResume, runWakeEventControlInterrupt:
		return true
	case runWakeEventError:
		return false
	default:
		return false
	}
}

func (l *runLoop) handleImmediateWakeEvent(ctx context.Context, evt runWakeEvent, state *runState) bool {
	switch evt.Type {
	case runWakeEventControlPause, runWakeEventControlInterrupt:
		state.Paused = true
		state.PauseAfterRun = false
		state.PauseNoticeShown = true
		l.println(runPausedNoticeText)
		_ = l.waitWhilePaused(ctx, state)
		return true
	case runWakeEventControlResume:
		state.Paused = false
		state.PauseNoticeShown = false
		return true
	default:
		return true
	}
}

func (l *runLoop) waitWhilePaused(ctx context.Context, state *runState) error {
	if state.ExitConfirmPending {
		return l.waitForExitConfirmation(ctx, state)
	}
	l.setStatusLine(runPausedStatusText)
	defer l.clearStatusLine()

	for {
		if state.StopRequested {
			return context.Canceled
		}
		if strings.TrimSpace(state.NextPrompt) != "" {
			state.Paused = false
			return nil
		}
		if !state.Paused {
			return nil
		}

		select {
		case event := <-l.controlEvents():
			l.applyControlEvent(event, state, false, nil)
			if state.StopRequested {
				return context.Canceled
			}
			if state.ExitConfirmPending {
				if err := l.waitForExitConfirmation(ctx, state); err != nil {
					return err
				}
				continue
			}
			if strings.TrimSpace(state.NextPrompt) != "" {
				state.Paused = false
				return nil
			}
			if !state.Paused {
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (l *runLoop) idleWithControls(ctx context.Context, seconds int, state *runState) error {
	return l.idleWithControlsLabel(ctx, seconds, state, "next run")
}

func (l *runLoop) idleWithControlsLabel(ctx context.Context, seconds int, state *runState, label string) error {
	if seconds <= 0 {
		return nil
	}
	defer l.clearStatusLine()

	for remaining := seconds; remaining > 0; remaining-- {
		l.renderIdleLine(label, remaining, state)

		select {
		case event := <-l.controlEvents():
			l.applyControlEvent(event, state, false, nil)
			if state.StopRequested {
				return context.Canceled
			}
			if state.ExitConfirmPending {
				if err := l.waitForExitConfirmation(ctx, state); err != nil {
					return err
				}
			}
			if strings.TrimSpace(state.NextPrompt) != "" {
				return nil
			}
			if state.Paused {
				return l.waitWhilePaused(ctx, state)
			}
			remaining++
		case <-ctx.Done():
			l.clearStatusLine()
			return ctx.Err()
		default:
			if err := l.sleep(ctx, time.Second); err != nil {
				l.clearStatusLine()
				return err
			}
		}
	}

	return nil
}

func (l *runLoop) controlEvents() <-chan runControlEvent {
	if l.control == nil {
		return nil
	}
	return l.control.Events()
}

func (l *runLoop) setExitConfirmation(active bool) {
	if l.screen != nil {
		l.screen.SetExitConfirmation(active)
	}
}

func (l *runLoop) offerExit(state *runState) {
	if state == nil {
		return
	}
	state.ExitConfirmPending = true
	l.setExitConfirmation(true)
	l.setStatusLine(runExitStatusText)
	if l.screen == nil {
		l.println(runExitStatusText)
	}
}

func (l *runLoop) cancelExitConfirmation(state *runState) {
	if state == nil || !state.ExitConfirmPending {
		return
	}
	state.ExitConfirmPending = false
	l.setExitConfirmation(false)
	if state.Paused {
		l.setStatusLine(runPausedStatusText)
		return
	}
	l.clearStatusLine()
}

func (l *runLoop) confirmExit(state *runState, activeRun bool, cancel context.CancelFunc) {
	if state == nil {
		return
	}
	state.PendingInput = false
	state.InputBuffer = ""
	state.StopRequested = true
	state.Paused = false
	state.PauseNoticeShown = false
	state.PauseAfterRun = false
	state.ExitConfirmPending = false
	l.setExitConfirmation(false)
	l.clearStatusLine()
	l.renderInputPrompt(state)
	if activeRun && cancel != nil {
		l.println("\nquitting.")
		cancel()
	}
}

func (l *runLoop) clearPendingInput(state *runState) {
	if state == nil {
		return
	}
	state.PendingInput = false
	state.InputBuffer = ""
	if l.screen != nil {
		l.screen.ClearInputLine()
		return
	}
	l.renderInputPrompt(state)
}

func (l *runLoop) waitForExitConfirmation(ctx context.Context, state *runState) error {
	if state == nil || !state.ExitConfirmPending {
		return nil
	}

	l.setStatusLine(runExitStatusText)
	for state.ExitConfirmPending && !state.StopRequested {
		select {
		case event := <-l.controlEvents():
			l.applyControlEvent(event, state, false, nil)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if state.StopRequested {
		return context.Canceled
	}
	return nil
}

func (l *runLoop) applyControlEvent(event runControlEvent, state *runState, activeRun bool, cancel context.CancelFunc) {
	l.logf("control event=%s active=%t text=%q", event.Type, activeRun, truncateRunText(strings.TrimSpace(event.Text), 120))

	switch event.Type {
	case runControlExitConfirm:
		l.confirmExit(state, activeRun, cancel)
		return
	case runControlExitCancel:
		l.cancelExitConfirmation(state)
		l.renderInputPrompt(state)
		return
	case runControlInterrupt:
		switch {
		case state.ExitConfirmPending:
			l.confirmExit(state, activeRun, cancel)
		case state.PendingInput || state.InputBuffer != "":
			l.clearPendingInput(state)
		case activeRun && cancel != nil:
			event = runControlEvent{Type: runControlStop}
		default:
			l.offerExit(state)
			l.renderInputPrompt(state)
			return
		}
	case runControlExitPrompt:
		if state.ExitConfirmPending {
			l.confirmExit(state, activeRun, cancel)
			return
		}
		l.offerExit(state)
		l.renderInputPrompt(state)
		return
	}

	if state.ExitConfirmPending {
		l.cancelExitConfirmation(state)
	}

	switch event.Type {
	case runControlTypingStarted:
		state.PendingInput = true
		if !activeRun {
			state.Paused = true
		}
		l.renderInputPrompt(state)
	case runControlBufferUpdated:
		state.InputBuffer = event.Text
		state.PendingInput = event.Text != ""
		if !activeRun && state.PendingInput {
			state.Paused = true
		}
		l.renderInputPrompt(state)
	case runControlPrompt:
		state.PendingInput = false
		state.InputBuffer = ""
		state.NextPrompt = strings.TrimSpace(event.Text)
		state.Paused = false
		state.PauseNoticeShown = false
		if state.AutofeedBeads {
			state.AutofeedBeads = false
			l.announceAutofeedState(false, "disabled for manual conversation. use /autofeed on to re-enable.")
		}
		if activeRun {
			l.printf("\nqueued prompt override: %s\n", truncateRunText(state.NextPrompt, 80))
		}
		l.renderInputPrompt(state)
	case runControlWait:
		state.PendingInput = false
		state.InputBuffer = ""
		state.PauseAfterRun = true
		state.Paused = !activeRun
		if activeRun {
			l.println("\nwill pause after this run.")
		} else {
			l.println(runPausedNoticeText)
			state.PauseNoticeShown = true
		}
	case runControlResume:
		state.PendingInput = false
		state.InputBuffer = ""
		state.Paused = false
		state.PauseNoticeShown = false
		if activeRun {
			state.PauseAfterRun = false
		}
		l.renderInputPrompt(state)
	case runControlAutofeedOn:
		state.AutofeedBeads = true
		l.announceAutofeedState(true, "on. claimed and ready beads can wake the agent.")
		l.renderInputPrompt(state)
	case runControlAutofeedOff:
		state.AutofeedBeads = false
		l.announceAutofeedState(false, "off. only comms can wake the agent.")
		l.renderInputPrompt(state)
	case runControlQuit:
		state.PendingInput = false
		state.InputBuffer = ""
		state.StopRequested = true
		state.Paused = false
		state.PauseNoticeShown = false
		state.PauseAfterRun = false
		if activeRun && cancel != nil {
			l.println("\nquitting.")
			cancel()
			return
		}
	case runControlStop:
		state.PendingInput = false
		state.InputBuffer = ""
		state.Paused = true
		state.PauseAfterRun = true
		if activeRun && cancel != nil {
			state.RunInterrupted = true
			l.println("\nstopped current run. " + runPausedNoticeText)
			state.PauseNoticeShown = true
			cancel()
			return
		}
		l.println(runPausedNoticeText)
		state.PauseNoticeShown = true
	}
}

func (l *runLoop) print(text string) {
	if l.screen != nil {
		l.screen.AppendText(text)
		return
	}
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	fmt.Fprint(l.out, text)
}

func (l *runLoop) printf(format string, args ...any) {
	if l.screen != nil {
		l.screen.AppendText(fmt.Sprintf(format, args...))
		return
	}
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	fmt.Fprintf(l.out, format, args...)
}

func (l *runLoop) println(text string) {
	if l.screen != nil {
		l.screen.AppendLine(text)
		return
	}
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	fmt.Fprintln(l.out, text)
}

func (l *runLoop) renderInputPrompt(state *runState) {
	if state == nil {
		return
	}
	if l.screen != nil && l.screen.hasActiveProgram() {
		return
	}
	if !state.PendingInput && !state.Paused && state.InputBuffer == "" {
		if l.screen != nil {
			l.screen.SetInputLine(l.promptLabel())
		}
		return
	}

	prompt := formatRunInputLine(l.promptLabel(), state.InputBuffer)
	if state.Paused && state.InputBuffer == "" {
		prompt = l.promptLabel()
	}

	if l.screen != nil {
		l.screen.SetInputLine(prompt)
		return
	}

	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	fmt.Fprintf(l.out, "\r\033[K%s", prompt)
}

func (l *runLoop) promptLabel() string {
	if strings.TrimSpace(l.inputPromptLabel) == "" {
		return defaultRunInputPromptLabel
	}
	return l.inputPromptLabel
}

func runIdentityPromptLabel(projectSlug string, canonicalOrigin string, repoOrigin string, alias string) string {
	projectSlug = strings.TrimSpace(projectSlug)
	shortRepo := runShortRepoName(canonicalOrigin, repoOrigin)
	alias = strings.TrimSpace(alias)
	if projectSlug == "" || alias == "" {
		return defaultRunInputPromptLabel
	}
	if shortRepo == "" {
		return projectSlug + ":" + alias + "> "
	}
	return projectSlug + ":" + shortRepo + ":" + alias + "> "
}

func runShortRepoName(canonicalOrigin string, repoOrigin string) string {
	for _, candidate := range []string{canonicalOrigin, repoOrigin} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		candidate = strings.TrimSuffix(candidate, ".git")
		candidate = strings.TrimSuffix(candidate, "/")
		candidate = strings.TrimSuffix(candidate, ":")
		candidate = strings.ReplaceAll(candidate, "\\", "/")
		if idx := strings.LastIndex(candidate, "/"); idx >= 0 && idx < len(candidate)-1 {
			return candidate[idx+1:]
		}
		if idx := strings.LastIndex(candidate, ":"); idx >= 0 && idx < len(candidate)-1 {
			return candidate[idx+1:]
		}
	}
	return ""
}

func (l *runLoop) renderIdleLine(label string, remaining int, state *runState) {
	line := fmt.Sprintf("%s in %ds", label, remaining)

	if l.screen != nil {
		l.screen.SetStatusLine(line)
		l.renderInputPrompt(state)
		return
	}

	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	if state != nil && strings.TrimSpace(state.InputBuffer) != "" {
		line = fmt.Sprintf("%s  >  %s", line, state.InputBuffer)
	}
	fmt.Fprintf(l.out, "\r\033[K%s", line)
}

func resolveRunMissionPrompt(basePrompt string, overridePrompt string) string {
	overridePrompt = strings.TrimSpace(overridePrompt)
	if overridePrompt != "" {
		return overridePrompt
	}
	return strings.TrimSpace(basePrompt)
}

func composeRunPrompt(missionPrompt string, cyclePrompt string) string {
	missionPrompt = strings.TrimSpace(missionPrompt)
	cyclePrompt = strings.TrimSpace(cyclePrompt)
	if missionPrompt == "" {
		return cyclePrompt
	}
	if cyclePrompt == "" {
		return missionPrompt
	}
	return fmt.Sprintf("Primary mission:\n%s\n\nCurrent cycle:\n%s", missionPrompt, cyclePrompt)
}

func composeRunPromptWithServices(missionPrompt string, cyclePrompt string, services []runServiceConfig) string {
	base := composeRunPrompt(missionPrompt, cyclePrompt)
	servicesSection := formatRunServicesPromptSection(services)
	if servicesSection == "" {
		return base
	}
	if strings.TrimSpace(base) == "" {
		return servicesSection
	}
	return fmt.Sprintf("%s\n\n%s", base, servicesSection)
}

func runDisplayPrompt(missionPrompt string, cyclePrompt string) string {
	cyclePrompt = strings.TrimSpace(cyclePrompt)
	if cyclePrompt != "" {
		return cyclePrompt
	}
	return strings.TrimSpace(missionPrompt)
}

func (l *runLoop) setStatusLine(text string) {
	if l.screen != nil {
		l.screen.SetStatusLine(text)
	}
}

func (l *runLoop) announceAutofeedState(enabled bool, detail string) {
	mode := "off"
	if enabled {
		mode = "on"
	}
	l.println("info: bead autofeed " + detail)
	l.setStatusLine("bead autofeed " + mode)
}

func (l *runLoop) clearStatusLine() {
	if l.screen != nil {
		l.screen.ClearStatusLine()
	}
}

func formatRunProviderMode(provider runProvider, opts runBuildOptions) string {
	name := "provider"
	if provider != nil && strings.TrimSpace(provider.Name()) != "" {
		name = provider.Name()
	}
	if strings.TrimSpace(opts.SessionID) != "" {
		return fmt.Sprintf("info: provider %s mode=resume session=%s", name, truncateRunText(opts.SessionID, 24))
	}
	if opts.ContinueSession {
		return fmt.Sprintf("info: provider %s mode=continue-last", name)
	}
	return fmt.Sprintf("info: provider %s mode=fresh", name)
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func realRunCommand(ctx context.Context, dir string, argv []string, onLine func(string), stderrSink io.Writer) error {
	if len(argv) == 0 {
		return fmt.Errorf("empty command")
	}

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	var stderr bytes.Buffer
	if stderrSink != nil {
		cmd.Stderr = io.MultiWriter(&stderr, stderrSink)
	} else {
		cmd.Stderr = &stderr
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		onLine(scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		_ = cmd.Wait()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		stderrText := strings.TrimSpace(stderr.String())
		if stderrText != "" {
			return fmt.Errorf("%w: %s", err, stderrText)
		}
		return err
	}
	return nil
}

func (l *runLoop) logf(format string, args ...any) {
	if l.logs != nil {
		l.logs.Logf(format, args...)
	}
}

func resolveRunDebugMode(flagSet bool, flagValue bool) bool {
	if flagSet {
		return flagValue
	}
	env := strings.TrimSpace(os.Getenv("BDH_RUN_DEBUG"))
	if env == "" {
		return false
	}
	switch strings.ToLower(env) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}
