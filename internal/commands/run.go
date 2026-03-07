package commands

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"time"

	aweb "github.com/awebai/aw"
	"github.com/awebai/aw/chat"
	"github.com/beadhub/bdh/internal/bd"
	"github.com/beadhub/bdh/internal/beads"
	"github.com/beadhub/bdh/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type runProviderCapabilities struct {
	SupportsResume   bool
	SupportsContinue bool
}

type runBuildOptions struct {
	SessionID       string
	ContinueSession bool
	AllowedTools    string
	Model           string
}

type runProvider interface {
	Name() string
	BuildCommand(prompt string, opts runBuildOptions) ([]string, error)
	ParseOutput(line string) (*runEvent, error)
	SessionID(event *runEvent) string
	Capabilities() runProviderCapabilities
}

type runEventType string

const (
	runEventText       runEventType = "text"
	runEventToolCall   runEventType = "tool_call"
	runEventToolResult runEventType = "tool_result"
	runEventDone       runEventType = "done"
	runEventSystem     runEventType = "system"
)

type runToolCall struct {
	Name  string
	Input map[string]any
}

type runEvent struct {
	Type       runEventType
	Text       string
	ToolCalls  []runToolCall
	DurationMS int
	CostUSD    *float64
	Session    string
}

type runCommandRunner func(ctx context.Context, dir string, argv []string, onLine func(string)) error
type runSleepFunc func(ctx context.Context, d time.Duration) error

type runControlEventType string

const (
	runControlTypingStarted runControlEventType = "typing_started"
	runControlPrompt        runControlEventType = "prompt"
	runControlStop          runControlEventType = "stop"
	runControlWait          runControlEventType = "wait"
	runControlResume        runControlEventType = "resume"
)

type runControlEvent struct {
	Type runControlEventType
	Text string
}

type runInputController interface {
	Start() error
	Stop() error
	Events() <-chan runControlEvent
	HasPendingInput() bool
}

type runDispatcher interface {
	Next(ctx context.Context) (runDispatchDecision, error)
}

type runDispatchDecision struct {
	Prompt      string
	WaitSeconds int
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

type runLoop struct {
	provider runProvider
	runner   runCommandRunner
	sleep    runSleepFunc
	now      func() time.Time
	out      io.Writer
	control  runInputController
	dispatch runDispatcher
	writeMu  sync.Mutex
}

type runLoopOptions struct {
	Prompt       string
	WaitSeconds  int
	MaxRuns      int
	SessionMode  bool
	WorkingDir   string
	AllowedTools string
	Model        string
}

type runState struct {
	Run           int
	SessionID     string
	RanOnce       bool
	PauseAfterRun bool
	StopRequested bool
	Paused        bool
	NextPrompt    string
	PendingInput  bool
}

type runPresenterState struct {
	lastWasText bool
}

type claudeProvider struct{}

type claudeEnvelope struct {
	Type       string          `json:"type"`
	Event      claudeEvent     `json:"event"`
	Message    json.RawMessage `json:"message"`
	Content    any             `json:"content"`
	DurationMS int             `json:"duration_ms"`
	Stats      struct {
		DurationMS int `json:"duration_ms"`
	} `json:"stats"`
	CostUSD *float64 `json:"cost_usd"`
	Session string   `json:"session_id"`
}

type claudeEvent struct {
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
}

type terminalRunInput struct {
	file    *os.File
	events  chan runControlEvent
	stopCh  chan struct{}
	mu      sync.Mutex
	pending bool
	started bool
	state   *term.State
}

type beadhubRunDispatcher struct {
	cfg        *config.Config
	aw         *aweb.Client
	readyTasks func(ctx context.Context) ([]runReadyTask, error)
}

var (
	runWaitSeconds  int
	runSessionMode  bool
	runMaxRuns      int
	runWorkingDir   string
	runAllowedTools string
	runModel        string
	runProviderName string
)

var runCmd = &cobra.Command{
	Use:   ":run [prompt]",
	Short: "Run an AI coding agent in a loop",
	Long: `Run an AI coding agent in a loop.

Current implementation includes:
  - repeated claude -p invocations
  - stream-json parsing and formatted output
  - session continuity when requested
  - /stop, /wait, /resume, and prompt override controls
  - bdh-driven dispatch between runs (chat, mail, claims, ready work, idle)
  - adaptive wait behavior based on dispatch priority

Future provider work will add non-Claude backends on top of the same loop.`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if runWaitSeconds < 0 {
			return fmt.Errorf("--wait must be >= 0")
		}
		if runMaxRuns < 0 {
			return fmt.Errorf("--max-runs must be >= 0")
		}

		provider, err := newRunProvider(runProviderName)
		if err != nil {
			return err
		}

		var dispatcher runDispatcher
		cfg, cfgErr := loadAndValidateConfig()
		if cfgErr == nil {
			if aw, awErr := newAwebClientRequired(cfg.BeadhubURL); awErr == nil {
				dispatcher = newBeadhubRunDispatcher(cfg, aw)
			}
		}

		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
		defer stop()

		loop := &runLoop{
			provider: provider,
			runner:   realRunCommand,
			sleep:    sleepWithContext,
			now:      time.Now,
			out:      cmd.OutOrStdout(),
			control:  newTerminalRunInput(cmd.InOrStdin()),
			dispatch: dispatcher,
		}

		opts := runLoopOptions{
			Prompt:       strings.Join(args, " "),
			WaitSeconds:  runWaitSeconds,
			MaxRuns:      runMaxRuns,
			SessionMode:  runSessionMode,
			WorkingDir:   runWorkingDir,
			AllowedTools: runAllowedTools,
			Model:        runModel,
		}

		err = loop.Run(ctx, opts)
		if err == nil || err == context.Canceled {
			return nil
		}
		return err
	},
}

func init() {
	runCmd.Flags().IntVar(&runWaitSeconds, "wait", 20, "Idle seconds between runs")
	runCmd.Flags().BoolVar(&runSessionMode, "session", false, "Resume the same provider session across runs")
	runCmd.Flags().IntVar(&runMaxRuns, "max-runs", 0, "Stop after N runs (0 means infinite)")
	runCmd.Flags().StringVar(&runWorkingDir, "dir", "", "Working directory for the agent process")
	runCmd.Flags().StringVar(&runAllowedTools, "allowed-tools", "", "Provider-specific allowed tools string")
	runCmd.Flags().StringVar(&runModel, "model", "", "Provider-specific model override")
	runCmd.Flags().StringVar(&runProviderName, "provider", "claude", "Agent provider to run")
}

func newRunProvider(name string) (runProvider, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "claude":
		return claudeProvider{}, nil
	default:
		return nil, fmt.Errorf("unsupported provider %q", name)
	}
}

func newTerminalRunInput(in io.Reader) runInputController {
	file, ok := in.(*os.File)
	if !ok {
		return nil
	}
	if !term.IsTerminal(int(file.Fd())) {
		return nil
	}
	return &terminalRunInput{
		file:   file,
		events: make(chan runControlEvent, 32),
		stopCh: make(chan struct{}),
	}
}

func newBeadhubRunDispatcher(cfg *config.Config, aw *aweb.Client) runDispatcher {
	dispatcher := &beadhubRunDispatcher{
		cfg: cfg,
		aw:  aw,
	}
	dispatcher.readyTasks = dispatcher.fetchReadyTasks
	return dispatcher
}

func (t *terminalRunInput) Start() error {
	if t == nil {
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.started {
		return nil
	}

	state, err := term.MakeRaw(int(t.file.Fd()))
	if err != nil {
		return err
	}
	t.state = state
	t.started = true
	go t.readLoop()
	return nil
}

func (t *terminalRunInput) Stop() error {
	if t == nil {
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.started {
		return nil
	}

	select {
	case <-t.stopCh:
	default:
		close(t.stopCh)
	}

	t.started = false
	t.pending = false
	if t.state != nil {
		err := term.Restore(int(t.file.Fd()), t.state)
		t.state = nil
		return err
	}
	return nil
}

func (t *terminalRunInput) Events() <-chan runControlEvent {
	if t == nil {
		return nil
	}
	return t.events
}

func (t *terminalRunInput) HasPendingInput() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.pending
}

func (t *terminalRunInput) readLoop() {
	buffer := make([]byte, 0, 256)
	byteBuf := make([]byte, 1)

	for {
		select {
		case <-t.stopCh:
			return
		default:
		}

		n, err := t.file.Read(byteBuf)
		if err != nil || n == 0 {
			return
		}

		b := byteBuf[0]
		switch b {
		case 3:
			t.emit(runControlEvent{Type: runControlStop})
		case '\r', '\n':
			text := strings.TrimSpace(string(buffer))
			buffer = buffer[:0]
			t.setPending(false)
			if text == "" {
				continue
			}
			switch text {
			case "/stop":
				t.emit(runControlEvent{Type: runControlStop})
			case "/wait":
				t.emit(runControlEvent{Type: runControlWait})
			case "/resume":
				t.emit(runControlEvent{Type: runControlResume})
			default:
				t.emit(runControlEvent{Type: runControlPrompt, Text: text})
			}
		case 8, 127:
			if len(buffer) > 0 {
				buffer = buffer[:len(buffer)-1]
			}
			t.setPending(len(buffer) > 0)
		default:
			if b < 32 || b == 255 {
				continue
			}
			if len(buffer) == 0 {
				t.setPending(true)
				t.emit(runControlEvent{Type: runControlTypingStarted})
			}
			buffer = append(buffer, b)
		}
	}
}

func (t *terminalRunInput) emit(event runControlEvent) {
	select {
	case t.events <- event:
	default:
	}
}

func (t *terminalRunInput) setPending(pending bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pending = pending
}

func (d *beadhubRunDispatcher) Next(ctx context.Context) (runDispatchDecision, error) {
	summary, err := d.summary(ctx)
	if err != nil {
		return runDispatchDecision{}, err
	}
	return selectRunDispatch(summary), nil
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

func selectRunDispatch(summary runDispatchSummary) runDispatchDecision {
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
			Prompt:      "Nothing is pending right now. Check in with your coordinator, stay available, and be ready to respond quickly if chat or mail arrives.",
			WaitSeconds: 60,
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

func (l *runLoop) Run(ctx context.Context, opts runLoopOptions) error {
	if strings.TrimSpace(opts.Prompt) == "" && l.dispatch == nil {
		return fmt.Errorf("prompt cannot be empty")
	}

	if l.control != nil {
		if err := l.control.Start(); err != nil {
			return err
		}
		defer func() { _ = l.control.Stop() }()
	}

	state := &runState{}

	for {
		prompt, waitSeconds, err := l.nextPrompt(ctx, opts, state)
		if err != nil {
			return err
		}

		state.Run++
		if err := l.runOnce(ctx, opts, state, prompt); err != nil {
			if state.StopRequested && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
				return nil
			}
			return err
		}

		if opts.MaxRuns > 0 && state.Run >= opts.MaxRuns {
			l.printf("\ndone: reached max-runs (%d)\n", opts.MaxRuns)
			return nil
		}

		if err := l.waitForNextCycle(ctx, waitSeconds, state); err != nil {
			if state.StopRequested && errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
	}
}

func (l *runLoop) nextPrompt(ctx context.Context, opts runLoopOptions, state *runState) (string, int, error) {
	if strings.TrimSpace(state.NextPrompt) != "" {
		prompt := state.NextPrompt
		state.NextPrompt = ""
		return prompt, opts.WaitSeconds, nil
	}
	if !state.RanOnce && strings.TrimSpace(opts.Prompt) != "" {
		return opts.Prompt, opts.WaitSeconds, nil
	}
	if l.dispatch != nil {
		decision, err := l.dispatch.Next(ctx)
		if err != nil {
			l.printf("info: dispatch failed: %v\n", err)
			if strings.TrimSpace(opts.Prompt) != "" {
				l.println("info: falling back to the explicit prompt.")
				return opts.Prompt, opts.WaitSeconds, nil
			}
			fallback := selectRunDispatch(runDispatchSummary{})
			l.println("info: falling back to idle prompt until dispatch recovers.")
			return fallback.Prompt, fallback.WaitSeconds, nil
		}
		return decision.Prompt, decision.WaitSeconds, nil
	}
	return opts.Prompt, opts.WaitSeconds, nil
}

func (l *runLoop) runOnce(ctx context.Context, opts runLoopOptions, state *runState, prompt string) error {
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

	l.printf("\nrun #%d  %s  >  %s\n\n", state.Run, l.now().Format("15:04:05"), truncateRunText(prompt, 80))

	presenter := &runPresenterState{}
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
			state.RanOnce = true
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
		l.print(event.Text)
		presenter.lastWasText = true
	case runEventToolCall:
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
		if presenter.lastWasText {
			l.println("")
			presenter.lastWasText = false
		}
		if text := strings.TrimSpace(event.Text); text != "" {
			l.printf("  -> %s\n", truncateRunText(text, 150))
		}
	case runEventDone:
		if presenter.lastWasText {
			l.println("")
			presenter.lastWasText = false
		}
		l.printf("%s\n", formatRunDone(event))
	case runEventSystem:
		if presenter.lastWasText {
			l.println("")
			presenter.lastWasText = false
		}
		if text := strings.TrimSpace(event.Text); text != "" {
			l.printf("info: %s\n", text)
		}
	}
}

func (l *runLoop) idle(ctx context.Context, seconds int) error {
	if seconds <= 0 {
		return nil
	}

	for remaining := seconds; remaining > 0; remaining-- {
		l.printf("\rnext run in %ds", remaining)
		if err := l.sleep(ctx, time.Second); err != nil {
			l.print("\r                \r")
			return err
		}
	}
	l.print("\r                \r")
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

	if state.PauseAfterRun || state.Paused || l.control.HasPendingInput() {
		state.Paused = true
		state.PauseAfterRun = false
		if !state.PendingInput {
			l.println("paused. use /resume or type a prompt to continue.")
		}
		return l.waitWhilePaused(ctx, state)
	}

	return l.idleWithControls(ctx, waitSeconds, state)
}

func (l *runLoop) waitWhilePaused(ctx context.Context, state *runState) error {
	for {
		if state.StopRequested {
			return context.Canceled
		}
		if strings.TrimSpace(state.NextPrompt) != "" {
			state.Paused = false
			return nil
		}
		if !state.Paused && !state.PendingInput {
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
			if !state.Paused && !state.PendingInput {
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (l *runLoop) idleWithControls(ctx context.Context, seconds int, state *runState) error {
	if seconds <= 0 {
		return nil
	}

	for remaining := seconds; remaining > 0; remaining-- {
		l.printf("\rnext run in %ds", remaining)

		select {
		case event := <-l.controlEvents():
			l.applyControlEvent(event, state, false, nil)
			l.print("\r                \r")
			if state.StopRequested {
				return context.Canceled
			}
			if strings.TrimSpace(state.NextPrompt) != "" {
				return nil
			}
			if state.Paused || state.PendingInput {
				return l.waitWhilePaused(ctx, state)
			}
			remaining++
		case <-ctx.Done():
			l.print("\r                \r")
			return ctx.Err()
		default:
			if err := l.sleep(ctx, time.Second); err != nil {
				l.print("\r                \r")
				return err
			}
		}
	}

	l.print("\r                \r")
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
		if activeRun && !state.PauseAfterRun {
			state.PauseAfterRun = true
			l.println("\ninput pending: auto-dispatch will pause after this run.")
		}
	case runControlPrompt:
		state.PendingInput = false
		state.NextPrompt = strings.TrimSpace(event.Text)
		state.Paused = false
		if activeRun {
			state.PauseAfterRun = true
			l.printf("\nqueued prompt override: %s\n", truncateRunText(state.NextPrompt, 80))
		}
	case runControlWait:
		state.PendingInput = false
		state.PauseAfterRun = true
		state.Paused = !activeRun
		if activeRun {
			l.println("\nwill pause after this run.")
		} else {
			l.println("paused. use /resume or type a prompt to continue.")
		}
	case runControlResume:
		state.PendingInput = false
		state.Paused = false
		if activeRun {
			state.PauseAfterRun = false
		}
	case runControlStop:
		state.PendingInput = false
		state.StopRequested = true
		if cancel != nil {
			cancel()
		}
	}
}

func (l *runLoop) print(text string) {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	fmt.Fprint(l.out, text)
}

func (l *runLoop) printf(format string, args ...any) {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	fmt.Fprintf(l.out, format, args...)
}

func (l *runLoop) println(text string) {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	fmt.Fprintln(l.out, text)
}

func formatRunDone(event *runEvent) string {
	parts := []string{"done"}
	duration := event.DurationMS
	if duration == 0 {
		duration = event.DurationMS
	}
	if duration > 0 {
		parts = append(parts, fmt.Sprintf("%.1fs", float64(duration)/1000.0))
	}
	if event.CostUSD != nil {
		parts = append(parts, fmt.Sprintf("$%.4f", *event.CostUSD))
	}
	return strings.Join(parts, "  ")
}

func formatRunToolInput(data map[string]any) string {
	if len(data) == 0 {
		return ""
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
			parts = append(parts, fmt.Sprintf("%s=%q", key, truncateRunText(typed, 60)))
		default:
			parts = append(parts, fmt.Sprintf("%s=%s", key, truncateRunText(fmt.Sprintf("%v", typed), 60)))
		}
	}
	return strings.Join(parts, "  ")
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
		return err
	}

	if err := cmd.Wait(); err != nil {
		stderrText := strings.TrimSpace(stderr.String())
		if stderrText != "" {
			return fmt.Errorf("%w: %s", err, stderrText)
		}
		return err
	}
	return nil
}

func (claudeProvider) Name() string {
	return "claude"
}

func (claudeProvider) BuildCommand(prompt string, opts runBuildOptions) ([]string, error) {
	if strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("prompt cannot be empty")
	}

	command := []string{
		"claude",
		"-p",
		prompt,
		"--output-format",
		"stream-json",
		"--verbose",
		"--include-partial-messages",
	}

	if opts.SessionID != "" {
		command = append(command, "--resume", opts.SessionID)
	} else if opts.ContinueSession {
		command = append(command, "--continue")
	}
	if strings.TrimSpace(opts.AllowedTools) != "" {
		command = append(command, "--allowedTools", opts.AllowedTools)
	}
	if strings.TrimSpace(opts.Model) != "" {
		command = append(command, "--model", opts.Model)
	}

	return command, nil
}

func (claudeProvider) ParseOutput(line string) (*runEvent, error) {
	var envelope claudeEnvelope
	if err := json.Unmarshal([]byte(line), &envelope); err != nil {
		return nil, err
	}

	switch envelope.Type {
	case "stream_event":
		if envelope.Event.Delta.Type == "text_delta" {
			return &runEvent{Type: runEventText, Text: envelope.Event.Delta.Text}, nil
		}
	case "assistant":
		var message struct {
			Content []struct {
				Type  string         `json:"type"`
				Name  string         `json:"name"`
				Input map[string]any `json:"input"`
			} `json:"content"`
		}
		if err := json.Unmarshal(envelope.Message, &message); err != nil {
			return nil, err
		}
		calls := make([]runToolCall, 0, len(message.Content))
		for _, block := range message.Content {
			if block.Type != "tool_use" {
				continue
			}
			calls = append(calls, runToolCall{Name: block.Name, Input: block.Input})
		}
		if len(calls) > 0 {
			return &runEvent{Type: runEventToolCall, ToolCalls: calls}, nil
		}
	case "tool_result":
		return &runEvent{Type: runEventToolResult, Text: claudeToolResultText(envelope.Content)}, nil
	case "result":
		duration := envelope.DurationMS
		if duration == 0 {
			duration = envelope.Stats.DurationMS
		}
		return &runEvent{
			Type:       runEventDone,
			DurationMS: duration,
			CostUSD:    envelope.CostUSD,
			Session:    envelope.Session,
		}, nil
	case "system":
		var text string
		if err := json.Unmarshal(envelope.Message, &text); err != nil {
			return nil, err
		}
		return &runEvent{Type: runEventSystem, Text: text}, nil
	}

	return &runEvent{}, nil
}

func (claudeProvider) SessionID(event *runEvent) string {
	if event == nil {
		return ""
	}
	return event.Session
}

func (claudeProvider) Capabilities() runProviderCapabilities {
	return runProviderCapabilities{
		SupportsResume:   true,
		SupportsContinue: true,
	}
}

func claudeToolResultText(content any) string {
	switch typed := content.(type) {
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}
			blockType, _ := block["type"].(string)
			if blockType != "text" {
				continue
			}
			text, _ := block["text"].(string)
			if strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, " ")
	default:
		return fmt.Sprintf("%v", content)
	}
}
