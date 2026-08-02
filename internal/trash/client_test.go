package trash

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mzner/ocis-cli/internal/httpapi"
)

const trashResponse = `<?xml version="1.0"?>
<d:multistatus xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns">
  <d:response>
    <d:href>/dav/spaces/trash-bin/storage%24space/</d:href>
    <d:propstat>
      <d:status>HTTP/1.1 200 OK</d:status>
      <d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop>
    </d:propstat>
  </d:response>
  <d:response>
    <d:href>/dav/spaces/trash-bin/storage%24space/file-key</d:href>
    <d:propstat>
      <d:status>HTTP/1.1 200 OK</d:status>
      <d:prop>
        <d:resourcetype/>
        <d:getcontentlength>12</d:getcontentlength>
        <oc:trashbin-original-filename>report final.txt</oc:trashbin-original-filename>
        <oc:trashbin-original-location>Documents/report final.txt</oc:trashbin-original-location>
        <oc:trashbin-delete-datetime>Mon, 27 Jul 2026 12:00:00 +0000</oc:trashbin-delete-datetime>
        <oc:trashbin-delete-timestamp>1785153600</oc:trashbin-delete-timestamp>
        <oc:spaceid>storage$space</oc:spaceid>
      </d:prop>
    </d:propstat>
  </d:response>
  <d:response>
    <d:href>/dav/spaces/trash-bin/storage%24space/folder-key/</d:href>
    <d:propstat>
      <d:status>HTTP/1.1 200 OK</d:status>
      <d:prop>
        <d:resourcetype><d:collection/></d:resourcetype>
        <oc:size>42</oc:size>
        <oc:trashbin-original-filename>Archive</oc:trashbin-original-filename>
        <oc:trashbin-original-location>Archive</oc:trashbin-original-location>
        <oc:trashbin-delete-datetime>Mon, 27 Jul 2026 13:00:00 +0000</oc:trashbin-delete-datetime>
        <oc:trashbin-delete-timestamp>1785157200</oc:trashbin-delete-timestamp>
        <oc:spaceid>storage$space</oc:spaceid>
      </d:prop>
    </d:propstat>
  </d:response>
</d:multistatus>`

func TestListTrashItems(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.Method != "PROPFIND" ||
			request.URL.Path != "/dav/spaces/trash-bin/storage$space" ||
			request.Header.Get("Depth") != "1" {
			t.Fatalf(
				"request: %s %s depth=%q",
				request.Method, request.URL.Path, request.Header.Get("Depth"),
			)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), "trashbin-original-location") {
			t.Fatalf("body: %s", body)
		}
		writer.WriteHeader(http.StatusMultiStatus)
		_, _ = io.WriteString(writer, trashResponse)
	}))
	defer server.Close()
	client := testClient(server, "storage$space")
	items, err := client.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != "folder-key" ||
		items[0].Type != "directory" || items[0].Size != 42 ||
		items[1].OriginalPath != "/Documents/report final.txt" ||
		items[1].Size != 12 {
		t.Fatalf("items: %#v", items)
	}
}

func TestTrashMutations(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		requests = append(requests, request.Method+" "+request.URL.Path)
		switch request.Method {
		case "MOVE":
			if request.Header.Get("Overwrite") != "T" ||
				request.Header.Get("Destination") !=
					serverURL(request)+
						"/dav/spaces/storage$space/Documents/report%20final.txt" {
				t.Fatalf("restore headers: %#v", request.Header)
			}
			writer.WriteHeader(http.StatusCreated)
		case http.MethodDelete:
			writer.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("method: %s", request.Method)
		}
	}))
	defer server.Close()
	client := testClient(server, "storage$space")
	if err := client.Restore(context.Background(), Item{
		ID: "file/key", OriginalPath: "/Documents/report final.txt",
	}, true); err != nil {
		t.Fatal(err)
	}
	if err := client.Remove(context.Background(), "file/key"); err != nil {
		t.Fatal(err)
	}
	if err := client.Empty(context.Background()); err != nil {
		t.Fatal(err)
	}
	expected := []string{
		"MOVE /dav/spaces/trash-bin/storage$space/file/key",
		"DELETE /dav/spaces/trash-bin/storage$space/file/key",
		"DELETE /dav/spaces/trash-bin/storage$space",
	}
	if strings.Join(requests, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("requests: %#v", requests)
	}
}

func TestPersonalTrashRoot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.URL.Path != "/remote.php/dav/trash-bin/alice" {
			t.Fatalf("path: %s", request.URL.Path)
		}
		writer.WriteHeader(http.StatusMultiStatus)
		_, _ = io.WriteString(writer, `<d:multistatus xmlns:d="DAV:"/>`)
	}))
	defer server.Close()
	items, err := testClient(server, "").List(context.Background())
	if err != nil || len(items) != 0 {
		t.Fatalf("items=%#v err=%v", items, err)
	}
}

func TestTrashErrorsAreTypedAndInputIsValidated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		writer.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	client := testClient(server, "storage$space")
	_, err := client.List(context.Background())
	status, ok := err.(interface{ HTTPStatusCode() int })
	if !ok || status.HTTPStatusCode() != http.StatusForbidden {
		t.Fatalf("error: %v", err)
	}
	if err := client.Restore(context.Background(), Item{}, false); err == nil {
		t.Fatal("empty restore item was accepted")
	}
	if err := client.Remove(context.Background(), " "); err == nil {
		t.Fatal("empty remove ID was accepted")
	}
}

func testClient(server *httptest.Server, spaceID string) *Client {
	apiConfig := httpapi.Config{Server: server.URL}
	return NewClient(Config{
		API: apiConfig, Server: server.URL, Username: "alice", SpaceID: spaceID,
	}, server.Client())
}

func serverURL(request *http.Request) string {
	return "http://" + request.Host
}
