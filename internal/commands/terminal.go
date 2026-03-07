package commands

import (
	"io"
	"os"
	"strings"
	"sync"

	"golang.org/x/term"
)

type runControlEventType string

const (
	runControlTypingStarted runControlEventType = "typing_started"
	runControlBufferUpdated runControlEventType = "buffer_updated"
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

type terminalRunInput struct {
	file    *os.File
	events  chan runControlEvent
	stopCh  chan struct{}
	mu      sync.Mutex
	pending bool
	started bool
	state   *term.State
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
			t.emit(runControlEvent{Type: runControlBufferUpdated, Text: ""})
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
			t.emit(runControlEvent{Type: runControlBufferUpdated, Text: string(buffer)})
		default:
			if b < 32 || b == 255 {
				continue
			}
			if len(buffer) == 0 {
				t.setPending(true)
				t.emit(runControlEvent{Type: runControlTypingStarted})
			}
			buffer = append(buffer, b)
			t.emit(runControlEvent{Type: runControlBufferUpdated, Text: string(buffer)})
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
