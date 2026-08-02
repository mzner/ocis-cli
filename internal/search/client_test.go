package search

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mzner/ocis-cli/internal/httpapi"
)

func TestSearchRequestAndResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.Method != "REPORT" ||
			request.URL.Path != "/remote.php/dav/spaces" {
			t.Fatalf("request: %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Content-Type") != "application/xml; charset=utf-8" {
			t.Fatalf("content type: %s", request.Header.Get("Content-Type"))
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		if !strings.Contains(text, "name:*report&amp;plan*") ||
			!strings.Contains(text, ">25<") {
			t.Fatalf("body: %s", text)
		}
		writer.Header().Set("Content-Range", "rows 0-0/12")
		writer.WriteHeader(http.StatusMultiStatus)
		_, _ = io.WriteString(writer, searchResponse)
	}))
	defer server.Close()

	client := NewClient(httpapi.Config{Server: server.URL}, server.Client())
	response, err := client.Search(context.Background(), Request{
		Pattern: "name:*report&plan*", Limit: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Total != 12 || len(response.Items) != 1 {
		t.Fatalf("response: %#v", response)
	}
	item := response.Items[0]
	if item.Path != "/Reports/Q1 plan.pdf" || item.SpaceID != "storage$space" ||
		item.Type != "file" || item.Size != 42 || item.Score != 1.25 {
		t.Fatalf("item: %#v", item)
	}
}

func TestSearchReturnsTypedHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(writer, "<d:error xmlns:d=\"DAV:\"><d:message>bad query</d:message></d:error>")
	}))
	defer server.Close()
	client := NewClient(httpapi.Config{Server: server.URL}, server.Client())
	_, err := client.Search(context.Background(), Request{Pattern: "AND", Limit: 10})
	status, ok := err.(interface{ HTTPStatusCode() int })
	if !ok || status.HTTPStatusCode() != http.StatusBadRequest ||
		!strings.Contains(err.Error(), "bad query") {
		t.Fatalf("error: %v", err)
	}
}

const searchResponse = `<?xml version="1.0"?>
<d:multistatus xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns">
  <d:response>
    <d:href>/remote.php/dav/spaces/storage$space/Reports/Q1%20plan.pdf</d:href>
    <d:propstat>
      <d:prop>
        <oc:fileid>storage$space!file</oc:fileid>
        <oc:file-parent>storage$space!parent</oc:file-parent>
        <oc:name>Q1 plan.pdf</oc:name>
        <d:getlastmodified>Mon, 27 Jul 2026 08:00:00 GMT</d:getlastmodified>
        <d:getcontenttype>application/pdf</d:getcontenttype>
        <d:getcontentlength>42</d:getcontentlength>
        <d:resourcetype/>
        <oc:permissions>R</oc:permissions>
        <oc:highlights>Q1 plan</oc:highlights>
        <oc:spaceid>storage$space</oc:spaceid>
        <oc:tags>quarterly</oc:tags>
        <d:getetag>&quot;etag&quot;</d:getetag>
        <oc:score>1.25</oc:score>
      </d:prop>
      <d:status>HTTP/1.1 200 OK</d:status>
    </d:propstat>
  </d:response>
</d:multistatus>`
