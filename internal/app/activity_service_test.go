package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

const appActivityFile = `<?xml version="1.0"?>
<d:multistatus xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns">
  <d:response>
    <d:href>/remote.php/dav/files/alice/report.txt</d:href>
    <d:propstat><d:status>HTTP/1.1 200 OK</d:status><d:prop>
      <d:displayname>report.txt</d:displayname>
      <d:getcontentlength>8</d:getcontentlength><d:resourcetype/>
      <oc:fileid>storage$space!report</oc:fileid>
    </d:prop></d:propstat>
  </d:response>
</d:multistatus>`

const appActivityResponse = `{"value":[{
  "id":"event-1","times":{"recordedTime":"2026-08-11T08:00:00Z"},
  "template":{"message":"{user} added {resource} to {folder}",
    "variables":{"user":{"id":"alice","displayName":"Alice Hansen"},
    "resource":{"id":"file-1","name":"report.txt"},
    "folder":{"id":"folder-1","name":"Reports"}}}
}]}`

func TestActivityUseCases(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch {
		case request.Method == http.MethodGet &&
			request.URL.Path == "/graph/v1.0/me/drives":
			_, _ = io.WriteString(writer, `{"value":[
				{"id":"storage$space-a","name":"Personal","driveType":"personal"},
				{"id":"storage$space-b","name":"Engineering","driveType":"project"},
				{"id":"shares","name":"Shares","driveType":"virtual"}
			]}`)
		case request.Method == "PROPFIND":
			writer.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(writer, appActivityFile)
		case request.Method == http.MethodGet:
			query := request.URL.Query().Get("kql")
			switch {
			case strings.Contains(query, "!report"):
				want := `itemid:"storage$space!report" AND depth:0 AND limit:10 AND sort:asc`
				if query != want {
					t.Fatalf("scoped kql: got %q, want %q", query, want)
				}
				_, _ = io.WriteString(writer, appActivityResponse)
			case strings.Contains(query, "storage$space-a"):
				want := `itemid:"storage$space-a" AND limit:100 AND sort:desc`
				if query != want {
					t.Fatalf("personal kql: got %q, want %q", query, want)
				}
				_, _ = io.WriteString(writer, appActivityResponse)
			case strings.Contains(query, "storage$space-b"):
				want := `itemid:"storage$space-b" AND limit:100 AND sort:desc`
				if query != want {
					t.Fatalf("project kql: got %q, want %q", query, want)
				}
				_, _ = io.WriteString(writer, strings.ReplaceAll(
					strings.ReplaceAll(appActivityResponse, "event-1", "event-2"),
					"2026-08-11T08:00:00Z", "2026-08-11T09:00:00Z",
				))
			default:
				t.Fatalf("unexpected activity kql: %q", query)
			}
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()
	configureSpaceTestProfile(t, server.URL, "")

	var global bytes.Buffer
	if err := RunActivityWithOptions(
		context.Background(), ActivityRequest{Limit: 100, Sort: "desc"},
		"", RunOptions{Out: &global, OutputMode: appoutput.JSON},
	); err != nil || !strings.Contains(global.String(), `"id": "event-1"`) ||
		!strings.Contains(global.String(), `"id": "event-2"`) ||
		strings.Index(global.String(), "event-2") >
			strings.Index(global.String(), "event-1") {
		t.Fatalf("global=%q error=%v", global.String(), err)
	}

	var scoped bytes.Buffer
	if err := RunActivityWithOptions(
		context.Background(), ActivityRequest{
			Path: "/report.txt", Depth: 0, DepthSet: true,
			Limit: 10, Sort: "asc",
		}, "", RunOptions{Out: &scoped},
	); err != nil || !strings.Contains(
		scoped.String(), "Alice Hansen added report.txt to Reports",
	) || !strings.Contains(scoped.String(), "event-1") {
		t.Fatalf("scoped=%q error=%v", scoped.String(), err)
	}
}

func TestActivityValidationFailsBeforeProfileLoad(t *testing.T) {
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "missing", "config.json"))
	for _, request := range []ActivityRequest{
		{Depth: -2, DepthSet: true, Limit: 100, Sort: "desc"},
		{Limit: 0, Sort: "desc"},
		{Limit: 100, Sort: "newest"},
	} {
		err := RunActivityWithOptions(
			context.Background(), request, "", RunOptions{Out: io.Discard},
		)
		if !apperror.IsKind(err, apperror.KindUsage) {
			t.Fatalf("request=%#v error=%v", request, err)
		}
	}
}

func TestActivityPermissionErrorIsActionable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		writer.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(writer, "Forbidden")
	}))
	defer server.Close()
	configureSpaceTestProfile(t, server.URL, "")
	err := RunActivityWithOptions(
		context.Background(), ActivityRequest{Limit: 100, Sort: "desc"},
		"", RunOptions{Out: io.Discard},
	)
	if !apperror.IsKind(err, apperror.KindAuthentication) ||
		!strings.Contains(err.Error(), "current user is not allowed") {
		t.Fatalf("error: %v", err)
	}
}

func TestWriteActivitiesReportsEmptyHistory(t *testing.T) {
	var output bytes.Buffer
	if err := writeActivities(
		nil, RunOptions{Out: &output}.normalized(),
	); err != nil ||
		output.String() != "No activities found\n" {
		t.Fatalf("output=%q error=%v", output.String(), err)
	}
}
