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

type runCommandRunner func(ctx context.Context, dir string, argv []string, onLine func(string)) error
type runSleepFunc func(ctx context.Context, d time.Duration) error

type runLoop struct {
	provider         runProvider
	runner           runCommandRunner
	sleep            runSleepFunc
	now              func() time.Time
	out              io.Writer
	control          runInputController
	dispatch         runDispatcher
	defaults         runDispatchDefaults
	screen           *runScreenManager
	inputPromptLabel string
	writeMu          sync.Mutex
}

type runLoopOptions struct {
	InitialPrompt string
	Prompt        string
	WaitSeconds   int
	MaxRuns       int
	SessionMode   bool
	WorkingDir    string
	AllowedTools  string
	Model         string
}

type runCycleDecision struct {
	MissionPrompt string
	Prompt        string
	WaitSeconds   int
	Skip          bool
}

type runState struct {
	Run            int
	SessionID      string
	RanOnce        bool
	RunInterrupted bool
	PauseAfterRun  bool
	StopRequested  bool
	Paused         bool
	NextPrompt     string
	PendingInput   bool
	InputBuffer    string
	TextProbe      string
	SuppressText   bool
	StructuredOut  bool
}

var (
	runWaitSeconds  int
	runSessionMode  bool
	runMaxRuns      int
	runIdleWait     int
	runBasePrompt   string
	runWorkPrompt   string
	runCommsPrompt  string
	runWorkingDir   string
	runAllowedTools string
	runModel        string
	runProviderName string
	runIgnoreBeads  bool
	runInitConfig   bool
)

var runCmd = &cobra.Command{
	Use:   ":run [prompt]",
	Short: "Run an AI coding agent in a loop",
	Long: `Run an AI coding agent in a loop.

Current implementation includes:
  - repeated claude -p invocations
  - stream-json parsing and formatted output
  - session continuity when requested
  - /stop, /wait, /resume, /quit, and prompt override controls
  - bdh-driven dispatch between runs (chat, mail, claims, ready work)
  - adaptive wait behavior based on dispatch priority

Future provider work will add non-Claude backends on top of the same loop.`,
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
			IgnoreBeads:          runIgnoreBeads,
			WorkPromptSuffix:     settings.WorkPromptSuffix,
			CommsPromptSuffix:    settings.CommsPromptSuffix,
			HasWorkPromptSuffix:  true,
			HasCommsPromptSuffix: true,
		}

		var dispatcher runDispatcher
		inputPromptLabel := defaultRunInputPromptLabel
		cfg, cfgErr := loadAndValidateConfig()
		if cfgErr == nil {
			inputPromptLabel = runIdentityPromptLabel(cfg.ProjectSlug, cfg.CanonicalOrigin, cfg.RepoOrigin, cfg.Alias)
			if aw, awErr := newAwebClientRequired(cfg.BeadhubURL); awErr == nil {
				dispatcher = newBeadhubRunDispatcher(cfg, aw, dispatchDefaults)
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
			now:              time.Now,
			out:              cmd.OutOrStdout(),
			control:          screen,
			dispatch:         dispatcher,
			defaults:         dispatchDefaults,
			screen:           screen,
			inputPromptLabel: inputPromptLabel,
		}

		opts := runLoopOptions{
			InitialPrompt: strings.TrimSpace(strings.Join(args, " ")),
			Prompt:        settings.BasePrompt,
			WaitSeconds:   settings.WaitSeconds,
			MaxRuns:       runMaxRuns,
			SessionMode:   runSessionMode,
			WorkingDir:    runWorkingDir,
			AllowedTools:  runAllowedTools,
			Model:         runModel,
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
	runCmd.Flags().BoolVar(&runSessionMode, "session", false, "Resume the same provider session across runs")
	runCmd.Flags().IntVar(&runMaxRuns, "max-runs", 0, "Stop after N runs (0 means infinite)")
	runCmd.Flags().StringVar(&runWorkingDir, "dir", "", "Working directory for the agent process")
	runCmd.Flags().StringVar(&runAllowedTools, "allowed-tools", "", "Provider-specific allowed tools string")
	runCmd.Flags().StringVar(&runModel, "model", "", "Provider-specific model override")
	runCmd.Flags().StringVar(&runProviderName, "provider", "claude", "Agent provider to run")
	runCmd.Flags().BoolVar(&runIgnoreBeads, "ignore-beads", false, "Ignore claims and ready beads; only wake for incoming chat or unread mail")
	runCmd.Flags().BoolVar(&runInitConfig, "init", false, "Prompt for ~/.config/beadhub/run.json values and write them")
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

	state := &runState{}

	for {
		decision, err := l.nextPrompt(ctx, opts, state)
		if err != nil {
			return err
		}
		if decision.Skip {
			if err := l.waitForWork(ctx, decision.WaitSeconds, state); err != nil {
				if state.StopRequested && errors.Is(err, context.Canceled) {
					return nil
				}
				return err
			}
			continue
		}

		baseMissionPrompt := strings.TrimSpace(opts.Prompt)
		if state.Run == 0 && strings.TrimSpace(opts.InitialPrompt) != "" {
			baseMissionPrompt = strings.TrimSpace(opts.InitialPrompt)
		}
		missionPrompt := resolveRunMissionPrompt(baseMissionPrompt, decision.MissionPrompt)
		prompt := composeRunPrompt(missionPrompt, decision.Prompt)
		displayPrompt := runDisplayPrompt(missionPrompt, decision.Prompt)
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
	if l.dispatch != nil {
		decision, err := l.dispatch.Next(ctx)
		if err != nil {
			l.printf("info: dispatch failed: %v\n", err)
			if queuedMissionPrompt != "" {
				l.println("info: using queued prompt while dispatch recovers.")
				return runCycleDecision{
					MissionPrompt: queuedMissionPrompt,
					WaitSeconds:   opts.WaitSeconds,
				}, nil
			}
			defaults := withRunDispatchDefaults(l.defaults)
			l.println("info: waiting for dispatch recovery before starting a run.")
			return runCycleDecision{WaitSeconds: defaults.IdleWaitSeconds, Skip: true}, nil
		}
		cycle := runCycleDecision{
			MissionPrompt: queuedMissionPrompt,
			Prompt:        decision.Prompt,
			WaitSeconds:   decision.WaitSeconds,
			Skip:          decision.Skip,
		}
		if queuedMissionPrompt != "" && cycle.Skip {
			cycle.Skip = false
			cycle.WaitSeconds = opts.WaitSeconds
		}
		return cycle, nil
	}
	return runCycleDecision{
		MissionPrompt: queuedMissionPrompt,
		Prompt:        "",
		WaitSeconds:   opts.WaitSeconds,
	}, nil
}

func (l *runLoop) runOnce(ctx context.Context, opts runLoopOptions, state *runState, prompt string, displayPrompt string) error {
	buildOpts := runBuildOptions{
		AllowedTools: opts.AllowedTools,
		Model:        opts.Model,
	}
	if opts.SessionMode {
		if state.SessionID != "" && l.provider.Capabilities().SupportsResume {
			buildOpts.SessionID = state.SessionID
		} else if state.RanOnce && l.provider.Capabilities().SupportsContinue {
			buildOpts.ContinueSession = true
		}
	}

	argv, err := l.provider.BuildCommand(prompt, buildOpts)
	if err != nil {
		return err
	}

	l.printf("\nrun #%d  %s  >  %s\n\n", state.Run, l.now().Format("15:04:05"), truncateRunText(displayPrompt, 80))
	l.println("type /wait, /stop, /quit, or start typing to queue a prompt.")
	l.clearStatusLine()
	l.renderInputPrompt(state)

	presenter := &runPresenterState{}
	state.TextProbe = ""
	state.SuppressText = false
	state.StructuredOut = false
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- l.runner(runCtx, opts.WorkingDir, argv, func(line string) {
			l.handleOutputLine(line, presenter, state)
		})
	}()

	for {
		select {
		case err := <-errCh:
			l.drainPendingControlEvents(state)
			state.RanOnce = true
			if state.RunInterrupted {
				state.Paused = true
				state.PauseAfterRun = true
				state.RunInterrupted = false
				return nil
			}
			state.RunInterrupted = false
			return err
		case event := <-l.controlEvents():
			l.applyControlEvent(event, state, true, cancel)
		case <-ctx.Done():
			cancel()
			state.StopRequested = true
			return ctx.Err()
		}
	}
}

func (l *runLoop) drainPendingControlEvents(state *runState) {
	for {
		select {
		case event := <-l.controlEvents():
			l.applyControlEvent(event, state, false, nil)
		default:
			return
		}
	}
}

func (l *runLoop) handleOutputLine(line string, presenter *runPresenterState, state *runState) {
	event, err := l.provider.ParseOutput(line)
	if err != nil {
		if presenter.lastWasText {
			l.println("")
			presenter.lastWasText = false
		}
		l.println(line)
		return
	}

	if sid := l.provider.SessionID(event); sid != "" {
		state.SessionID = sid
	}

	switch event.Type {
	case runEventText:
		if l.shouldSuppressText(state, event.Text) {
			return
		}
		l.print(event.Text)
		presenter.lastWasText = true
	case runEventToolCall:
		state.StructuredOut = true
		if presenter.lastWasText {
			l.println("")
			presenter.lastWasText = false
		}
		for _, call := range event.ToolCalls {
			if input := formatRunToolInput(call.Input); input != "" {
				l.printf("tool: %s  %s\n", call.Name, input)
				continue
			}
			l.printf("tool: %s\n", call.Name)
		}
	case runEventToolResult:
		state.StructuredOut = true
		if presenter.lastWasText {
			l.println("")
			presenter.lastWasText = false
		}
		if text := strings.TrimSpace(event.Text); text != "" {
			l.printf("  -> %s\n", truncateRunText(text, 150))
		}
	case runEventDone:
		state.StructuredOut = true
		if presenter.lastWasText {
			l.println("")
			presenter.lastWasText = false
		}
		l.printf("%s\n", formatRunDone(event))
	case runEventSystem:
		state.StructuredOut = true
		if presenter.lastWasText {
			l.println("")
			presenter.lastWasText = false
		}
		if text := strings.TrimSpace(event.Text); text != "" {
			l.printf("info: %s\n", text)
		}
	}
	l.renderInputPrompt(state)
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
		if !state.PendingInput {
			l.println("paused. use /resume, /quit, or type a prompt to continue.")
		}
		return l.waitWhilePaused(ctx, state)
	}

	return l.idleWithControls(ctx, waitSeconds, state)
}

func (l *runLoop) waitForWork(ctx context.Context, waitSeconds int, state *runState) error {
	return l.idleWithControlsLabel(ctx, waitSeconds, state, "waiting for work")
}

func (l *runLoop) waitWhilePaused(ctx context.Context, state *runState) error {
	l.setStatusLine("paused: /resume, /quit, or type a prompt")
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
			l.clearStatusLine()
			if state.StopRequested {
				return context.Canceled
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

func (l *runLoop) applyControlEvent(event runControlEvent, state *runState, activeRun bool, cancel context.CancelFunc) {
	switch event.Type {
	case runControlTypingStarted:
		state.PendingInput = true
		l.renderInputPrompt(state)
	case runControlBufferUpdated:
		state.InputBuffer = event.Text
		state.PendingInput = event.Text != ""
		l.renderInputPrompt(state)
	case runControlPrompt:
		state.PendingInput = false
		state.InputBuffer = ""
		state.NextPrompt = strings.TrimSpace(event.Text)
		state.Paused = false
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
			l.println("paused. use /resume, /quit, or type a prompt to continue.")
		}
	case runControlResume:
		state.PendingInput = false
		state.InputBuffer = ""
		state.Paused = false
		if activeRun {
			state.PauseAfterRun = false
		}
		l.renderInputPrompt(state)
	case runControlQuit:
		state.PendingInput = false
		state.InputBuffer = ""
		state.StopRequested = true
		state.Paused = false
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
			l.println("\nstopped current run. paused. use /resume, /quit, or type a prompt to continue.")
			cancel()
			return
		}
		l.println("paused. use /resume, /quit, or type a prompt to continue.")
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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

func (l *runLoop) clearStatusLine() {
	if l.screen != nil {
		l.screen.ClearStatusLine()
	}
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

func realRunCommand(ctx context.Context, dir string, argv []string, onLine func(string)) error {
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
	cmd.Stderr = &stderr

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
