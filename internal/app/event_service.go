package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mzner/ocis-cli/internal/apperror"
	"github.com/mzner/ocis-cli/internal/eventstream"
	appoutput "github.com/mzner/ocis-cli/internal/output"
	"github.com/mzner/ocis-cli/internal/retry"
)

const eventRetryWait = 200 * time.Millisecond

type watchedEvent struct {
	Type       string `json:"type"`
	Data       any    `json:"data"`
	ID         string `json:"id,omitempty"`
	ReceivedAt string `json:"receivedAt"`
}

func runEventTypes(options RunOptions) error {
	types := eventstream.KnownTypes()
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "event-type", types)
	}
	writer := tabwriter.NewWriter(options.Out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "TYPE\tDESCRIPTION"); err != nil {
		return err
	}
	for _, eventType := range types {
		if _, err := fmt.Fprintf(
			writer, "%s\t%s\n", eventType.Name, eventType.Description,
		); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func runEventWatch(
	ctx context.Context,
	request EventWatchRequest,
	selectedProfile string,
	options RunOptions,
) error {
	if options.OutputMode == appoutput.JSON {
		return apperror.Wrap(
			apperror.KindUsage, "event watch",
			errors.New("--json cannot represent an open-ended stream; use --jsonl"),
		)
	}
	if request.MaxWait < 0 {
		return apperror.Wrap(
			apperror.KindUsage, "event watch",
			errors.New("--max-wait cannot be negative"),
		)
	}
	if request.MaxWait > 0 && !request.Once {
		return apperror.Wrap(
			apperror.KindUsage, "event watch",
			errors.New("--max-wait requires --once"),
		)
	}
	selectedTypes, err := eventTypeFilter(request.Types)
	if err != nil {
		return apperror.Wrap(apperror.KindUsage, "event watch", err)
	}
	client, err := newClientWithOptions(ctx, selectedProfile, options)
	if err != nil {
		return err
	}
	capabilities, err := client.sharingClient().Capabilities(ctx)
	if err != nil {
		return fmt.Errorf("check real-time event support: %w", err)
	}
	if !capabilities.Core.SupportSSE {
		return errors.New(
			"server does not advertise real-time event support (core.support-sse)",
		)
	}
	watchCtx := ctx
	if request.MaxWait > 0 {
		var cancel context.CancelFunc
		watchCtx, cancel = context.WithTimeout(ctx, request.MaxWait)
		defer cancel()
	}

	attempt := 0
	connection := 0
	for {
		received := false
		onceComplete := false
		loggedOut := false
		var handlerErr error
		serverDelay := time.Duration(0)
		stop := errors.New("stop event stream")
		err := client.eventStreamClient().Watch(
			watchCtx, func() {
				connection++
				writeEventConnectionStatus(
					client, request, options, connection > 1,
				)
			}, func(event eventstream.Event) error {
				received = true
				if event.Retry > 0 {
					serverDelay = event.Retry
				}
				if event.Type == "backchannel-logout" {
					loggedOut = true
					return stop
				}
				if len(selectedTypes) > 0 && !selectedTypes[event.Type] {
					return nil
				}
				if err := writeWatchedEvent(event, options); err != nil {
					handlerErr = err
					return stop
				}
				if request.Once {
					onceComplete = true
					return stop
				}
				return nil
			},
		)
		switch {
		case handlerErr != nil:
			return handlerErr
		case onceComplete:
			return nil
		case loggedOut:
			return apperror.Wrap(
				apperror.KindAuthentication, "event watch",
				errors.New("the server ended this login session; run ocis auth login again"),
			)
		case errors.Is(err, context.Canceled),
			errors.Is(err, context.DeadlineExceeded):
			return eventWatchContextError(ctx, request, err)
		case errors.Is(err, stop):
			return nil
		}
		if received {
			attempt = 0
		}
		if attempt >= options.Retries {
			if err == nil {
				err = errors.New("server closed the event stream")
			}
			return fmt.Errorf("event stream disconnected: %w", err)
		}
		writeEventReconnectStatus(options, attempt+1, options.Retries)
		client.logger.Debug(
			"reconnecting event stream", "attempt", attempt+2,
		)
		if err := retry.Wait(
			watchCtx, eventRetryWait, attempt, serverDelay,
		); err != nil {
			return eventWatchContextError(ctx, request, err)
		}
		attempt++
	}
}

func eventWatchContextError(
	parent context.Context, request EventWatchRequest, err error,
) error {
	if errors.Is(err, context.DeadlineExceeded) && parent.Err() == nil &&
		request.MaxWait > 0 {
		return fmt.Errorf(
			"no matching event received within %s", request.MaxWait,
		)
	}
	return err
}

func writeEventConnectionStatus(
	client *client,
	request EventWatchRequest,
	options RunOptions,
	reconnected bool,
) {
	if options.OutputMode != appoutput.Human {
		return
	}
	if reconnected {
		_, _ = fmt.Fprintln(options.Err, "Reconnected.")
		return
	}
	host := client.profile.Server
	if serverURL, err := url.Parse(client.profile.Server); err == nil &&
		serverURL.Host != "" {
		host = serverURL.Host
	}
	_, _ = fmt.Fprintf(options.Err, "Connected to %s.\n", host)
	selected := "all events"
	if len(request.Types) > 0 {
		values := append([]string(nil), request.Types...)
		sort.Strings(values)
		selected = strings.Join(values, ", ")
	}
	if request.Once {
		if request.MaxWait > 0 {
			_, _ = fmt.Fprintf(
				options.Err,
				"Waiting up to %s for the first matching event (%s). Press Ctrl-C to stop.\n",
				request.MaxWait, selected,
			)
			return
		}
		_, _ = fmt.Fprintf(
			options.Err,
			"Waiting for the first matching event (%s). Press Ctrl-C to stop.\n",
			selected,
		)
		return
	}
	_, _ = fmt.Fprintf(
		options.Err, "Watching %s. Press Ctrl-C to stop.\n", selected,
	)
}

func writeEventReconnectStatus(options RunOptions, attempt, maximum int) {
	if options.OutputMode == appoutput.Human {
		_, _ = fmt.Fprintf(
			options.Err, "Connection lost. Reconnecting (%d/%d)...\n",
			attempt, maximum,
		)
	}
}

func eventTypeFilter(values []string) (map[string]bool, error) {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, errors.New("--type cannot be empty")
		}
		result[value] = true
	}
	return result, nil
}

func writeWatchedEvent(event eventstream.Event, options RunOptions) error {
	data := any(event.Data)
	var structured any
	if json.Valid([]byte(event.Data)) &&
		json.Unmarshal([]byte(event.Data), &structured) == nil {
		data = structured
	}
	value := watchedEvent{
		Type: event.Type, Data: data, ID: event.ID,
		ReceivedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if options.OutputMode == appoutput.JSONL {
		return (appoutput.Renderer{
			Writer: options.Out, Mode: appoutput.JSONL, Type: "event",
		}).WriteJSONL(value)
	}
	_, err := fmt.Fprintf(
		options.Out, "%s  %s  %s\n",
		time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		eventstream.Description(value.Type), eventHumanDetail(value.Data),
	)
	return err
}

func eventHumanDetail(data any) string {
	fields, ok := data.(map[string]any)
	if !ok {
		return compactEventData(data)
	}
	if subject := eventString(fields, "subject"); subject != "" {
		if message := eventString(fields, "message"); message != "" &&
			message != subject {
			return subject + " - " + message
		}
		return subject
	}
	parts := make([]string, 0, 4)
	for _, field := range []struct {
		key   string
		label string
	}{
		{key: "itemid", label: "item ID"},
		{key: "spaceid", label: "Space ID"},
		{key: "initiatorid", label: "initiator ID"},
		{key: "userid", label: "user ID"},
	} {
		if value := eventString(fields, field.key); value != "" {
			parts = append(parts, field.label+": "+value)
		}
	}
	if affected, ok := fields["affecteduserids"].([]any); ok && len(affected) > 0 {
		values := make([]string, 0, len(affected))
		for _, value := range affected {
			if text, ok := value.(string); ok && text != "" {
				values = append(values, text)
			}
		}
		if len(values) > 0 {
			parts = append(parts, "affected user IDs: "+strings.Join(values, ", "))
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "; ")
	}
	return compactEventData(data)
}

func eventString(fields map[string]any, key string) string {
	value, _ := fields[key].(string)
	return strings.TrimSpace(value)
}

func compactEventData(data any) string {
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Sprint(data)
	}
	return string(payload)
}
