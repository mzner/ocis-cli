package activities

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mzner/ocis-cli/internal/httpapi"
)

func TestListActivities(t *testing.T) {
	depth := 1
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.Method != http.MethodGet || request.URL.Path != endpoint {
			t.Fatalf("request: %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("authorization: %q", request.Header.Get("Authorization"))
		}
		if request.Header.Get("Accept") != "application/json" {
			t.Fatalf("accept: %q", request.Header.Get("Accept"))
		}
		wantQuery := `itemid:"storage$space!report" AND depth:1 AND limit:50 AND sort:desc`
		if got := request.URL.Query().Get("kql"); got != wantQuery {
			t.Fatalf("kql: got %q, want %q", got, wantQuery)
		}
		_, _ = io.WriteString(writer, `{"value":[{
			"id":"event-1","times":{"recordedTime":"2026-08-11T08:00:00Z"},
			"template":{"message":"{user} added {resource} to {folder}",
			"variables":{"user":{"id":"alice","displayName":"Alice"},
			"resource":{"id":"file-1","name":"report.txt"},
			"folder":{"id":"folder-1","name":"Reports"}}}
		}]}`)
	}))
	defer server.Close()

	client := NewClient(httpapi.Config{
		Server: server.URL, AuthType: "oidc", AccessToken: "token",
	}, server.Client())
	values, err := client.List(context.Background(), ListRequest{
		ItemID: "storage$space!report", Depth: &depth, Limit: 50, Sort: "DESC",
	})
	if err != nil || len(values) != 1 || values[0].ID != "event-1" ||
		values[0].Template.Variables["resource"] == nil {
		t.Fatalf("activities: %#v, %v", values, err)
	}
}

func TestListActivitiesAcceptsNullCollection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		_, _ = io.WriteString(writer, `{"value":null}`)
	}))
	defer server.Close()
	client := NewClient(httpapi.Config{Server: server.URL}, server.Client())
	values, err := client.List(context.Background(), ListRequest{})
	if err != nil || values == nil || len(values) != 0 {
		t.Fatalf("activities: %#v, %v", values, err)
	}
}

func TestListActivitiesValidatesFilters(t *testing.T) {
	invalidDepth := -2
	client := NewClient(httpapi.Config{Server: "http://127.0.0.1:1"}, nil)
	for _, request := range []ListRequest{
		{ItemID: "invalid\"id"},
		{Depth: &invalidDepth},
		{Limit: 1001},
		{Sort: "newest"},
	} {
		if _, err := client.List(context.Background(), request); err == nil {
			t.Fatalf("accepted invalid request: %#v", request)
		}
	}
}

func TestListActivitiesReturnsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		writer.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(writer, "activity access denied")
	}))
	defer server.Close()
	client := NewClient(httpapi.Config{Server: server.URL}, server.Client())
	_, err := client.List(context.Background(), ListRequest{Limit: 100})
	if err == nil || !strings.Contains(err.Error(), "activity access denied") {
		t.Fatalf("error: %v", err)
	}
}
