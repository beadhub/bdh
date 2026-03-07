package commands

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"golang.org/x/term"
)

type runScreenManager struct {
	file       *os.File
	mu         sync.Mutex
	lines      []string
	current    string
	statusLine string
	inputLine  string
	active     bool
}

func newRunScreenManager(out io.Writer) *runScreenManager {
	file, ok := out.(*os.File)
	if !ok {
		return nil
	}
	if !term.IsTerminal(int(file.Fd())) {
		return nil
	}
	return &runScreenManager{file: file}
}

func (s *runScreenManager) Start() error {
	if s == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.active {
		return nil
	}
	s.active = true
	if s.inputLine == "" {
		s.inputLine = "input> "
	}
	_, err := fmt.Fprint(s.file, "\033[?1049h\033[H\033[2J")
	if err != nil {
		s.active = false
		return err
	}
	return s.renderLocked()
}

func (s *runScreenManager) Stop() error {
	if s == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.active {
		return nil
	}
	s.active = false
	_, err := fmt.Fprint(s.file, "\033[?1049l")
	return err
}

func (s *runScreenManager) AppendText(text string) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	text = strings.ReplaceAll(text, "\r", "")
	parts := strings.Split(text, "\n")
	if len(parts) == 1 {
		s.current += parts[0]
		_ = s.renderLocked()
		return
	}

	s.current += parts[0]
	s.lines = append(s.lines, s.current)
	for _, part := range parts[1 : len(parts)-1] {
		s.lines = append(s.lines, part)
	}
	s.current = parts[len(parts)-1]
	_ = s.renderLocked()
}

func (s *runScreenManager) AppendLine(line string) {
	s.AppendText(line + "\n")
}

func (s *runScreenManager) SetInputLine(line string) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.inputLine = line
	_ = s.renderLocked()
}

func (s *runScreenManager) SetStatusLine(line string) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.statusLine = line
	_ = s.renderLocked()
}

func (s *runScreenManager) ClearStatusLine() {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.statusLine = ""
	_ = s.renderLocked()
}

func (s *runScreenManager) ClearInputLine() {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.inputLine = ""
	_ = s.renderLocked()
}

func (s *runScreenManager) renderLocked() error {
	if !s.active {
		return nil
	}

	width, height, err := term.GetSize(int(s.file.Fd()))
	if err != nil || width <= 0 || height <= 0 {
		width, height = 120, 24
	}

	outputHeight := height - 2
	if outputHeight < 1 {
		outputHeight = 1
	}

	lines := make([]string, 0, len(s.lines)+1)
	lines = append(lines, s.lines...)
	if s.current != "" {
		lines = append(lines, s.current)
	}
	if len(lines) > outputHeight {
		lines = lines[len(lines)-outputHeight:]
	}

	var b strings.Builder
	b.WriteString("\033[H\033[2J")
	for i := 0; i < outputHeight; i++ {
		if i < len(lines) {
			b.WriteString(truncateRunText(lines[i], width))
		}
		b.WriteString("\033[K")
		if i < outputHeight-1 {
			b.WriteByte('\n')
		}
	}
	b.WriteByte('\n')
	b.WriteString(truncateRunText(s.statusLine, width))
	b.WriteString("\033[K\n")
	b.WriteString(truncateRunText(s.inputLine, width))
	b.WriteString("\033[K")
	cursorCol := len(truncateRunText(s.inputLine, width)) + 1
	if cursorCol > width {
		cursorCol = width
	}
	b.WriteString(fmt.Sprintf("\033[%d;%dH", height, cursorCol))
	_, err = fmt.Fprint(s.file, b.String())
	return err
}
