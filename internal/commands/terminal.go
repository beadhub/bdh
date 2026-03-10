package commands

import (
	"strings"

	awrun "github.com/awebai/aw/run"
)

type runControlEventType = awrun.ControlEventType

const defaultRunInputPromptLabel = ">> "

const (
	runControlTypingStarted runControlEventType = awrun.ControlTypingStarted
	runControlBufferUpdated runControlEventType = awrun.ControlBufferUpdated
	runControlPrompt        runControlEventType = awrun.ControlPrompt
	runControlQuit          runControlEventType = awrun.ControlQuit
	runControlStop          runControlEventType = awrun.ControlStop
	runControlWait          runControlEventType = awrun.ControlWait
	runControlResume        runControlEventType = awrun.ControlResume
	runControlAutofeedOn    runControlEventType = awrun.ControlAutofeedOn
	runControlAutofeedOff   runControlEventType = awrun.ControlAutofeedOff
	runControlInterrupt     runControlEventType = awrun.ControlInterrupt
	runControlExitPrompt    runControlEventType = awrun.ControlExitPrompt
	runControlExitConfirm   runControlEventType = awrun.ControlExitConfirm
	runControlExitCancel    runControlEventType = awrun.ControlExitCancel
)

type runControlEvent = awrun.ControlEvent

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
	case "/autofeed on":
		return runControlEvent{Type: runControlAutofeedOn}
	case "/autofeed off":
		return runControlEvent{Type: runControlAutofeedOff}
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
