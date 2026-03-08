package commands

import "strings"

type runControlEventType string

const defaultRunInputPromptLabel = "input> "

const (
	runControlTypingStarted runControlEventType = "typing_started"
	runControlBufferUpdated runControlEventType = "buffer_updated"
	runControlPrompt        runControlEventType = "prompt"
	runControlQuit          runControlEventType = "quit"
	runControlStop          runControlEventType = "stop"
	runControlWait          runControlEventType = "wait"
	runControlResume        runControlEventType = "resume"
	runControlInterrupt     runControlEventType = "interrupt"
	runControlExitPrompt    runControlEventType = "exit_prompt"
	runControlExitConfirm   runControlEventType = "exit_confirm"
	runControlExitCancel    runControlEventType = "exit_cancel"
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

func formatRunInputLine(promptLabel string, value string) string {
	if strings.TrimSpace(promptLabel) == "" {
		promptLabel = defaultRunInputPromptLabel
	}
	if value == "" {
		return promptLabel
	}
	return promptLabel + value
}

func runInputValueFromLine(line string, promptLabel string) string {
	line = strings.TrimLeft(line, " \t")
	if strings.TrimSpace(promptLabel) == "" {
		promptLabel = defaultRunInputPromptLabel
	}
	trimmedPrompt := strings.TrimSpace(promptLabel)
	if line == "" || line == trimmedPrompt {
		return ""
	}
	if strings.HasPrefix(line, promptLabel) {
		return strings.TrimPrefix(line, promptLabel)
	}
	if strings.HasPrefix(line, trimmedPrompt) {
		return strings.TrimPrefix(line, trimmedPrompt)
	}
	return line
}
