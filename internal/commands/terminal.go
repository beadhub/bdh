package commands

import "strings"

type runControlEventType string

const (
	runControlTypingStarted runControlEventType = "typing_started"
	runControlBufferUpdated runControlEventType = "buffer_updated"
	runControlPrompt        runControlEventType = "prompt"
	runControlQuit          runControlEventType = "quit"
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

func parseRunControlSubmission(text string) runControlEvent {
	text = strings.TrimSpace(text)
	switch text {
	case "/quit", "/exit":
		return runControlEvent{Type: runControlQuit}
	case "/stop":
		return runControlEvent{Type: runControlStop}
	case "/wait":
		return runControlEvent{Type: runControlWait}
	case "/resume":
		return runControlEvent{Type: runControlResume}
	default:
		return runControlEvent{Type: runControlPrompt, Text: text}
	}
}

func runInputValueFromLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" || line == "input>" {
		return ""
	}
	if strings.HasPrefix(line, "input> ") {
		return strings.TrimPrefix(line, "input> ")
	}
	return line
}
