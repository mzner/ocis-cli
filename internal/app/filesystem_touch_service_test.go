package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

func TestTouchCreatesEmptyFileAndIsIdempotent(t *testing.T) {
	const target = "/Documents/empty.txt"
	var targetExists, temporaryExists bool
	var temporary string
	mutations := 0
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		remote := strings.TrimPrefix(
			request.URL.Path, "/remote.php/dav/files/alice",
		)
		switch request.Method {
		case "PROPFIND":
			switch {
			case remote == target && targetExists:
				writeTouchFileResponse(writer, remote, 0)
			case remote == temporary && temporaryExists:
				writeTouchFileResponse(writer, remote, 0)
			default:
				writer.WriteHeader(http.StatusNotFound)
			}
		case http.MethodPut:
			mutations++
			body, err := io.ReadAll(request.Body)
			if err != nil || len(body) != 0 || remote == target {
				t.Errorf("PUT remote=%q body=%q err=%v", remote, body, err)
			}
			temporary, temporaryExists = remote, true
			writer.WriteHeader(http.StatusCreated)
		case "MOVE":
			mutations++
			if remote != temporary || request.Header.Get("Overwrite") != "F" ||
				!strings.HasSuffix(request.Header.Get("Destination"), target) {
				t.Errorf("unexpected MOVE headers=%v remote=%q", request.Header, remote)
			}
			temporaryExists, targetExists = false, true
			writer.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected method: %s", request.Method)
		}
	}))
	defer server.Close()
	configureFilesystemTestProfile(t, server.URL)

	var created bytes.Buffer
	request := FilesystemRequest{Operation: FilesystemTouch, Source: target}
	if err := RunFilesystemWithOptions(
		context.Background(), request, "", RunOptions{
			Out: &created, OutputMode: appoutput.JSON,
		},
	); err != nil {
		t.Fatal(err)
	}
	if mutations != 2 || !strings.Contains(created.String(), `"created": true`) ||
		!strings.Contains(created.String(), `"unchanged": false`) {
		t.Fatalf("mutations=%d output=%s", mutations, created.String())
	}

	var unchanged bytes.Buffer
	if err := RunFilesystemWithOptions(
		context.Background(), request, "", RunOptions{Out: &unchanged},
	); err != nil {
		t.Fatal(err)
	}
	if mutations != 2 || unchanged.String() != "Unchanged "+target+"\n" {
		t.Fatalf("mutations=%d output=%q", mutations, unchanged.String())
	}
}

func TestTouchLeavesExistingFileUnchanged(t *testing.T) {
	mutations := 0
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.Method != "PROPFIND" {
			mutations++
			t.Fatalf("unexpected mutation: %s", request.Method)
		}
		writeTouchFileResponse(writer, "/Documents/report.txt", 42)
	}))
	defer server.Close()
	configureFilesystemTestProfile(t, server.URL)
	var output bytes.Buffer
	if err := RunFilesystemWithOptions(
		context.Background(), FilesystemRequest{
			Operation: FilesystemTouch, Source: "/Documents/report.txt",
		}, "", RunOptions{Out: &output, OutputMode: appoutput.JSON},
	); err != nil {
		t.Fatal(err)
	}
	if mutations != 0 || !strings.Contains(output.String(), `"created": false`) ||
		!strings.Contains(output.String(), `"unchanged": true`) {
		t.Fatalf("mutations=%d output=%s", mutations, output.String())
	}
}

func TestTouchRejectsExistingDirectory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.Method != "PROPFIND" {
			t.Fatalf("unexpected mutation: %s", request.Method)
		}
		writer.WriteHeader(http.StatusMultiStatus)
		_, _ = io.WriteString(writer, appDAVDirectory)
	}))
	defer server.Close()
	configureFilesystemTestProfile(t, server.URL)
	err := RunFilesystemWithOptions(
		context.Background(), FilesystemRequest{
			Operation: FilesystemTouch, Source: "/demo",
		}, "", RunOptions{Out: io.Discard},
	)
	if !apperror.IsKind(err, apperror.KindConflict) ||
		!strings.Contains(err.Error(), "/demo is a directory") {
		t.Fatalf("error: %v", err)
	}
}

func TestTouchAcceptsConcurrentFileCreationWithoutOverwriting(t *testing.T) {
	const target = "/Documents/race.txt"
	var targetExists, temporaryExists bool
	var temporary string
	deletes := 0
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		remote := strings.TrimPrefix(
			request.URL.Path, "/remote.php/dav/files/alice",
		)
		switch request.Method {
		case "PROPFIND":
			switch {
			case remote == target && targetExists:
				writeTouchFileResponse(writer, remote, 7)
			case remote == temporary && temporaryExists:
				writeTouchFileResponse(writer, remote, 0)
			default:
				writer.WriteHeader(http.StatusNotFound)
			}
		case http.MethodPut:
			temporary, temporaryExists = remote, true
			writer.WriteHeader(http.StatusCreated)
		case "MOVE":
			targetExists = true
			writer.WriteHeader(http.StatusPreconditionFailed)
		case http.MethodDelete:
			if remote != temporary {
				t.Errorf("deleted %q, want %q", remote, temporary)
			}
			deletes++
			temporaryExists = false
			writer.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected method: %s", request.Method)
		}
	}))
	defer server.Close()
	configureFilesystemTestProfile(t, server.URL)
	var output bytes.Buffer
	if err := RunFilesystemWithOptions(
		context.Background(), FilesystemRequest{
			Operation: FilesystemTouch, Source: target,
		}, "", RunOptions{Out: &output},
	); err != nil {
		t.Fatal(err)
	}
	if deletes != 1 || output.String() != "Unchanged "+target+"\n" {
		t.Fatalf("deletes=%d output=%q", deletes, output.String())
	}
}

func writeTouchFileResponse(
	writer http.ResponseWriter, remote string, size int64,
) {
	writer.WriteHeader(http.StatusMultiStatus)
	_, _ = fmt.Fprintf(
		writer, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:">%s</d:multistatus>`,
		davFileXML(remote, pathName(remote), size),
	)
}
