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

func TestCopyAndMoveResolveExistingDirectoryDestination(t *testing.T) {
	methods := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		methods[request.Method]++
		switch request.Method {
		case "PROPFIND":
			writer.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(
				writer, davDirectoryXML("/remote.php/dav/files/alice/Documents/", "Documents"),
			)
		case "COPY", "MOVE":
			if !strings.HasSuffix(
				request.Header.Get("Destination"), "/Documents/report.txt",
			) {
				t.Fatalf("%s destination: %s", request.Method, request.Header.Get("Destination"))
			}
			writer.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected method: %s", request.Method)
		}
	}))
	defer server.Close()
	configureFilesystemTestProfile(t, server.URL)
	var output bytes.Buffer
	for _, operation := range []FilesystemOperation{
		FilesystemCopy, FilesystemMove,
	} {
		if err := RunFilesystemWithOptions(
			context.Background(), FilesystemRequest{
				Operation: operation, Source: "/report.txt",
				Destination: "/Documents",
			}, "", RunOptions{Out: &output},
		); err != nil {
			t.Fatalf("%s: %v", operation, err)
		}
	}
	if methods["COPY"] != 1 || methods["MOVE"] != 1 ||
		strings.Count(output.String(), "/Documents/report.txt") != 2 {
		t.Fatalf("methods=%#v output=%s", methods, output.String())
	}
}

func TestCopyDirectoryDestinationDryRunReportsResolvedPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.Method != "PROPFIND" {
			t.Fatalf("dry run used %s", request.Method)
		}
		writer.WriteHeader(http.StatusMultiStatus)
		_, _ = io.WriteString(
			writer, davDirectoryXML("/remote.php/dav/files/alice/Documents/", "Documents"),
		)
	}))
	defer server.Close()
	configureFilesystemTestProfile(t, server.URL)
	var output bytes.Buffer
	if err := RunFilesystemWithOptions(
		context.Background(), FilesystemRequest{
			Operation: FilesystemCopy, Source: "/report.txt",
			Destination: "/Documents/", DryRun: true,
		}, "", RunOptions{Out: &output, OutputMode: appoutput.JSON},
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"destination": "/Documents/report.txt"`) ||
		!strings.Contains(output.String(), `"dryRun": true`) {
		t.Fatalf("output: %s", output.String())
	}
}

func TestCopyAndMoveDestinationDirectoryErrorsFailClosed(t *testing.T) {
	for _, test := range []struct {
		name        string
		status      int
		body        string
		wantCode    int
		want        string
		destination string
	}{
		{
			name: "missing trailing slash directory", status: http.StatusNotFound,
			wantCode: 4, want: "destination directory /missing does not exist",
			destination: "/missing/",
		},
		{
			name: "trailing slash file", status: http.StatusMultiStatus,
			body: appDAVFile, wantCode: 2, want: "/report.txt is not a directory",
			destination: "/report.txt/",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			mutations := 0
			server := httptest.NewServer(http.HandlerFunc(func(
				writer http.ResponseWriter, request *http.Request,
			) {
				if request.Method != "PROPFIND" {
					mutations++
				}
				writer.WriteHeader(test.status)
				_, _ = io.WriteString(writer, test.body)
			}))
			defer server.Close()
			configureFilesystemTestProfile(t, server.URL)
			err := RunFilesystemWithOptions(
				context.Background(), FilesystemRequest{
					Operation: FilesystemMove, Source: "/source.txt",
					Destination: test.destination,
				}, "", RunOptions{Out: io.Discard},
			)
			if apperror.ExitCode(err) != test.wantCode || mutations != 0 ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v mutations=%d", err, mutations)
			}
		})
	}
}

func TestCopyPreservesExplicitMissingDestination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch request.Method {
		case "PROPFIND":
			writer.WriteHeader(http.StatusNotFound)
		case "COPY":
			if !strings.HasSuffix(
				request.Header.Get("Destination"), "/renamed.txt",
			) {
				t.Fatalf("destination: %s", request.Header.Get("Destination"))
			}
			writer.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected method: %s", request.Method)
		}
	}))
	defer server.Close()
	configureFilesystemTestProfile(t, server.URL)
	if err := RunFilesystemWithOptions(
		context.Background(), FilesystemRequest{
			Operation: FilesystemCopy, Source: "/report.txt",
			Destination: "/renamed.txt",
		}, "", RunOptions{Out: io.Discard},
	); err != nil {
		t.Fatal(err)
	}
}
