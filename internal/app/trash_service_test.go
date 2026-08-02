package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

const appTrashResponse = `<?xml version="1.0"?>
<d:multistatus xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns">
  <d:response>
    <d:href>/dav/spaces/trash-bin/space-id/</d:href>
    <d:propstat><d:status>HTTP/1.1 200 OK</d:status>
      <d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop>
    </d:propstat>
  </d:response>
  <d:response>
    <d:href>/dav/spaces/trash-bin/space-id/item-key</d:href>
    <d:propstat><d:status>HTTP/1.1 200 OK</d:status>
      <d:prop>
        <d:resourcetype/><d:getcontentlength>12</d:getcontentlength>
        <oc:trashbin-original-filename>report.txt</oc:trashbin-original-filename>
        <oc:trashbin-original-location>Documents/report.txt</oc:trashbin-original-location>
        <oc:trashbin-delete-datetime>Mon, 27 Jul 2026 12:00:00 +0000</oc:trashbin-delete-datetime>
        <oc:trashbin-delete-timestamp>1785153600</oc:trashbin-delete-timestamp>
        <oc:spaceid>space-id</oc:spaceid>
      </d:prop>
    </d:propstat>
  </d:response>
</d:multistatus>`

type trashServerState struct {
	restored  bool
	overwrite string
	removed   bool
	emptied   bool
}

func TestTrashUseCases(t *testing.T) {
	state := &trashServerState{}
	server := newTrashAppServer(t, state)
	defer server.Close()
	configureSpaceTestProfile(t, server.URL, "space-id")

	var listed bytes.Buffer
	if err := RunTrashWithOptions(
		context.Background(), TrashRequest{Operation: TrashList}, "",
		RunOptions{Out: &listed, OutputMode: appoutput.JSON},
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listed.String(), `"id": "item-key"`) ||
		!strings.Contains(listed.String(), `"originalPath": "/Documents/report.txt"`) {
		t.Fatalf("list: %s", listed.String())
	}

	var dryRun bytes.Buffer
	if err := RunTrashWithOptions(
		context.Background(),
		TrashRequest{
			Operation: TrashRestore, ItemID: "item-key", DryRun: true,
		},
		"", RunOptions{Out: &dryRun},
	); err != nil {
		t.Fatal(err)
	}
	if state.restored ||
		!strings.Contains(dryRun.String(), "Would restore item-key") {
		t.Fatalf("restored=%t output=%s", state.restored, dryRun.String())
	}

	if err := RunTrashWithOptions(
		context.Background(),
		TrashRequest{
			Operation: TrashRestore, ItemID: "item-key", Overwrite: true,
		},
		"", RunOptions{Out: io.Discard},
	); err != nil {
		t.Fatal(err)
	}
	if !state.restored || state.overwrite != "T" {
		t.Fatalf("restored=%t overwrite=%q", state.restored, state.overwrite)
	}

	if err := RunTrashWithOptions(
		context.Background(),
		TrashRequest{
			Operation: TrashRemove, ItemID: "item-key", Permanent: true,
		},
		"", RunOptions{Out: io.Discard},
	); err != nil {
		t.Fatal(err)
	}
	if !state.removed {
		t.Fatal("trash item was not permanently removed")
	}

	if err := RunTrashWithOptions(
		context.Background(),
		TrashRequest{Operation: TrashEmpty, Permanent: true},
		"", RunOptions{Out: io.Discard},
	); err != nil {
		t.Fatal(err)
	}
	if !state.emptied {
		t.Fatal("trash was not emptied")
	}
}

func TestTrashPermanentOperationsFailClosed(t *testing.T) {
	for _, request := range []TrashRequest{
		{Operation: TrashRemove, ItemID: "item-key", DryRun: true},
		{Operation: TrashEmpty, DryRun: true},
	} {
		err := RunTrashWithOptions(
			context.Background(), request, "",
			RunOptions{Out: io.Discard},
		)
		if !apperror.IsKind(err, apperror.KindUsage) ||
			!strings.Contains(err.Error(), "explicit confirmation") {
			t.Fatalf("%s: %v", request.Operation, err)
		}
	}
}

func TestTrashUnknownItemIsNotFound(t *testing.T) {
	state := &trashServerState{}
	server := newTrashAppServer(t, state)
	defer server.Close()
	configureSpaceTestProfile(t, server.URL, "space-id")
	err := RunTrashWithOptions(
		context.Background(),
		TrashRequest{
			Operation: TrashRestore, ItemID: "missing", DryRun: true,
		},
		"", RunOptions{Out: io.Discard},
	)
	if !apperror.IsKind(err, apperror.KindNotFound) ||
		!strings.Contains(err.Error(), "ocis trash list") {
		t.Fatalf("error: %v", err)
	}
}

func newTrashAppServer(
	t *testing.T, state *trashServerState,
) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch {
		case request.Method == http.MethodGet &&
			request.URL.Path == "/graph/v1.0/me/drives":
			_, _ = io.WriteString(writer, `{"value":[{
				"id":"space-id","name":"Engineering","driveType":"project"
			}]}`)
		case request.Method == "PROPFIND" &&
			request.URL.Path == "/dav/spaces/trash-bin/space-id":
			writer.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(writer, appTrashResponse)
		case request.Method == "MOVE" &&
			request.URL.Path == "/dav/spaces/trash-bin/space-id/item-key":
			state.restored = true
			state.overwrite = request.Header.Get("Overwrite")
			if !strings.HasSuffix(
				request.Header.Get("Destination"),
				"/dav/spaces/space-id/Documents/report.txt",
			) {
				t.Fatalf("destination: %q", request.Header.Get("Destination"))
			}
			writer.WriteHeader(http.StatusCreated)
		case request.Method == http.MethodDelete &&
			request.URL.Path == "/dav/spaces/trash-bin/space-id/item-key":
			state.removed = true
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodDelete &&
			request.URL.Path == "/dav/spaces/trash-bin/space-id":
			state.emptied = true
			writer.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
	}))
}
