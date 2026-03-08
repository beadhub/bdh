package commands

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type runWakeEventType string

const (
	runWakeEventConnected        runWakeEventType = "connected"
	runWakeEventMailMessage      runWakeEventType = "mail_message"
	runWakeEventChatMessage      runWakeEventType = "chat_message"
	runWakeEventWorkAvailable    runWakeEventType = "work_available"
	runWakeEventClaimUpdate      runWakeEventType = "claim_update"
	runWakeEventClaimRemoved     runWakeEventType = "claim_removed"
	runWakeEventControlPause     runWakeEventType = "control_pause"
	runWakeEventControlResume    runWakeEventType = "control_resume"
	runWakeEventControlInterrupt runWakeEventType = "control_interrupt"
	runWakeEventError            runWakeEventType = "error"
)

type runWakeEvent struct {
	Type      runWakeEventType
	AgentID   string
	ProjectID string

	MessageID string
	FromAlias string
	SessionID string
	Subject   string

	TaskID string
	Title  string
	Status string

	SignalID string
	Text     string
}

type runEventStreamClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func newRunEventStreamClient(beadhubURL string) (*runEventStreamClient, error) {
	sel, err := resolveBeadhubAuth(beadhubURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(sel.APIKey) == "" {
		return nil, fmt.Errorf("missing beadhub API key (configure ~/.config/aw/config.yaml + .aw/context, or set BEADHUB_API_KEY)")
	}
	return &runEventStreamClient{
		baseURL:    strings.TrimRight(sel.BaseURL, "/"),
		apiKey:     sel.APIKey,
		httpClient: http.DefaultClient,
	}, nil
}

func (c *runEventStreamClient) Stream(ctx context.Context, deadline time.Time) (<-chan runWakeEvent, <-chan error) {
	events := make(chan runWakeEvent, 32)
	errs := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errs)
		if err := c.stream(ctx, deadline, events); err != nil && ctx.Err() == nil {
			errs <- err
		}
	}()

	return events, errs
}

func (c *runEventStreamClient) stream(ctx context.Context, deadline time.Time, events chan<- runWakeEvent) error {
	if c == nil {
		return fmt.Errorf("event stream client is nil")
	}
	if strings.TrimSpace(c.baseURL) == "" {
		return fmt.Errorf("event stream base URL is empty")
	}
	if strings.TrimSpace(c.apiKey) == "" {
		return fmt.Errorf("event stream API key is empty")
	}
	httpClient := c.httpClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	endpoint, err := url.Parse(c.baseURL + "/v1/events/stream")
	if err != nil {
		return fmt.Errorf("parse event stream URL: %w", err)
	}
	query := endpoint.Query()
	query.Set("deadline", deadline.UTC().Format(time.RFC3339))
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return fmt.Errorf("build event stream request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("event stream request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		bodyText := strings.TrimSpace(string(body))
		if bodyText == "" {
			return fmt.Errorf("event stream returned %s", resp.Status)
		}
		return fmt.Errorf("event stream returned %s: %s", resp.Status, bodyText)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var eventName string
	var dataLines []string

	flush := func() error {
		if strings.TrimSpace(eventName) == "" && len(dataLines) == 0 {
			return nil
		}
		evt, ok, err := parseRunWakeEvent(eventName, strings.Join(dataLines, "\n"))
		eventName = ""
		dataLines = dataLines[:0]
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case events <- evt:
			return nil
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "event:"):
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("read event stream: %w", err)
	}
	if err := flush(); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

func parseRunWakeEvent(eventName, data string) (runWakeEvent, bool, error) {
	eventName = strings.TrimSpace(eventName)
	data = strings.TrimSpace(data)
	if eventName == "" {
		return runWakeEvent{}, false, nil
	}

	switch runWakeEventType(eventName) {
	case runWakeEventConnected:
		var payload struct {
			AgentID   string `json:"agent_id"`
			ProjectID string `json:"project_id"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return runWakeEvent{}, false, fmt.Errorf("parse connected event: %w", err)
		}
		return runWakeEvent{
			Type:      runWakeEventConnected,
			AgentID:   payload.AgentID,
			ProjectID: payload.ProjectID,
		}, true, nil

	case runWakeEventMailMessage:
		var payload struct {
			MessageID string `json:"message_id"`
			FromAlias string `json:"from_alias"`
			Subject   string `json:"subject"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return runWakeEvent{}, false, fmt.Errorf("parse mail_message event: %w", err)
		}
		return runWakeEvent{
			Type:      runWakeEventMailMessage,
			MessageID: payload.MessageID,
			FromAlias: payload.FromAlias,
			Subject:   payload.Subject,
		}, true, nil

	case runWakeEventChatMessage:
		var payload struct {
			MessageID string `json:"message_id"`
			FromAlias string `json:"from_alias"`
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return runWakeEvent{}, false, fmt.Errorf("parse chat_message event: %w", err)
		}
		return runWakeEvent{
			Type:      runWakeEventChatMessage,
			MessageID: payload.MessageID,
			FromAlias: payload.FromAlias,
			SessionID: payload.SessionID,
		}, true, nil

	case runWakeEventWorkAvailable:
		var payload struct {
			TaskID string `json:"task_id"`
			Title  string `json:"title"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return runWakeEvent{}, false, fmt.Errorf("parse work_available event: %w", err)
		}
		return runWakeEvent{
			Type:   runWakeEventWorkAvailable,
			TaskID: payload.TaskID,
			Title:  payload.Title,
		}, true, nil

	case runWakeEventClaimUpdate:
		var payload struct {
			TaskID string `json:"task_id"`
			Title  string `json:"title"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return runWakeEvent{}, false, fmt.Errorf("parse claim_update event: %w", err)
		}
		return runWakeEvent{
			Type:   runWakeEventClaimUpdate,
			TaskID: payload.TaskID,
			Title:  payload.Title,
			Status: payload.Status,
		}, true, nil

	case runWakeEventClaimRemoved:
		var payload struct {
			TaskID string `json:"task_id"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return runWakeEvent{}, false, fmt.Errorf("parse claim_removed event: %w", err)
		}
		return runWakeEvent{
			Type:   runWakeEventClaimRemoved,
			TaskID: payload.TaskID,
		}, true, nil

	case runWakeEventControlPause, runWakeEventControlResume, runWakeEventControlInterrupt:
		var payload struct {
			SignalID string `json:"signal_id"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return runWakeEvent{}, false, fmt.Errorf("parse %s event: %w", eventName, err)
		}
		return runWakeEvent{
			Type:     runWakeEventType(eventName),
			SignalID: payload.SignalID,
		}, true, nil

	case runWakeEventError:
		var payload map[string]any
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return runWakeEvent{}, false, fmt.Errorf("parse error event: %w", err)
		}
		return runWakeEvent{
			Type: runWakeEventError,
			Text: strings.TrimSpace(string(mustJSON(payload))),
		}, true, nil

	default:
		return runWakeEvent{}, false, nil
	}
}

func mustJSON(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return bytes.TrimSpace(data)
}
