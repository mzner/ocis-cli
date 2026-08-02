package webdav

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPropertyLifecycle(t *testing.T) {
	const namespace = "https://example.test/metadata"
	property := PropertyName{Namespace: namespace, Name: "review-status"}
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		requests = append(requests, request.Method+" "+string(body))
		writer.WriteHeader(http.StatusMultiStatus)
		switch request.Method {
		case "PROPFIND":
			_, _ = io.WriteString(writer,
				`<d:multistatus xmlns:d="DAV:" xmlns:x="`+namespace+`">`+
					`<d:response><d:propstat><d:prop>`+
					`<x:review-status>ready &amp; reviewed</x:review-status>`+
					`</d:prop><d:status>HTTP/1.1 200 OK</d:status>`+
					`</d:propstat></d:response></d:multistatus>`,
			)
		case "PROPPATCH":
			status := "HTTP/1.1 200 OK"
			if strings.Contains(string(body), "remove") {
				status = "HTTP/1.1 204 No Content"
			}
			_, _ = io.WriteString(writer,
				`<d:multistatus xmlns:d="DAV:" xmlns:x="`+namespace+`">`+
					`<d:response><d:propstat><d:prop>`+
					`<x:review-status/>`+
					`</d:prop><d:status>`+status+`</d:status>`+
					`</d:propstat></d:response></d:multistatus>`,
			)
		default:
			t.Fatalf("method: %s", request.Method)
		}
	}))
	defer server.Close()

	client := NewClient(
		Config{Server: server.URL, Username: "alice"},
		server.Client(),
	)
	value, err := client.GetProperty(
		context.Background(), "/report.txt", property,
	)
	if err != nil || value.Value != "ready & reviewed" {
		t.Fatalf("get: %#v, %v", value, err)
	}
	if err := client.SetProperty(
		context.Background(), "/report.txt", property, "draft <new>",
	); err != nil {
		t.Fatal(err)
	}
	if err := client.RemoveProperty(
		context.Background(), "/report.txt", property,
	); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 3 ||
		!strings.Contains(requests[0], "review-status") ||
		!strings.Contains(requests[1], "draft &lt;new&gt;") ||
		!strings.Contains(requests[2], "remove") {
		t.Fatalf("requests: %#v", requests)
	}
}

func TestGetPropertyReportsUnsupportedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		writer.WriteHeader(http.StatusMultiStatus)
		_, _ = io.WriteString(writer,
			`<d:multistatus xmlns:d="DAV:" xmlns:x="https://example.test">`+
				`<d:response><d:propstat><d:prop><x:rating/>`+
				`</d:prop><d:status>HTTP/1.1 404 Not Found</d:status>`+
				`</d:propstat></d:response></d:multistatus>`,
		)
	}))
	defer server.Close()
	client := NewClient(Config{Server: server.URL}, server.Client())
	_, err := client.GetProperty(
		context.Background(), "/report.txt",
		PropertyName{Namespace: "https://example.test", Name: "rating"},
	)
	if !errors.Is(err, ErrPropertyNotFound) {
		t.Fatalf("error: %v", err)
	}
}

func TestPropertyUpdateRejectsFailedPropstat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		writer.WriteHeader(http.StatusMultiStatus)
		_, _ = io.WriteString(writer,
			`<d:multistatus xmlns:d="DAV:" xmlns:x="https://example.test">`+
				`<d:response><d:propstat><d:prop><x:rating/>`+
				`</d:prop><d:status>HTTP/1.1 403 Forbidden</d:status>`+
				`</d:propstat></d:response></d:multistatus>`,
		)
	}))
	defer server.Close()
	client := NewClient(Config{Server: server.URL}, server.Client())
	err := client.SetProperty(
		context.Background(), "/report.txt",
		PropertyName{Namespace: "https://example.test", Name: "rating"},
		"five",
	)
	if err == nil || !strings.Contains(err.Error(), "403 Forbidden") {
		t.Fatalf("error: %v", err)
	}
}
