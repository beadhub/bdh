package commands

import (
	"io"
	"os"
	"strings"
	"sync"
	"time"

	awrun "github.com/awebai/aw/run"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

var _ awrun.UI = (*runScreenManager)(nil)

type runScreenManager struct {
	inputFile   *os.File
	outputFile  *os.File
	promptLabel string
	footerID    string

	mu          sync.Mutex
	lines       []string
	current     string
	statusLine  string
	inputLine   string
	pending     bool
	active      bool
	exitConfirm bool

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
	FooterID    string
	ExitConfirm bool
}

type runScreenAppendTextMsg string
type runScreenSetStatusMsg string
type runScreenSetInputMsg string
type runScreenSetFooterIDMsg string
type runScreenSetExitConfirmMsg bool
type runScreenQuitMsg struct{}

type runScreenFocus int

const (
	runScreenFocusInput runScreenFocus = iota
	runScreenFocusViewport
)

type runScreenModel struct {
	viewport    viewport.Model
	input       textarea.Model
	width       int
	height      int
	promptLabel string
	footerID    string
	exitConfirm bool
	focus       runScreenFocus

	lines      []string
	current    string
	statusLine string
	styles     runScreenStyles

	onInputChanged func(string)
	onSubmitted    func(string)
	onInterrupt    func()
	onExitPrompt   func()
	onExitConfirm  func()
	onExitCancel   func()
}

type runScreenStyles struct {
	runHeader lipgloss.Style
	tool      lipgloss.Style
	result    lipgloss.Style
	done      lipgloss.Style
	info      lipgloss.Style
	status    lipgloss.Style
	hint      lipgloss.Style
	divider   lipgloss.Style
}

const (
	runScreenFooterChromeLines = 2
	runScreenFixedInputHeight  = 2
)

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
	model := newRunScreenModel(
		snapshot,
		s.handleInputChanged,
		s.handleInputSubmitted,
		s.handleInterruptRequested,
		s.handleExitPromptRequested,
		s.handleExitConfirmed,
		s.handleExitCanceled,
	)
	program := tea.NewProgram(
		model,
		tea.WithInput(s.inputFile),
		tea.WithOutput(s.outputFile),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
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

// SetPromptLabel must be called before Start. Calling it after the Bubble Tea
// program is running updates the manager's state but does not propagate the
// label change into the live model's textarea prompt function.
func (s *runScreenManager) SetPromptLabel(label string) {
	if s == nil {
		return
	}
	if strings.TrimSpace(label) == "" {
		label = defaultRunInputPromptLabel
	}

	s.mu.Lock()
	previousPrompt := s.promptLabel
	s.promptLabel = label
	if s.inputLine == "" || s.inputLine == defaultRunInputPromptLabel || s.inputLine == previousPrompt {
		s.inputLine = label
	}
	s.mu.Unlock()
}

func (s *runScreenManager) SetFooterIdentity(identity string) {
	if s == nil {
		return
	}

	s.mu.Lock()
	s.footerID = strings.TrimSpace(identity)
	program := s.program
	s.mu.Unlock()

	if program != nil {
		program.Send(runScreenSetFooterIDMsg(identity))
	}
}

func (s *runScreenManager) HasActiveProgram() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.program != nil
}

func (s *runScreenManager) hasActiveProgram() bool {
	return s.HasActiveProgram()
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
		FooterID:    s.footerID,
		ExitConfirm: s.exitConfirm,
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

func (s *runScreenManager) handleInterruptRequested() {
	s.emit(runControlEvent{Type: runControlInterrupt})
}

func (s *runScreenManager) handleExitPromptRequested() {
	s.emit(runControlEvent{Type: runControlExitPrompt})
}

func (s *runScreenManager) handleExitConfirmed() {
	s.emit(runControlEvent{Type: runControlExitConfirm})
}

func (s *runScreenManager) handleExitCanceled() {
	s.emit(runControlEvent{Type: runControlExitCancel})
}

func (s *runScreenManager) SetExitConfirmation(active bool) {
	if s == nil {
		return
	}

	s.mu.Lock()
	s.exitConfirm = active
	program := s.program
	s.mu.Unlock()

	if program != nil {
		program.Send(runScreenSetExitConfirmMsg(active))
	}
}

func newRunScreenModel(
	snapshot runScreenSnapshot,
	onInputChanged func(string),
	onSubmitted func(string),
	onInterrupt func(),
	onExitPrompt func(),
	onExitConfirm func(),
	onExitCancel func(),
) runScreenModel {
	input := textarea.New()
	input.Prompt = snapshot.PromptLabel
	input.ShowLineNumbers = false
	input.SetValue(runInputValueFromLine(snapshot.InputLine, snapshot.PromptLabel))
	input.Focus()
	input.CharLimit = 0
	input.SetPromptFunc(lipgloss.Width(snapshot.PromptLabel), func(lineIdx int) string {
		if lineIdx == 0 {
			return snapshot.PromptLabel
		}
		return strings.Repeat(" ", lipgloss.Width(snapshot.PromptLabel))
	})
	input.FocusedStyle.CursorLine = lipgloss.NewStyle()
	input.FocusedStyle.Base = lipgloss.NewStyle()
	input.FocusedStyle.Text = lipgloss.NewStyle()
	input.BlurredStyle.CursorLine = lipgloss.NewStyle()
	input.BlurredStyle.Base = lipgloss.NewStyle()
	input.BlurredStyle.Text = lipgloss.NewStyle()

	model := runScreenModel{
		viewport:       viewport.New(0, 0),
		input:          input,
		promptLabel:    snapshot.PromptLabel,
		footerID:       snapshot.FooterID,
		exitConfirm:    snapshot.ExitConfirm,
		focus:          runScreenFocusInput,
		lines:          snapshot.Lines,
		current:        snapshot.Current,
		statusLine:     snapshot.StatusLine,
		styles:         newRunScreenStyles(),
		onInputChanged: onInputChanged,
		onSubmitted:    onSubmitted,
		onInterrupt:    onInterrupt,
		onExitPrompt:   onExitPrompt,
		onExitConfirm:  onExitConfirm,
		onExitCancel:   onExitCancel,
	}
	model.setFocus(runScreenFocusInput)
	model.syncViewport(true)
	return model
}

func newRunScreenStyles() runScreenStyles {
	return runScreenStyles{
		runHeader: lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "24", Dark: "12"}).Bold(true),
		tool:      lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "130", Dark: "214"}).Bold(true),
		result:    lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "23", Dark: "14"}),
		done:      lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "10"}).Bold(true),
		info:      lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "240", Dark: "8"}),
		status: lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "236", Dark: "252"}).
			Background(lipgloss.AdaptiveColor{Light: "252", Dark: "236"}),
		hint:    lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "240", Dark: "8"}),
		divider: lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "248", Dark: "239"}),
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
			m.syncLayout()
		}
		return m, nil
	case runScreenSetFooterIDMsg:
		m.footerID = strings.TrimSpace(string(typed))
		return m, nil
	case runScreenSetExitConfirmMsg:
		m.exitConfirm = bool(typed)
		return m, nil
	case runScreenQuitMsg:
		return m, tea.Quit
	case tea.MouseMsg:
		return m.handleMouse(typed)
	case tea.KeyMsg:
		if m.exitConfirm {
			switch typed.Type {
			case tea.KeyCtrlC, tea.KeyCtrlD:
				if m.onExitConfirm != nil {
					m.onExitConfirm()
				}
				return m, nil
			case tea.KeyEsc:
				m.exitConfirm = false
				if m.onExitCancel != nil {
					m.onExitCancel()
				}
				return m, nil
			case tea.KeyRunes:
				if len(typed.Runes) == 1 {
					switch typed.Runes[0] {
					case 'y', 'Y':
						if m.onExitConfirm != nil {
							m.onExitConfirm()
						}
						return m, nil
					case 'n', 'N':
						m.exitConfirm = false
						if m.onExitCancel != nil {
							m.onExitCancel()
						}
						return m, nil
					}
				}
				m.exitConfirm = false
				if m.onExitCancel != nil {
					m.onExitCancel()
				}
			default:
				m.exitConfirm = false
				if m.onExitCancel != nil {
					m.onExitCancel()
				}
			}
		}

		switch typed.Type {
		case tea.KeyCtrlC:
			if m.onInterrupt != nil {
				m.onInterrupt()
			}
			return m, nil
		case tea.KeyCtrlD:
			if m.onExitPrompt != nil {
				m.onExitPrompt()
			}
			return m, nil
		case tea.KeyEsc:
			m.setFocus(runScreenFocusInput)
			return m, nil
		}

		if m.focus == runScreenFocusViewport {
			switch typed.Type {
			case tea.KeyPgUp, tea.KeyUp, tea.KeyHome, tea.KeyPgDown, tea.KeyEnd:
				var cmd tea.Cmd
				m.viewport, cmd = m.viewport.Update(typed)
				return m, cmd
			case tea.KeyDown:
				if m.viewport.AtBottom() {
					m.setFocus(runScreenFocusInput)
					return m, nil
				}
				var cmd tea.Cmd
				m.viewport, cmd = m.viewport.Update(typed)
				return m, cmd
			case tea.KeyEnter:
				m.setFocus(runScreenFocusInput)
				return m, nil
			default:
				// Returns focus to input and falls through to the input
				// handling below so the keystroke is typed into the textarea.
				m.setFocus(runScreenFocusInput)
			}
		} else {
			switch typed.Type {
			case tea.KeyUp, tea.KeyPgUp, tea.KeyHome:
				m.setFocus(runScreenFocusViewport)
				var cmd tea.Cmd
				m.viewport, cmd = m.viewport.Update(typed)
				return m, cmd
			}
		}

		switch typed.Type {
		case tea.KeyEnter:
			if m.onSubmitted != nil {
				m.onSubmitted(m.input.Value())
			}
			m.input.SetValue("")
			m.syncLayout()
			return m, nil
		}

		previous := m.input.Value()
		previousHeight := m.input.Height()
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(typed)
		m.syncLayout()
		if m.input.Height() > previousHeight {
			m.restoreWrappedInputViewport()
		}
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

	divider := m.styles.divider.Render(strings.Repeat("─", max(1, m.width)))
	status := m.styles.status.Width(m.width).Render(m.footerText())
	return m.viewport.View() + "\n" + divider + "\n" + m.input.View() + "\n" + status
}

func (m *runScreenModel) syncLayout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}

	m.input.SetWidth(m.width)
	maxInputHeight := max(1, m.height-runScreenFooterChromeLines-1)
	inputHeight := runScreenFixedInputHeight
	if inputHeight > maxInputHeight {
		inputHeight = maxInputHeight
	}
	m.input.SetHeight(inputHeight)

	outputHeight := m.height - (runScreenFooterChromeLines + inputHeight)
	if outputHeight < 1 {
		outputHeight = 1
	}

	m.viewport.Width = m.width
	m.viewport.Height = outputHeight
	m.syncViewport(false)
}

func (m *runScreenModel) syncViewport(autoBottom bool) {
	content := strings.Join(m.formattedOutputLines(), "\n")
	m.viewport.SetContent(content)
	if autoBottom {
		m.viewport.GotoBottom()
	}
}

func (m *runScreenModel) restoreWrappedInputViewport() {
	value := m.input.Value()
	cursorCol := runInputCursorColumn(m.input)
	m.input.SetValue(value)
	m.input.SetCursor(cursorCol)
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

func (m *runScreenModel) setFocus(focus runScreenFocus) {
	m.focus = focus
	if focus == runScreenFocusViewport {
		m.input.Blur()
		return
	}
	m.input.Focus()
}

func (m runScreenModel) footerText() string {
	left := strings.TrimSpace(m.footerID)
	right := runScreenFooterStatus(m.statusLine)
	return runScreenFooterLine(left, right, m.width)
}

func runScreenFooterStatus(statusLine string) string {
	status := strings.TrimSpace(statusLine)
	switch {
	case status == "":
		return "running"
	case strings.HasPrefix(status, "waiting for work"):
		return "...waiting for work"
	case strings.HasPrefix(status, "next run"):
		return "running"
	default:
		return status
	}
}

func runScreenFooterLine(left string, right string, width int) string {
	if width <= 0 {
		return ""
	}
	if right == "" {
		return truncateRunText(left, width)
	}

	right = truncateRunText(right, width)
	rightWidth := lipgloss.Width(right)
	if rightWidth >= width {
		return right
	}

	leftWidth := width - rightWidth - 1
	if leftWidth < 0 {
		leftWidth = 0
	}
	left = truncateRunText(left, leftWidth)
	padding := width - lipgloss.Width(left) - rightWidth
	if padding < 1 {
		padding = 1
	}
	return left + strings.Repeat(" ", padding) + right
}

func (m runScreenModel) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action != tea.MouseActionPress {
		return m, nil
	}

	if msg.Button == tea.MouseButtonLeft {
		switch {
		case m.mouseInViewport(msg.Y):
			m.setFocus(runScreenFocusViewport)
		case m.mouseInInput(msg.Y):
			m.setFocus(runScreenFocusInput)
		}
		return m, nil
	}

	if tea.MouseEvent(msg).IsWheel() && m.mouseInViewport(msg.Y) {
		m.setFocus(runScreenFocusViewport)
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m runScreenModel) mouseInViewport(y int) bool {
	return y >= 0 && y < m.viewport.Height
}

func (m runScreenModel) mouseInInput(y int) bool {
	start := m.viewport.Height + 1
	end := start + m.input.Height()
	return y >= start && y < end
}

func runInputVisualHeight(promptLabel string, value string, width int) int {
	if width <= 0 {
		return 1
	}

	promptWidth := lipgloss.Width(promptLabel)
	availableWidth := width - promptWidth
	if availableWidth < 1 {
		availableWidth = 1
	}

	lines := strings.Split(value, "\n")
	if len(lines) == 0 {
		return 1
	}

	height := 0
	for _, line := range lines {
		if line == "" {
			height++
			continue
		}

		currentWidth := 0
		height++
		for _, r := range line {
			runeWidth := lipgloss.Width(string(r))
			if runeWidth <= 0 {
				runeWidth = 1
			}
			if currentWidth+runeWidth > availableWidth {
				height++
				currentWidth = runeWidth
				continue
			}
			currentWidth += runeWidth
		}
	}

	if height < 1 {
		return 1
	}
	return height
}

func runInputCursorColumn(input textarea.Model) int {
	info := input.LineInfo()
	return info.StartColumn + info.CharOffset
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
			trimmed := strings.TrimLeft(token, " ")
			if trimmed == "" {
				current = indent
			} else if indent != "" {
				current = indent + trimmed
			} else {
				current = trimmed
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
			if lineIndent == "" {
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
		return styleRunScreenToolLine(line, styles)
	case "result":
		return styles.result.Render(line)
	case "done":
		return styles.done.Render(line)
	case "info":
		return styles.info.Render(line)
	case "hint":
		return styles.hint.Render(line)
	default:
		return styleRunScreenToolClosingParen(line, styles)
	}
}

func styleRunScreenToolLine(line string, styles runScreenStyles) string {
	idx := strings.Index(line, "(")
	if idx < 0 {
		return styles.tool.Render(line)
	}
	return styles.tool.Render(line[:idx+1]) + styleRunScreenToolClosingParen(line[idx+1:], styles)
}

func styleRunScreenToolClosingParen(line string, styles runScreenStyles) string {
	trimmed := strings.TrimRight(line, " ")
	if trimmed == "" || !strings.HasSuffix(trimmed, ")") {
		return line
	}
	suffixStart := len(trimmed) - 1
	return line[:suffixStart] + styles.tool.Render(")") + line[len(trimmed):]
}

func runScreenLineStyleKind(line string) string {
	trimmed := strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(trimmed, "run #"):
		return "run_header"
	case strings.HasPrefix(trimmed, "- ") && strings.Contains(trimmed, "("):
		return "tool"
	case strings.HasPrefix(trimmed, "->") || strings.HasPrefix(trimmed, "  ->"):
		return "result"
	case strings.HasPrefix(trimmed, "done"):
		return "done"
	case strings.HasPrefix(trimmed, "info:"):
		return "info"
	case strings.HasPrefix(trimmed, "type /"):
		return "hint"
	default:
		return "plain"
	}
}
