// Package eventstream implements the authenticated oCIS server-sent-events
// protocol. It owns one connection at a time; reconnect policy belongs to the
// application service that knows the user's retry settings.
package eventstream

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mzner/ocis-cli/internal/httpapi"
)

const (
	endpoint     = "/ocs/v2.php/apps/notifications/api/v1/notifications/sse"
	maxLineBytes = 1 << 20
	maxEventData = 4 << 20
)

// Event is one decoded server-sent event.
type Event struct {
	Type  string
	Data  string
	ID    string
	Retry time.Duration
}

// Client opens authenticated SSE connections.
type Client struct {
	api *httpapi.Client
}

// NewClient constructs an event stream client.
func NewClient(config httpapi.Config, httpClient *http.Client) *Client {
	return &Client{api: httpapi.NewClient(config, httpClient)}
}

// Watch opens one SSE connection and invokes handle for each complete event.
// It returns when the connection closes, the context is canceled, or handle
// returns an error.
func (client *Client) Watch(
	ctx context.Context, connected func(), handle func(Event) error,
) error {
	headers := make(http.Header)
	headers.Set("Accept", "text/event-stream")
	headers.Set("Cache-Control", "no-cache")
	response, err := client.api.Do(
		ctx, http.MethodGet, endpoint, nil, headers,
	)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK ||
		response.StatusCode >= http.StatusMultipleChoices {
		return httpapi.ResponseError(response)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "text/event-stream") {
		return fmt.Errorf(
			"event stream returned content type %q, want text/event-stream",
			response.Header.Get("Content-Type"),
		)
	}
	if connected != nil {
		connected()
	}
	return decode(ctx, response, handle)
}

func decode(
	ctx context.Context,
	response *http.Response,
	handle func(Event) error,
) error {
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), maxLineBytes)
	var event Event
	retryDelay := time.Duration(0)
	data := make([]string, 0, 1)
	dataBytes := 0
	dispatch := func() error {
		if len(data) == 0 {
			event.Type = ""
			event.ID = ""
			return nil
		}
		event.Data = strings.Join(data, "\n")
		event.Retry = retryDelay
		if event.Type == "" {
			event.Type = "message"
		}
		if err := handle(event); err != nil {
			return err
		}
		event = Event{}
		data = data[:0]
		dataBytes = 0
		return nil
	}
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if err := dispatch(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if !found {
			field, value = line, ""
		} else {
			value = strings.TrimPrefix(value, " ")
		}
		switch field {
		case "event":
			event.Type = value
		case "data":
			dataBytes += len(value)
			if len(data) > 0 {
				dataBytes++
			}
			if dataBytes > maxEventData {
				return errors.New("event data exceeds the 4 MiB limit")
			}
			data = append(data, value)
		case "id":
			if !strings.ContainsRune(value, '\x00') {
				event.ID = value
			}
		case "retry":
			milliseconds, parseErr := strconv.ParseInt(value, 10, 64)
			if parseErr == nil && milliseconds >= 0 &&
				milliseconds <= int64(^uint64(0)>>1)/int64(time.Millisecond) {
				retryDelay = time.Duration(milliseconds) * time.Millisecond
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("read event stream: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// The SSE parsing algorithm discards a partially received event at EOF. A
	// blank line is required to dispatch it.
	return nil
}
