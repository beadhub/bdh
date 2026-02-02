// Package client implements the BeadHub HTTP and SSE clients.
package client

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

const (
	// MaxSSELineSize is the maximum size of a single SSE line (64KB).
	// Lines exceeding this will cause the connection to close.
	MaxSSELineSize = 64 * 1024

	// MaxSSEEventSize is the maximum total size of an SSE event's data (1MB).
	// Events exceeding this will be discarded.
	MaxSSEEventSize = 1024 * 1024
)

// SSEEvent represents a Server-Sent Event.
type SSEEvent struct {
	// Type is the event type (from "event:" line). Defaults to "message".
	Type string

	// Data is the event payload (from "data:" lines, joined with newlines).
	Data string

	// ID is the optional event ID (from "id:" line).
	ID string
}

// SSE is a Server-Sent Events client for streaming chat events.
type SSE struct {
	httpClient *http.Client
}

// NewSSE creates a new SSE client.
func NewSSE() *SSE {
	return &SSE{
		httpClient: &http.Client{
			// No timeout - SSE connections are long-lived
		},
	}
}

// Connect establishes an SSE connection and returns a channel of events.
// The channel is closed when the connection ends or context is cancelled.
func (s *SSE) Connect(ctx context.Context, url string) (<-chan SSEEvent, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connecting to SSE: %w", err)
	}

	// Check for HTTP errors
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer func() { _ = resp.Body.Close() }() // Ensure closure even if ReadAll fails
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, &Error{
			StatusCode: resp.StatusCode,
			Body:       string(body),
		}
	}

	// Buffered channel improves throughput; goroutine exits via ctx.Done()
	events := make(chan SSEEvent, 100)

	go s.readEvents(ctx, resp.Body, events)

	return events, nil
}

// readEvents reads SSE events from the response body and sends them to the channel.
func (s *SSE) readEvents(ctx context.Context, body io.ReadCloser, events chan<- SSEEvent) {
	defer close(events)
	defer func() { _ = body.Close() }()

	// Use Scanner with explicit buffer limit to prevent memory DoS
	scanner := bufio.NewScanner(body)
	buf := make([]byte, MaxSSELineSize)
	scanner.Buffer(buf, MaxSSELineSize)

	var event SSEEvent
	var dataLines []string
	var eventSize int
	var eventOversized bool

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if !scanner.Scan() {
			// EOF, error, or line too long - close the channel
			if err := scanner.Err(); err != nil {
				// Don't log context deadline/cancellation - these are expected when timeout occurs
				if ctx.Err() == nil {
					fmt.Fprintf(os.Stderr, "SSE: connection error: %v\n", err)
				}
			}
			return
		}

		line := scanner.Text()

		// Empty line signals end of event
		if line == "" {
			if len(dataLines) > 0 && !eventOversized {
				// Build and send the event
				event.Data = strings.Join(dataLines, "\n")
				if event.Type == "" {
					event.Type = "message" // Default SSE event type
				}

				select {
				case events <- event:
				case <-ctx.Done():
					return
				}
			}

			// Reset for next event
			event = SSEEvent{}
			dataLines = nil
			eventSize = 0
			eventOversized = false
			continue
		}

		// Comments (lines starting with :) are ignored
		if strings.HasPrefix(line, ":") {
			continue
		}

		// Parse field: value
		colonIdx := strings.Index(line, ":")
		if colonIdx == -1 {
			// Field name only, no value
			continue
		}

		field := line[:colonIdx]
		value := line[colonIdx+1:]

		// Remove leading space from value (per SSE spec)
		value = strings.TrimPrefix(value, " ")

		switch field {
		case "event":
			event.Type = value
		case "data":
			if eventOversized {
				continue // Skip processing oversized event data
			}
			// Track event size to prevent memory DoS from many small lines
			newSize := eventSize + len(value)
			if eventSize > 0 {
				newSize++ // Add joining newline for all but the first line
			}
			if newSize > MaxSSEEventSize {
				fmt.Fprintf(os.Stderr, "SSE: discarding oversized event (%d bytes, limit %d)\n", newSize, MaxSSEEventSize)
				eventOversized = true
				dataLines = nil // Release already-accumulated data
				continue
			}
			dataLines = append(dataLines, value)
			eventSize = newSize
		case "id":
			event.ID = value
			// "retry" field is ignored (connection retry delay)
		}
	}
}
