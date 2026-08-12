package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mzner/ocis-cli/internal/apperror"
	"github.com/mzner/ocis-cli/internal/eventstream"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

func TestEventWatchOnceFiltersAndWritesJSONL(t *testing.T) {
	var connections atomic.Int32
	server := newEventTestServer(t, true, func(writer http.ResponseWriter) {
		connections.Add(1)
		_, _ = io.WriteString(writer, "event: file-touched\n")
		_, _ = io.WriteString(writer, "data: {\"path\":\"/ignored.txt\"}\n\n")
		_, _ = io.WriteString(writer, "event: share-created\n")
		_, _ = io.WriteString(writer, "id: event-2\n")
		_, _ = io.WriteString(writer, "data: {\"path\":\"/report.txt\"}\n\n")
	})
	defer server.Close()
	configureSpaceTestProfile(t, server.URL, "")

	var output bytes.Buffer
	var diagnostics bytes.Buffer
	err := RunEventWatchWithOptions(
		context.Background(), EventWatchRequest{
			Types: []string{"share-created"}, Once: true,
		}, "", RunOptions{
			Out: &output, Err: &diagnostics, OutputMode: appoutput.JSONL,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	var envelope appoutput.Envelope
	if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Type != "event" ||
		!strings.Contains(output.String(), `"type":"share-created"`) ||
		!strings.Contains(output.String(), `"path":"/report.txt"`) ||
		strings.Contains(output.String(), "ignored.txt") || connections.Load() != 1 {
		t.Fatalf("connections=%d output=%q", connections.Load(), output.String())
	}
	if diagnostics.Len() != 0 {
		t.Fatalf("JSONL diagnostics: %q", diagnostics.String())
	}
}

func TestEventWatchReconnects(t *testing.T) {
	var connections atomic.Int32
	server := newEventTestServer(t, true, func(writer http.ResponseWriter) {
		if connections.Add(1) == 1 {
			return
		}
		_, _ = io.WriteString(writer, "event: folder-created\n")
		_, _ = io.WriteString(writer, "data: {\"name\":\"Reports\"}\n\n")
	})
	defer server.Close()
	configureSpaceTestProfile(t, server.URL, "")

	var output bytes.Buffer
	var diagnostics bytes.Buffer
	err := RunEventWatchWithOptions(
		context.Background(), EventWatchRequest{Once: true}, "",
		RunOptions{Out: &output, Err: &diagnostics, Retries: 1},
	)
	if err != nil || connections.Load() != 2 ||
		!strings.Contains(output.String(), "Folder created") ||
		!strings.Contains(diagnostics.String(), "Connection lost") ||
		!strings.Contains(diagnostics.String(), "Reconnected") {
		t.Fatalf(
			"connections=%d output=%q diagnostics=%q error=%v",
			connections.Load(), output.String(), diagnostics.String(), err,
		)
	}
}

func TestEventWatchMaxWaitBoundsOnce(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch request.URL.Path {
		case "/ocs/v2.php/cloud/capabilities":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"ocs":{"meta":{
				"status":"ok","statuscode":200,"message":"OK"
			},"data":{"capabilities":{"core":{"support-sse":true}}}}}`)
		case "/ocs/v2.php/apps/notifications/api/v1/notifications/sse":
			writer.Header().Set("Content-Type", "text/event-stream")
			writer.WriteHeader(http.StatusOK)
			writer.(http.Flusher).Flush()
			<-request.Context().Done()
		default:
			t.Fatalf("unexpected request: %s", request.URL.Path)
		}
	}))
	defer server.Close()
	configureSpaceTestProfile(t, server.URL, "")
	err := RunEventWatchWithOptions(
		context.Background(), EventWatchRequest{
			Once: true, MaxWait: 25 * time.Millisecond,
		}, "", RunOptions{Out: io.Discard, Err: io.Discard},
	)
	if err == nil || !strings.Contains(err.Error(), "within 25ms") {
		t.Fatalf("error: %v", err)
	}
}

func TestEventWatchValidatesOutputAndCapability(t *testing.T) {
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "missing", "config.json"))
	err := RunEventWatchWithOptions(
		context.Background(), EventWatchRequest{}, "",
		RunOptions{Out: io.Discard, OutputMode: appoutput.JSON},
	)
	if !apperror.IsKind(err, apperror.KindUsage) ||
		!strings.Contains(err.Error(), "--jsonl") {
		t.Fatalf("JSON error: %v", err)
	}
	for _, request := range []EventWatchRequest{
		{Once: true, MaxWait: -time.Second},
		{MaxWait: time.Second},
	} {
		err = RunEventWatchWithOptions(
			context.Background(), request, "", RunOptions{Out: io.Discard},
		)
		if !apperror.IsKind(err, apperror.KindUsage) {
			t.Fatalf("request=%#v error=%v", request, err)
		}
	}

	server := newEventTestServer(t, false, func(http.ResponseWriter) {
		t.Fatal("unsupported server must not receive an SSE request")
	})
	defer server.Close()
	configureSpaceTestProfile(t, server.URL, "")
	err = RunEventWatchWithOptions(
		context.Background(), EventWatchRequest{}, "", RunOptions{Out: io.Discard},
	)
	if err == nil || !strings.Contains(err.Error(), "core.support-sse") {
		t.Fatalf("capability error: %v", err)
	}
}

func TestEventWatchTreatsBackchannelLogoutAsAuthenticationFailure(t *testing.T) {
	server := newEventTestServer(t, true, func(writer http.ResponseWriter) {
		_, _ = io.WriteString(writer, "event: backchannel-logout\n")
		_, _ = io.WriteString(writer, "data: {}\n\n")
	})
	defer server.Close()
	configureSpaceTestProfile(t, server.URL, "")
	err := RunEventWatchWithOptions(
		context.Background(), EventWatchRequest{}, "", RunOptions{Out: io.Discard},
	)
	if !apperror.IsKind(err, apperror.KindAuthentication) ||
		!strings.Contains(err.Error(), "login session") {
		t.Fatalf("error: %v", err)
	}
}

func TestEventTypesOutput(t *testing.T) {
	var output bytes.Buffer
	if err := RunEventTypesWithOptions(RunOptions{Out: &output}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"userlog-notification", "file-touched", "backchannel-logout",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("missing %q in %q", expected, output.String())
		}
	}
}

func TestWatchedEventHumanOutputUsesReadableServerFields(t *testing.T) {
	tests := []struct {
		event eventstream.Event
		want  []string
	}{
		{
			event: eventstream.Event{
				Type: "file-touched",
				Data: `{"itemid":"storage$space!file","spaceid":"storage$space"}`,
			},
			want: []string{
				"File changed", "item ID: storage$space!file",
				"Space ID: storage$space",
			},
		},
		{
			event: eventstream.Event{
				Type: "userlog-notification",
				Data: `{"subject":"Resource shared","message":"Alice shared report.pdf with you"}`,
			},
			want: []string{
				"Unread notification received", "Resource shared",
				"Alice shared report.pdf with you",
			},
		},
	}
	for _, test := range tests {
		var output bytes.Buffer
		if err := writeWatchedEvent(
			test.event, RunOptions{Out: &output}.normalized(),
		); err != nil {
			t.Fatal(err)
		}
		for _, expected := range test.want {
			if !strings.Contains(output.String(), expected) {
				t.Fatalf("missing %q in %q", expected, output.String())
			}
		}
	}
}

func newEventTestServer(
	t *testing.T, supportSSE bool, stream func(http.ResponseWriter),
) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch request.URL.Path {
		case "/ocs/v2.php/cloud/capabilities":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"ocs":{"meta":{
				"status":"ok","statuscode":200,"message":"OK"
			},"data":{"capabilities":{"core":{"support-sse":`+
				strconv.FormatBool(supportSSE)+`}}}}}`)
		case "/ocs/v2.php/apps/notifications/api/v1/notifications/sse":
			writer.Header().Set("Content-Type", "text/event-stream")
			stream(writer)
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
	}))
}
