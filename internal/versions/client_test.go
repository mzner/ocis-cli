package versions

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mzner/ocis-cli/internal/httpapi"
)

const versionsResponse = `<?xml version="1.0"?>
<d:multistatus xmlns:d="DAV:">
  <d:response>
    <d:href>/remote.php/dav/meta/storage%24space%21file/v/</d:href>
    <d:propstat><d:status>HTTP/1.1 200 OK</d:status>
      <d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop>
    </d:propstat>
  </d:response>
  <d:response>
    <d:href>/remote.php/dav/meta/storage%24space%21file/v/older</d:href>
    <d:propstat><d:status>HTTP/1.1 200 OK</d:status>
      <d:prop><d:resourcetype/><d:getcontentlength>5</d:getcontentlength>
        <d:getlastmodified>Mon, 27 Jul 2026 11:00:00 GMT</d:getlastmodified>
        <d:getetag>"older-etag"</d:getetag></d:prop>
    </d:propstat>
  </d:response>
  <d:response>
    <d:href>/remote.php/dav/meta/storage%24space%21file/v/newer</d:href>
    <d:propstat><d:status>HTTP/1.1 200 OK</d:status>
      <d:prop><d:resourcetype/><d:getcontentlength>7</d:getcontentlength>
        <d:getlastmodified>Mon, 27 Jul 2026 12:00:00 GMT</d:getlastmodified>
        <d:getetag>"newer-etag"</d:getetag></d:prop>
    </d:propstat>
  </d:response>
</d:multistatus>`

func TestVersionUseCases(t *testing.T) {
	var restored bool
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		expectedRoot := "/remote.php/dav/meta/storage$space!file/v"
		switch {
		case request.Method == "PROPFIND" && request.URL.Path == expectedRoot:
			if request.Header.Get("Depth") != "1" {
				t.Fatalf("depth: %q", request.Header.Get("Depth"))
			}
			writer.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(writer, versionsResponse)
		case request.Method == http.MethodGet &&
			request.URL.Path == expectedRoot+"/newer":
			writer.Header().Set("ETag", `"newer-etag"`)
			writer.Header().Set("Content-Length", "7")
			_, _ = io.WriteString(writer, "content")
		case request.Method == "COPY" &&
			request.URL.Path == expectedRoot+"/newer":
			restored = true
			writer.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()
	client := NewClient(
		httpapi.Config{Server: server.URL}, server.Client(),
	)
	available, err := client.List(context.Background(), "storage$space!file")
	if err != nil {
		t.Fatal(err)
	}
	if len(available) != 2 || available[0].ID != "newer" ||
		available[0].Size != 7 || available[1].ID != "older" {
		t.Fatalf("versions: %#v", available)
	}
	content, err := client.Open(
		context.Background(), "storage$space!file", "newer",
	)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(content.Body)
	_ = content.Body.Close()
	if err != nil || string(data) != "content" ||
		content.Size != 7 || content.ETag != `"newer-etag"` {
		t.Fatalf("content=%q metadata=%#v err=%v", data, content, err)
	}
	if err := client.Restore(
		context.Background(), "storage$space!file", "newer",
	); err != nil {
		t.Fatal(err)
	}
	if !restored {
		t.Fatal("version was not restored")
	}
}

func TestVersionsValidateInputAndReturnTypedErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		writer.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	client := NewClient(
		httpapi.Config{Server: server.URL}, server.Client(),
	)
	if _, err := client.List(context.Background(), ""); err == nil {
		t.Fatal("empty resource ID was accepted")
	}
	if _, err := client.Open(context.Background(), "resource", ""); err == nil {
		t.Fatal("empty version ID was accepted")
	}
	if err := client.Restore(
		context.Background(), "", "version",
	); err == nil {
		t.Fatal("empty resource ID was accepted for restore")
	}
	_, err := client.List(context.Background(), "resource")
	status, ok := err.(interface{ HTTPStatusCode() int })
	if !ok || status.HTTPStatusCode() != http.StatusForbidden {
		t.Fatalf("error: %v", err)
	}
}

func TestDecodeVersionsRejectsInvalidResponses(t *testing.T) {
	outside := strings.Replace(
		versionsResponse,
		"/remote.php/dav/meta/storage%24space%21file/v/older",
		"/outside/older", 1,
	)
	if _, err := DecodeList(
		[]byte(outside), "/remote.php/dav/meta/storage$space!file/v",
	); err == nil || !strings.Contains(err.Error(), "outside expected root") {
		t.Fatalf("outside href: %v", err)
	}
	invalidSize := strings.Replace(versionsResponse, ">5<", ">invalid<", 1)
	if _, err := DecodeList(
		[]byte(invalidSize), "/remote.php/dav/meta/storage$space!file/v",
	); err == nil || !strings.Contains(err.Error(), "version size") {
		t.Fatalf("invalid size: %v", err)
	}
}
