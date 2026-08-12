package eventstream

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mzner/ocis-cli/internal/httpapi"
)

func TestWatchDecodesEventsAndAuthenticates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.URL.Path != endpoint ||
			request.Header.Get("Accept") != "text/event-stream" ||
			request.Header.Get("Authorization") != "Bearer access-token" {
			t.Fatalf("request: %s headers=%v", request.URL.Path, request.Header)
		}
		writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		_, _ = io.WriteString(writer, ": keepalive\r\n")
		_, _ = io.WriteString(writer, "event: file-touched\r\n")
		_, _ = io.WriteString(writer, "id: event-1\r\n")
		_, _ = io.WriteString(writer, "retry: 1500\r\n")
		_, _ = io.WriteString(writer, "data: {\"item\":\r\n")
		_, _ = io.WriteString(writer, "data: \"report.txt\"}\r\n\r\n")
	}))
	defer server.Close()

	client := NewClient(httpapi.Config{
		Server: server.URL, AuthType: "oidc", AccessToken: "access-token",
	}, server.Client())
	var events []Event
	connected := false
	err := client.Watch(context.Background(), func() {
		connected = true
	}, func(event Event) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !connected || len(events) != 1 || events[0].Type != "file-touched" ||
		events[0].ID != "event-1" || events[0].Retry != 1500*time.Millisecond ||
		events[0].Data != "{\"item\":\n\"report.txt\"}" {
		t.Fatalf("events: %#v", events)
	}
}

func TestWatchRejectsHTTPAndContentTypeFailures(t *testing.T) {
	tests := []struct {
		status      int
		contentType string
		want        string
	}{
		{status: http.StatusUnauthorized, want: "401 Unauthorized"},
		{status: http.StatusOK, contentType: "application/json", want: "text/event-stream"},
	}
	for _, test := range tests {
		server := httptest.NewServer(http.HandlerFunc(func(
			writer http.ResponseWriter, _ *http.Request,
		) {
			writer.Header().Set("Content-Type", test.contentType)
			writer.WriteHeader(test.status)
		}))
		client := NewClient(httpapi.Config{Server: server.URL}, server.Client())
		err := client.Watch(
			context.Background(), nil, func(Event) error { return nil },
		)
		server.Close()
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("status=%d contentType=%q error=%v", test.status, test.contentType, err)
		}
	}
}

func TestWatchHonorsContextCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		close(started)
		<-request.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	client := NewClient(httpapi.Config{Server: server.URL}, server.Client())
	go func() {
		done <- client.Watch(ctx, nil, func(Event) error { return nil })
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("error: %v", err)
	}
}

func TestDecodeDefaultsTypeAndPropagatesHandlerError(t *testing.T) {
	response := &http.Response{Body: io.NopCloser(strings.NewReader("data: value\n\n"))}
	want := errors.New("stop")
	err := decode(context.Background(), response, func(event Event) error {
		if event.Type != "message" || event.Data != "value" {
			t.Fatalf("event: %#v", event)
		}
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("error: %v", err)
	}
}

func TestDecodeDiscardsIncompleteEventAtEOF(t *testing.T) {
	response := &http.Response{Body: io.NopCloser(strings.NewReader("data: incomplete\n"))}
	called := false
	if err := decode(context.Background(), response, func(Event) error {
		called = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("incomplete event was dispatched")
	}
}

func TestDecodePreservesReconnectDelayAcrossComments(t *testing.T) {
	input := "retry: 750\n\n: keepalive\n\nevent: ready\ndata: {}\n\n"
	response := &http.Response{Body: io.NopCloser(strings.NewReader(input))}
	var got Event
	if err := decode(context.Background(), response, func(event Event) error {
		got = event
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got.Type != "ready" || got.Retry != 750*time.Millisecond {
		t.Fatalf("event: %#v", got)
	}
}

func TestDecodeRejectsOversizedEvent(t *testing.T) {
	line := "data: " + strings.Repeat("x", maxLineBytes-16) + "\n"
	data := strings.Repeat(line, 5) + "\n"
	response := &http.Response{Body: io.NopCloser(strings.NewReader(data))}
	err := decode(context.Background(), response, func(Event) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("error: %v", err)
	}
}
