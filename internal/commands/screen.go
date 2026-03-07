package commands

import (
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

type runScreenManager struct {
	inputFile   *os.File
	outputFile  *os.File
	promptLabel string

	mu         sync.Mutex
	lines      []string
	current    string
	statusLine string
	inputLine  string
	pending    bool
	active     bool

	events  chan runControlEvent
	program *tea.Program
	doneCh  chan error
}

type runScreenSnapshot struct {
	Lines       []string
	Current     string
	StatusLine  string
	InputLine   string
	PromptLabel string
}

type runScreenAppendTextMsg string
type runScreenSetStatusMsg string
type runScreenSetInputMsg string
type runScreenQuitMsg struct{}

type runScreenModel struct {
	viewport    viewport.Model
	input       textinput.Model
	width       int
	height      int
	promptLabel string

	lines      []string
	current    string
	statusLine string
	styles     runScreenStyles

	onInputChanged func(string)
	onSubmitted    func(string)
	onStop         func()
}

type runScreenStyles struct {
	runHeader lipgloss.Style
	tool      lipgloss.Style
	result    lipgloss.Style
	done      lipgloss.Style
	info      lipgloss.Style
	status    lipgloss.Style
	hint      lipgloss.Style
}

func newRunScreenManager(in io.Reader, out io.Writer) *runScreenManager {
	inputFile, ok := in.(*os.File)
	if !ok || !term.IsTerminal(int(inputFile.Fd())) {
		return nil
	}

	outputFile, ok := out.(*os.File)
	if !ok || !term.IsTerminal(int(outputFile.Fd())) {
		return nil
	}

	return &runScreenManager{
		inputFile:   inputFile,
		outputFile:  outputFile,
		promptLabel: defaultRunInputPromptLabel,
		events:      make(chan runControlEvent, 64),
		inputLine:   defaultRunInputPromptLabel,
	}
}

func (s *runScreenManager) Start() error {
	if s == nil {
		return nil
	}

	s.mu.Lock()
	if s.active {
		s.mu.Unlock()
		return nil
	}
	s.active = true
	snapshot := s.snapshotLocked()
	doneCh := make(chan error, 1)
	model := newRunScreenModel(snapshot, s.handleInputChanged, s.handleInputSubmitted, s.handleStopRequested)
	program := tea.NewProgram(
		model,
		tea.WithInput(s.inputFile),
		tea.WithOutput(s.outputFile),
		tea.WithAltScreen(),
	)
	s.program = program
	s.doneCh = doneCh
	s.mu.Unlock()

	go func() {
		_, err := program.Run()
		doneCh <- err
	}()

	return nil
}

func (s *runScreenManager) Stop() error {
	if s == nil {
		return nil
	}

	s.mu.Lock()
	if !s.active {
		s.mu.Unlock()
		return nil
	}
	s.active = false
	program := s.program
	doneCh := s.doneCh
	s.program = nil
	s.doneCh = nil
	s.pending = false
	s.mu.Unlock()

	if program != nil {
		program.Send(runScreenQuitMsg{})
	}
	if doneCh == nil {
		return nil
	}

	select {
	case err := <-doneCh:
		return err
	case <-time.After(2 * time.Second):
		return nil
	}
}

func (s *runScreenManager) Events() <-chan runControlEvent {
	if s == nil {
		return nil
	}
	return s.events
}

func (s *runScreenManager) HasPendingInput() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pending
}

func (s *runScreenManager) AppendText(text string) {
	if s == nil {
		return
	}

	s.mu.Lock()
	appendRunScreenText(&s.lines, &s.current, text)
	program := s.program
	s.mu.Unlock()

	if program != nil {
		program.Send(runScreenAppendTextMsg(text))
	}
}

func (s *runScreenManager) AppendLine(line string) {
	s.AppendText(line + "\n")
}

func (s *runScreenManager) SetInputLine(line string) {
	if s == nil {
		return
	}

	value := runInputValueFromLine(line, s.promptLabel)

	s.mu.Lock()
	s.pending = value != ""
	s.inputLine = formatRunInputLine(s.promptLabel, value)
	program := s.program
	s.mu.Unlock()

	if program != nil {
		program.Send(runScreenSetInputMsg(value))
	}
}

func (s *runScreenManager) SetStatusLine(line string) {
	if s == nil {
		return
	}

	s.mu.Lock()
	s.statusLine = line
	program := s.program
	s.mu.Unlock()

	if program != nil {
		program.Send(runScreenSetStatusMsg(line))
	}
}

func (s *runScreenManager) ClearStatusLine() {
	s.SetStatusLine("")
}

func (s *runScreenManager) ClearInputLine() {
	s.SetInputLine(s.promptLabel)
}

func (s *runScreenManager) hasActiveProgram() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.program != nil
}

func (s *runScreenManager) snapshotLocked() runScreenSnapshot {
	lines := make([]string, len(s.lines))
	copy(lines, s.lines)
	return runScreenSnapshot{
		Lines:       lines,
		Current:     s.current,
		StatusLine:  s.statusLine,
		InputLine:   s.inputLine,
		PromptLabel: s.promptLabel,
	}
}

func (s *runScreenManager) emit(event runControlEvent) {
	select {
	case s.events <- event:
	default:
	}
}

func (s *runScreenManager) handleInputChanged(value string) {
	s.mu.Lock()
	wasPending := s.pending
	s.pending = value != ""
	s.inputLine = formatRunInputLine(s.promptLabel, value)
	s.mu.Unlock()

	if !wasPending && value != "" {
		s.emit(runControlEvent{Type: runControlTypingStarted})
	}
	s.emit(runControlEvent{Type: runControlBufferUpdated, Text: value})
}

func (s *runScreenManager) handleInputSubmitted(value string) {
	s.mu.Lock()
	s.pending = false
	s.inputLine = s.promptLabel
	s.mu.Unlock()

	s.emit(runControlEvent{Type: runControlBufferUpdated, Text: ""})
	if strings.TrimSpace(value) == "" {
		return
	}
	s.emit(parseRunControlSubmission(value))
}

func (s *runScreenManager) handleStopRequested() {
	s.emit(runControlEvent{Type: runControlStop})
}

func newRunScreenModel(
	snapshot runScreenSnapshot,
	onInputChanged func(string),
	onSubmitted func(string),
	onStop func(),
) runScreenModel {
	input := textinput.New()
	input.Prompt = snapshot.PromptLabel
	input.SetValue(runInputValueFromLine(snapshot.InputLine, snapshot.PromptLabel))
	input.Focus()
	input.CharLimit = 0

	model := runScreenModel{
		viewport:       viewport.New(0, 0),
		input:          input,
		promptLabel:    snapshot.PromptLabel,
		lines:          snapshot.Lines,
		current:        snapshot.Current,
		statusLine:     snapshot.StatusLine,
		styles:         newRunScreenStyles(),
		onInputChanged: onInputChanged,
		onSubmitted:    onSubmitted,
		onStop:         onStop,
	}
	model.syncViewport(true)
	return model
}

func newRunScreenStyles() runScreenStyles {
	return runScreenStyles{
		runHeader: lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "24", Dark: "12"}).Bold(true),
		tool:      lipgloss.NewStyle().Bold(true),
		result:    lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "23", Dark: "14"}),
		done:      lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "10"}).Bold(true),
		info:      lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "240", Dark: "8"}),
		status: lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "236", Dark: "252"}).
			Background(lipgloss.AdaptiveColor{Light: "252", Dark: "236"}).
			Padding(0, 1),
		hint: lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "240", Dark: "8"}),
	}
}

func (m runScreenModel) Init() tea.Cmd {
	return nil
}

func (m runScreenModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = typed.Width
		m.height = typed.Height
		m.syncLayout()
		return m, nil
	case runScreenAppendTextMsg:
		appendRunScreenText(&m.lines, &m.current, string(typed))
		m.syncViewport(true)
		return m, nil
	case runScreenSetStatusMsg:
		m.statusLine = string(typed)
		return m, nil
	case runScreenSetInputMsg:
		if m.input.Value() != string(typed) {
			m.input.SetValue(string(typed))
			m.input.CursorEnd()
		}
		return m, nil
	case runScreenQuitMsg:
		return m, tea.Quit
	case tea.KeyMsg:
		switch typed.Type {
		case tea.KeyCtrlC:
			if m.onStop != nil {
				m.onStop()
			}
			return m, nil
		case tea.KeyEnter:
			if m.onSubmitted != nil {
				m.onSubmitted(m.input.Value())
			}
			m.input.SetValue("")
			return m, nil
		case tea.KeyPgUp, tea.KeyPgDown, tea.KeyUp, tea.KeyDown, tea.KeyHome, tea.KeyEnd:
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(typed)
			return m, cmd
		}

		previous := m.input.Value()
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(typed)
		if m.input.Value() != previous && m.onInputChanged != nil {
			m.onInputChanged(m.input.Value())
		}
		return m, cmd
	}

	return m, nil
}

func (m runScreenModel) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}

	status := m.styles.status.Width(m.width).Render(m.statusText())
	return m.viewport.View() + "\n" + status + "\n" + m.input.View()
}

func (m *runScreenModel) syncLayout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}

	outputHeight := m.height - 2
	if outputHeight < 1 {
		outputHeight = 1
	}

	m.viewport.Width = m.width
	m.viewport.Height = outputHeight
	inputWidth := m.width - lipgloss.Width(m.input.Prompt)
	if inputWidth < 1 {
		inputWidth = 1
	}
	m.input.Width = inputWidth
	m.syncViewport(false)
}

func (m *runScreenModel) syncViewport(autoBottom bool) {
	content := strings.Join(m.formattedOutputLines(), "\n")
	m.viewport.SetContent(content)
	if autoBottom {
		m.viewport.GotoBottom()
	}
}

func (m runScreenModel) formattedOutputLines() []string {
	lines := make([]string, 0, len(m.lines)+1)
	for _, line := range m.lines {
		lines = appendWrappedStyledRunScreenLine(lines, line, m.width, m.styles)
	}
	if m.current != "" {
		lines = appendWrappedStyledRunScreenLine(lines, m.current, m.width, m.styles)
	}
	return lines
}

func (m runScreenModel) statusText() string {
	if strings.TrimSpace(m.statusLine) == "" {
		return "running"
	}
	return truncateRunText(strings.TrimSpace(m.statusLine), max(1, m.width-2))
}

func appendRunScreenText(lines *[]string, current *string, text string) {
	text = strings.ReplaceAll(text, "\r", "")
	parts := strings.Split(text, "\n")
	if len(parts) == 1 {
		*current += parts[0]
		return
	}

	*current += parts[0]
	*lines = append(*lines, *current)
	for _, part := range parts[1 : len(parts)-1] {
		*lines = append(*lines, part)
	}
	*current = parts[len(parts)-1]
}

func appendWrappedStyledRunScreenLine(lines []string, line string, width int, styles runScreenStyles) []string {
	for _, wrapped := range wrapRunScreenLine(line, width) {
		lines = append(lines, styleRunScreenLine(wrapped, styles))
	}
	return lines
}

func wrapRunScreenLine(line string, width int) []string {
	if width <= 0 || lipgloss.Width(line) <= width {
		return []string{line}
	}

	indent := leadingWhitespace(line)
	tokens := splitRunWrapTokens(line)
	if len(tokens) == 0 {
		return []string{line}
	}

	lines := make([]string, 0, 4)
	current := ""
	lineIndent := ""

	for _, token := range tokens {
		if current == "" {
			current = strings.TrimLeft(token, " ")
			if current == "" {
				current = indent
			}
			continue
		}

		candidate := current + token
		if lipgloss.Width(candidate) <= width {
			current = candidate
			continue
		}

		lines = append(lines, strings.TrimRight(current, " "))
		if lineIndent == "" {
			lineIndent = indent
			if strings.TrimSpace(lineIndent) == "" {
				lineIndent = "  "
			}
		}

		trimmed := strings.TrimLeft(token, " ")
		if trimmed == "" {
			current = lineIndent
			continue
		}
		current = lineIndent + trimmed
		for lipgloss.Width(current) > width && width > lipgloss.Width(lineIndent) {
			available := max(1, width-lipgloss.Width(lineIndent))
			chunk, rest := splitRunWrapChunk(strings.TrimPrefix(current, lineIndent), available)
			lines = append(lines, lineIndent+chunk)
			if rest == "" {
				current = lineIndent
				break
			}
			current = lineIndent + rest
		}
	}

	if strings.TrimSpace(current) != "" {
		lines = append(lines, strings.TrimRight(current, " "))
	}
	if len(lines) == 0 {
		return []string{line}
	}
	return lines
}

func splitRunWrapTokens(line string) []string {
	parts := strings.SplitAfter(line, " ")
	if len(parts) == 0 {
		return []string{line}
	}
	return parts
}

func splitRunWrapChunk(s string, width int) (string, string) {
	if lipgloss.Width(s) <= width {
		return s, ""
	}
	runes := []rune(s)
	if width >= len(runes) {
		return s, ""
	}
	return string(runes[:width]), strings.TrimLeft(string(runes[width:]), " ")
}

func leadingWhitespace(s string) string {
	idx := 0
	for idx < len(s) && (s[idx] == ' ' || s[idx] == '\t') {
		idx++
	}
	return s[:idx]
}

func styleRunScreenLine(line string, styles runScreenStyles) string {
	switch runScreenLineStyleKind(line) {
	case "run_header":
		return styles.runHeader.Render(line)
	case "tool":
		return styles.tool.Render(line)
	case "result":
		return styles.result.Render(line)
	case "done":
		return styles.done.Render(line)
	case "info":
		return styles.info.Render(line)
	case "hint":
		return styles.hint.Render(line)
	default:
		return line
	}
}

func runScreenLineStyleKind(line string) string {
	trimmed := strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(trimmed, "run #"):
		return "run_header"
	case strings.HasPrefix(trimmed, "tool:"):
		return "tool"
	case strings.HasPrefix(trimmed, "->") || strings.HasPrefix(trimmed, "  ->"):
		return "result"
	case strings.HasPrefix(trimmed, "done"):
		return "done"
	case strings.HasPrefix(trimmed, "info:"):
		return "info"
	case strings.HasPrefix(trimmed, "type /wait"):
		return "hint"
	default:
		return "plain"
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
