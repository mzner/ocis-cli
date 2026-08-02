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

func TestMkdirParentsCreatesOnlyMissingDirectoriesAndIsIdempotent(t *testing.T) {
	resources := map[string]string{"/Projects": "directory"}
	created := make([]string, 0)
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		remote := strings.TrimPrefix(
			request.URL.Path, "/remote.php/dav/files/alice",
		)
		switch request.Method {
		case "PROPFIND":
			kind, ok := resources[remote]
			if !ok {
				writer.WriteHeader(http.StatusNotFound)
				return
			}
			writer.WriteHeader(http.StatusMultiStatus)
			if kind == "directory" {
				_, _ = io.WriteString(writer, davDirectoryXML(remote+"/", pathName(remote)))
				return
			}
			_, _ = io.WriteString(writer, davFileXML(remote, pathName(remote), 1))
		case "MKCOL":
			resources[remote] = "directory"
			created = append(created, remote)
			writer.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected method: %s", request.Method)
		}
	}))
	defer server.Close()
	configureFilesystemTestProfile(t, server.URL)
	target := "/Projects/2026/Reports"
	var machine bytes.Buffer
	if err := RunFilesystemWithOptions(
		context.Background(), FilesystemRequest{
			Operation: FilesystemMkdir, Source: target, Parents: true,
		}, "", RunOptions{Out: &machine, OutputMode: appoutput.JSON},
	); err != nil {
		t.Fatal(err)
	}
	if strings.Join(created, ",") != "/Projects/2026,/Projects/2026/Reports" ||
		!strings.Contains(machine.String(), `"createdDirectories": [`) ||
		!strings.Contains(machine.String(), `"parents": true`) {
		t.Fatalf("created=%v output=%s", created, machine.String())
	}

	created = nil
	var human bytes.Buffer
	if err := RunFilesystemWithOptions(
		context.Background(), FilesystemRequest{
			Operation: FilesystemMkdir, Source: target, Parents: true,
		}, "", RunOptions{Out: &human},
	); err != nil {
		t.Fatal(err)
	}
	if len(created) != 0 || human.String() != "Directory exists "+target+"\n" {
		t.Fatalf("created=%v output=%q", created, human.String())
	}
}

func TestMkdirParentsRejectsFileComponent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.Method != "PROPFIND" {
			t.Fatalf("unexpected mutation: %s", request.Method)
		}
		remote := strings.TrimPrefix(
			request.URL.Path, "/remote.php/dav/files/alice",
		)
		writer.WriteHeader(http.StatusMultiStatus)
		if remote == "/Projects" {
			_, _ = io.WriteString(writer, davDirectoryXML(remote+"/", "Projects"))
			return
		}
		_, _ = io.WriteString(
			writer,
			`<?xml version="1.0"?><d:multistatus xmlns:d="DAV:">`+
				davFileXML(remote, "2026", 1)+`</d:multistatus>`,
		)
	}))
	defer server.Close()
	configureFilesystemTestProfile(t, server.URL)
	err := RunFilesystemWithOptions(
		context.Background(), FilesystemRequest{
			Operation: FilesystemMkdir, Source: "/Projects/2026/Reports",
			Parents: true,
		}, "", RunOptions{Out: io.Discard},
	)
	if apperror.ExitCode(err) != 5 ||
		!strings.Contains(err.Error(), "/Projects/2026 is a file") {
		t.Fatalf("error: %v", err)
	}
}

func TestMkdirParentsAcceptsRemoteRoot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		http.ResponseWriter, *http.Request,
	) {
		t.Fatal("mkdir -p / contacted the server")
	}))
	defer server.Close()
	configureFilesystemTestProfile(t, server.URL)
	var output bytes.Buffer
	if err := RunFilesystemWithOptions(
		context.Background(), FilesystemRequest{
			Operation: FilesystemMkdir, Source: "/", Parents: true,
		}, "", RunOptions{Out: &output},
	); err != nil {
		t.Fatal(err)
	}
	if output.String() != "Directory exists /\n" {
		t.Fatalf("output: %q", output.String())
	}
}

func pathName(remote string) string {
	parts := strings.Split(strings.TrimSuffix(remote, "/"), "/")
	return parts[len(parts)-1]
}
