package commands

import (
	"strings"
	"testing"
)

func TestRunScreenManagerRenderFrameUsesCarriageReturnNewlines(t *testing.T) {
	screen := &runScreenManager{
		lines:      []string{"first line", "second line"},
		statusLine: "status",
		inputLine:  "input> hello",
	}

	frame := screen.renderFrame(40, 6)

	if !strings.Contains(frame, "first line\033[K\r\nsecond line") {
		t.Fatalf("expected CRLF-separated output lines, got %q", frame)
	}
	if !strings.Contains(frame, "status\033[K\r\ninput> hello") {
		t.Fatalf("expected CRLF before input line, got %q", frame)
	}
	if strings.Contains(frame, "first line\033[K\nsecond line") {
		t.Fatalf("unexpected bare LF-separated output lines, got %q", frame)
	}
}
