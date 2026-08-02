package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

const appVersionFile = `<?xml version="1.0"?>
<d:multistatus xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns">
  <d:response>
    <d:href>/remote.php/dav/files/alice/report.txt</d:href>
    <d:propstat><d:status>HTTP/1.1 200 OK</d:status><d:prop>
      <d:displayname>report.txt</d:displayname>
      <d:getcontentlength>8</d:getcontentlength><d:resourcetype/>
      <oc:fileid>storage$space!file</oc:fileid>
    </d:prop></d:propstat>
  </d:response>
</d:multistatus>`

const appVersions = `<?xml version="1.0"?>
<d:multistatus xmlns:d="DAV:">
  <d:response>
    <d:href>/remote.php/dav/meta/storage%24space%21file/v/</d:href>
    <d:propstat><d:status>HTTP/1.1 200 OK</d:status>
      <d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop>
    </d:propstat>
  </d:response>
  <d:response>
    <d:href>/remote.php/dav/meta/storage%24space%21file/v/version-1</d:href>
    <d:propstat><d:status>HTTP/1.1 200 OK</d:status><d:prop>
      <d:resourcetype/><d:getcontentlength>7</d:getcontentlength>
      <d:getlastmodified>Mon, 27 Jul 2026 12:00:00 GMT</d:getlastmodified>
      <d:getetag>"version-etag"</d:getetag>
    </d:prop></d:propstat>
  </d:response>
</d:multistatus>`

type versionServerState struct {
	downloaded bool
	restored   bool
}

func TestVersionUseCases(t *testing.T) {
	state := &versionServerState{}
	server := newVersionAppServer(t, state)
	defer server.Close()
	configureSpaceTestProfile(t, server.URL, "")

	var listed bytes.Buffer
	if err := RunVersionWithOptions(
		context.Background(),
		VersionRequest{Operation: VersionList, Path: "/report.txt"},
		"", RunOptions{Out: &listed, OutputMode: appoutput.JSON},
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listed.String(), `"id": "version-1"`) ||
		!strings.Contains(listed.String(), `"size": 7`) {
		t.Fatalf("list: %s", listed.String())
	}

	var info bytes.Buffer
	if err := RunVersionWithOptions(
		context.Background(),
		VersionRequest{
			Operation: VersionInfo, Path: "/report.txt",
			VersionID: "version-1",
		},
		"", RunOptions{Out: &info},
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(info.String(), "Version ID: version-1") {
		t.Fatalf("info: %s", info.String())
	}

	destination := filepath.Join(t.TempDir(), "old-report.txt")
	if err := RunVersionWithOptions(
		context.Background(),
		VersionRequest{
			Operation: VersionDownload, Path: "/report.txt",
			VersionID: "version-1", Destination: destination, Verify: true,
		},
		"", RunOptions{Out: io.Discard},
	); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "content" || !state.downloaded {
		t.Fatalf(
			"download=%q downloaded=%t err=%v", data, state.downloaded, err,
		)
	}

	var dryRun bytes.Buffer
	if err := RunVersionWithOptions(
		context.Background(),
		VersionRequest{
			Operation: VersionRestore, Path: "/report.txt",
			VersionID: "version-1", Confirmed: true, DryRun: true,
		},
		"", RunOptions{Out: &dryRun},
	); err != nil {
		t.Fatal(err)
	}
	if state.restored || !strings.Contains(dryRun.String(), "Would restore") {
		t.Fatalf("restored=%t output=%s", state.restored, dryRun.String())
	}

	if err := RunVersionWithOptions(
		context.Background(),
		VersionRequest{
			Operation: VersionRestore, Path: "/report.txt",
			VersionID: "version-1", Confirmed: true,
		},
		"", RunOptions{Out: io.Discard},
	); err != nil {
		t.Fatal(err)
	}
	if !state.restored {
		t.Fatal("version was not restored")
	}
}

func TestVersionDownloadNoClobberAvoidsContentRequest(t *testing.T) {
	state := &versionServerState{}
	server := newVersionAppServer(t, state)
	defer server.Close()
	configureSpaceTestProfile(t, server.URL, "")
	destination := filepath.Join(t.TempDir(), "existing.txt")
	if err := os.WriteFile(destination, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	err := RunVersionWithOptions(
		context.Background(),
		VersionRequest{
			Operation: VersionDownload, Path: "/report.txt",
			VersionID: "version-1", Destination: destination,
			NoClobber: true, Verify: true,
		},
		"", RunOptions{Out: io.Discard},
	)
	if !apperror.IsKind(err, apperror.KindConflict) || state.downloaded {
		t.Fatalf("error=%v downloaded=%t", err, state.downloaded)
	}
	data, readErr := os.ReadFile(destination)
	if readErr != nil || string(data) != "keep" {
		t.Fatalf("destination=%q err=%v", data, readErr)
	}
}

func TestVersionRestoreFailsClosedAndUnknownVersionIsNotFound(t *testing.T) {
	err := RunVersionWithOptions(
		context.Background(),
		VersionRequest{
			Operation: VersionRestore, Path: "/report.txt",
			VersionID: "version-1", DryRun: true,
		},
		"", RunOptions{Out: io.Discard},
	)
	if !apperror.IsKind(err, apperror.KindUsage) ||
		!strings.Contains(err.Error(), "explicit confirmation") {
		t.Fatalf("confirmation: %v", err)
	}

	state := &versionServerState{}
	server := newVersionAppServer(t, state)
	defer server.Close()
	configureSpaceTestProfile(t, server.URL, "")
	err = RunVersionWithOptions(
		context.Background(),
		VersionRequest{
			Operation: VersionInfo, Path: "/report.txt",
			VersionID: "missing",
		},
		"", RunOptions{Out: io.Discard},
	)
	if !apperror.IsKind(err, apperror.KindNotFound) ||
		!strings.Contains(err.Error(), "ocis version list") {
		t.Fatalf("missing: %v", err)
	}
}

func newVersionAppServer(
	t *testing.T, state *versionServerState,
) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch {
		case request.Method == "PROPFIND" &&
			request.URL.Path == "/remote.php/dav/files/alice/report.txt":
			writer.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(writer, appVersionFile)
		case request.Method == "PROPFIND" &&
			request.URL.Path == "/remote.php/dav/meta/storage$space!file/v":
			writer.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(writer, appVersions)
		case request.Method == http.MethodGet &&
			request.URL.Path ==
				"/remote.php/dav/meta/storage$space!file/v/version-1":
			state.downloaded = true
			writer.Header().Set("Content-Length", "7")
			writer.Header().Set("ETag", `"version-etag"`)
			_, _ = io.WriteString(writer, "content")
		case request.Method == "COPY" &&
			request.URL.Path ==
				"/remote.php/dav/meta/storage$space!file/v/version-1":
			state.restored = true
			writer.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
	}))
}
