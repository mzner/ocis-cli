package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

func TestFilesystemCatStreamsExactContent(t *testing.T) {
	var gets int
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch request.Method {
		case "PROPFIND":
			writer.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(writer, appDAVFile)
		case http.MethodGet:
			gets++
			writer.Header().Set("Content-Length", "5")
			_, _ = io.WriteString(writer, "hello")
		default:
			t.Fatalf("unexpected method: %s", request.Method)
		}
	}))
	defer server.Close()
	configureFilesystemTestProfile(t, server.URL)
	var output bytes.Buffer
	if err := RunFilesystemWithOptions(
		context.Background(),
		FilesystemRequest{Operation: FilesystemCat, Source: "/report.txt"},
		"", RunOptions{Out: &output},
	); err != nil {
		t.Fatal(err)
	}
	if output.String() != "hello" || gets != 1 {
		t.Fatalf("output=%q gets=%d", output.String(), gets)
	}
}

func TestFilesystemCatRejectsDirectoriesBeforeGet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.Method != "PROPFIND" {
			t.Fatalf("unexpected method: %s", request.Method)
		}
		writer.WriteHeader(http.StatusMultiStatus)
		_, _ = io.WriteString(writer, appDAVDirectory)
	}))
	defer server.Close()
	configureFilesystemTestProfile(t, server.URL)
	err := RunFilesystemWithOptions(
		context.Background(),
		FilesystemRequest{Operation: FilesystemCat, Source: "/demo"},
		"", RunOptions{Out: io.Discard},
	)
	if !apperror.IsKind(err, apperror.KindUsage) ||
		!strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("error: %v", err)
	}
}

func TestFilesystemTreeIsSortedBoundedAndMachineReadable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.Method != "PROPFIND" {
			t.Fatalf("unexpected method: %s", request.Method)
		}
		writer.WriteHeader(http.StatusMultiStatus)
		switch {
		case request.Header.Get("Depth") == "0":
			_, _ = io.WriteString(writer, davDirectoryXML("/demo/", "demo"))
		case strings.HasSuffix(request.URL.Path, "/demo/alpha"):
			_, _ = io.WriteString(writer, davListXML(
				"/demo/alpha/", "alpha",
				davFileXML("/demo/alpha/deep.txt", "deep.txt", 4),
			))
		default:
			_, _ = io.WriteString(writer, davListXML(
				"/demo/", "demo",
				davFileXML("/demo/z.txt", "z.txt", 1),
				davDirectoryResponseXML("/demo/alpha/", "alpha"),
			))
		}
	}))
	defer server.Close()
	configureFilesystemTestProfile(t, server.URL)

	var human bytes.Buffer
	request := FilesystemRequest{
		Operation: FilesystemTree, Source: "/demo",
		MaxDepth: 10, MaxEntries: 10,
	}
	if err := RunFilesystemWithOptions(
		context.Background(), request, "", RunOptions{Out: &human},
	); err != nil {
		t.Fatal(err)
	}
	want := "/demo/\n├── alpha/\n│   └── deep.txt\n└── z.txt\n"
	if human.String() != want {
		t.Fatalf("tree:\n%s\nwant:\n%s", human.String(), want)
	}

	request.MaxDepth = 1
	var machine bytes.Buffer
	if err := RunFilesystemWithOptions(
		context.Background(), request, "", RunOptions{
			Out: &machine, OutputMode: appoutput.JSON,
		},
	); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(machine.String(), "deep.txt") ||
		!strings.Contains(machine.String(), `"depth": 1`) ||
		!strings.Contains(machine.String(), `"path": "/demo/alpha"`) {
		t.Fatalf("JSON: %s", machine.String())
	}
}

func TestFilesystemDUReportsLogicalSizeAndDepthCompleteness(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.Method != "PROPFIND" {
			t.Fatalf("unexpected method: %s", request.Method)
		}
		writer.WriteHeader(http.StatusMultiStatus)
		switch {
		case request.Header.Get("Depth") == "0":
			_, _ = io.WriteString(writer, davDirectoryXML("/demo/", "demo"))
		case strings.HasSuffix(request.URL.Path, "/demo/alpha"):
			_, _ = io.WriteString(writer, davListXML(
				"/demo/alpha/", "alpha",
				davFileXML("/demo/alpha/deep.txt", "deep.txt", 4),
			))
		default:
			_, _ = io.WriteString(writer, davListXML(
				"/demo/", "demo",
				davFileXML("/demo/z.txt", "z.txt", 1),
				davDirectoryResponseXML("/demo/alpha/", "alpha"),
			))
		}
	}))
	defer server.Close()
	configureFilesystemTestProfile(t, server.URL)

	var complete bytes.Buffer
	request := FilesystemRequest{
		Operation: FilesystemDU, Source: "/demo",
		MaxDepth: 10, MaxEntries: 10,
	}
	if err := RunFilesystemWithOptions(
		context.Background(), request, "", RunOptions{
			Out: &complete, OutputMode: appoutput.JSON,
		},
	); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"logicalBytes": 5`, `"files": 2`, `"directories": 2`,
		`"entries": 4`, `"complete": true`,
	} {
		if !strings.Contains(complete.String(), expected) {
			t.Fatalf("complete JSON lacks %q: %s", expected, complete.String())
		}
	}

	request.MaxDepth = 1
	var limited bytes.Buffer
	if err := RunFilesystemWithOptions(
		context.Background(), request, "", RunOptions{
			Out: &limited, OutputMode: appoutput.JSON,
		},
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(limited.String(), `"logicalBytes": 1`) ||
		!strings.Contains(limited.String(), `"complete": false`) ||
		strings.Contains(limited.String(), "deep.txt") {
		t.Fatalf("limited JSON: %s", limited.String())
	}
}

func TestFilesystemTreeFailsClosedAtEntryLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		writer.WriteHeader(http.StatusMultiStatus)
		if request.Header.Get("Depth") == "0" {
			_, _ = io.WriteString(writer, davDirectoryXML("/demo/", "demo"))
			return
		}
		_, _ = io.WriteString(writer, davListXML(
			"/demo/", "demo",
			davFileXML("/demo/one.txt", "one.txt", 1),
			davFileXML("/demo/two.txt", "two.txt", 1),
		))
	}))
	defer server.Close()
	configureFilesystemTestProfile(t, server.URL)
	var output bytes.Buffer
	err := RunFilesystemWithOptions(
		context.Background(), FilesystemRequest{
			Operation: FilesystemTree, Source: "/demo",
			MaxDepth: 1, MaxEntries: 2,
		}, "", RunOptions{Out: &output},
	)
	if !apperror.IsKind(err, apperror.KindUsage) ||
		!strings.Contains(err.Error(), "exceeds --max-entries 2") ||
		output.Len() != 0 {
		t.Fatalf("error=%v output=%q", err, output.String())
	}
}

func configureFilesystemTestProfile(t *testing.T, server string) {
	t.Helper()
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := saveStore(defaultDependencies(), &store{
		Current: "work",
		Profiles: map[string]profile{"work": {
			Server: server, Insecure: true, Username: "alice",
			AuthType: "basic", Password: "secret",
		}},
	}); err != nil {
		t.Fatal(err)
	}
}

func davDirectoryXML(href, name string) string {
	return `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:">` +
		davDirectoryResponseXML(href, name) + `</d:multistatus>`
}

func davListXML(href, name string, children ...string) string {
	return `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:">` +
		davDirectoryResponseXML(href, name) + strings.Join(children, "") +
		`</d:multistatus>`
}

func davDirectoryResponseXML(href, name string) string {
	return fmt.Sprintf(`<d:response><d:href>%s</d:href><d:propstat>
<d:status>HTTP/1.1 200 OK</d:status><d:prop><d:displayname>%s</d:displayname>
<d:resourcetype><d:collection/></d:resourcetype></d:prop>
</d:propstat></d:response>`, href, name)
}

func davFileXML(href, name string, size int64) string {
	return fmt.Sprintf(`<d:response><d:href>%s</d:href><d:propstat>
<d:status>HTTP/1.1 200 OK</d:status><d:prop><d:displayname>%s</d:displayname>
<d:getcontentlength>%d</d:getcontentlength><d:resourcetype/></d:prop>
</d:propstat></d:response>`, href, name, size)
}
