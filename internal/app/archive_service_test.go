package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

const archiveDirectoryStat = `<?xml version="1.0"?>
<d:multistatus xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns">
  <d:response><d:href>/remote.php/dav/files/alice/reports/</d:href>
    <d:propstat><d:status>HTTP/1.1 200 OK</d:status><d:prop>
      <d:displayname>reports</d:displayname>
      <d:resourcetype><d:collection/></d:resourcetype>
      <oc:fileid>storage$space!reports</oc:fileid>
    </d:prop></d:propstat>
  </d:response>
</d:multistatus>`

const archiveDirectoryList = `<?xml version="1.0"?>
<d:multistatus xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns">
  <d:response><d:href>/remote.php/dav/files/alice/reports/</d:href>
    <d:propstat><d:status>HTTP/1.1 200 OK</d:status><d:prop>
      <d:displayname>reports</d:displayname>
      <d:resourcetype><d:collection/></d:resourcetype>
      <oc:fileid>storage$space!reports</oc:fileid>
    </d:prop></d:propstat>
  </d:response>
  <d:response><d:href>/remote.php/dav/files/alice/reports/report.txt</d:href>
    <d:propstat><d:status>HTTP/1.1 200 OK</d:status><d:prop>
      <d:displayname>report.txt</d:displayname>
      <d:getcontentlength>5</d:getcontentlength><d:resourcetype/>
      <oc:fileid>storage$space!report</oc:fileid>
    </d:prop></d:propstat>
  </d:response>
</d:multistatus>`

func TestArchiveDownloadAndDryRun(t *testing.T) {
	payload := appArchiveZIP(t, "reports/report.txt", "hello")
	var archiveRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch {
		case request.URL.Path == "/ocs/v2.php/cloud/capabilities":
			writeAppOCS(writer, `{"capabilities":{"files":{"archivers":[{
				"enabled":true,"version":"2.0.0","formats":["zip","tar"],
				"archiver_url":"/archiver","max_num_files":"10","max_size":"100"
			}]}}}`)
		case request.Method == "PROPFIND" && request.Header.Get("Depth") == "0":
			writer.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(writer, archiveDirectoryStat)
		case request.Method == "PROPFIND" && request.Header.Get("Depth") == "1":
			writer.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(writer, archiveDirectoryList)
		case request.URL.Path == "/archiver":
			archiveRequests.Add(1)
			if request.URL.Query().Get("id") != "storage$space!reports" ||
				request.URL.Query().Get("output-format") != "zip" {
				t.Fatalf("archive query: %s", request.URL.RawQuery)
			}
			_, _ = writer.Write(payload)
		default:
			t.Fatalf("unexpected request: %s %s depth=%s", request.Method, request.URL.Path, request.Header.Get("Depth"))
		}
	}))
	defer server.Close()
	configureSpaceTestProfile(t, server.URL, "")
	destination := filepath.Join(t.TempDir(), "reports.zip")

	var dryOutput bytes.Buffer
	if err := RunArchiveDownloadWithOptions(
		context.Background(), ArchiveDownloadRequest{
			Paths: []string{"/reports"}, Destination: destination, DryRun: true,
		}, "", RunOptions{Out: &dryOutput, Err: io.Discard},
	); err != nil {
		t.Fatal(err)
	}
	if archiveRequests.Load() != 0 ||
		!strings.Contains(dryOutput.String(), "Would archive 2 entries (5 source bytes)") {
		t.Fatalf("requests=%d output=%q", archiveRequests.Load(), dryOutput.String())
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("dry-run created destination: %v", err)
	}

	var output bytes.Buffer
	if err := RunArchiveDownloadWithOptions(
		context.Background(), ArchiveDownloadRequest{
			Paths: []string{"reports"}, Destination: destination,
		}, "", RunOptions{
			Out: &output, Err: io.Discard, OutputMode: appoutput.JSON,
		},
	); err != nil {
		t.Fatal(err)
	}
	if archiveRequests.Load() != 1 {
		t.Fatalf("archive requests: %d", archiveRequests.Load())
	}
	if data, err := os.ReadFile(destination); err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("archive bytes=%d error=%v", len(data), err)
	}
	var envelope appoutput.Envelope
	if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Type != "archive" || !strings.Contains(output.String(), `"entries": 2`) ||
		!strings.Contains(output.String(), `"archiveBytes":`) {
		t.Fatalf("output: %s", output.String())
	}
}

func TestArchiveFormatsAndPreferredVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		writeAppOCS(writer, `{"capabilities":{"files":{"archivers":[
			{"enabled":true,"version":"1.0.0","formats":["zip"],
			 "archiver_url":"/old","max_num_files":"5","max_size":"50"},
			{"enabled":true,"version":"2.0.0","formats":["tar","zip"],
			 "archiver_url":"/archiver","max_num_files":"10","max_size":"100"},
			{"enabled":false,"version":"3.0.0","formats":["zip"],
			 "archiver_url":"/disabled"}
		]}}}`)
	}))
	defer server.Close()
	configureSpaceTestProfile(t, server.URL, "")
	var output bytes.Buffer
	if err := RunArchiveFormatsWithOptions(
		context.Background(), "", RunOptions{Out: &output},
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "FORMAT") ||
		!strings.Contains(output.String(), "tar") ||
		!strings.Contains(output.String(), "2.0.0") ||
		strings.Contains(output.String(), "1.0.0") {
		t.Fatalf("output: %q", output.String())
	}
}

func TestArchiveValidationFailsBeforeProfileLoad(t *testing.T) {
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "missing", "config.json"))
	tests := []ArchiveDownloadRequest{
		{Destination: "archive.zip"},
		{Paths: []string{"/reports"}},
		{Paths: []string{"/reports"}, Destination: "-"},
		{Paths: []string{"/reports"}, Destination: "archive.zip", Format: "tar"},
		{Paths: []string{"/reports", "/reports/file"}, Destination: "archive.zip"},
	}
	for _, request := range tests {
		err := RunArchiveDownloadWithOptions(
			context.Background(), request, "", RunOptions{Out: io.Discard},
		)
		if !apperror.IsKind(err, apperror.KindUsage) {
			t.Fatalf("request=%#v error=%v", request, err)
		}
	}
}

func TestArchiveDownloadRefusesExistingDestinationBeforeNetwork(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "archive.zip")
	if err := os.WriteFile(destination, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	err := RunArchiveDownloadWithOptions(
		context.Background(), ArchiveDownloadRequest{
			Paths: []string{"/reports"}, Destination: destination,
		}, "", RunOptions{Out: io.Discard},
	)
	if !apperror.IsKind(err, apperror.KindConflict) ||
		!strings.Contains(err.Error(), "--overwrite") {
		t.Fatalf("error: %v", err)
	}
	if data, readErr := os.ReadFile(destination); readErr != nil || string(data) != "existing" {
		t.Fatalf("destination=%q error=%v", data, readErr)
	}
}

func TestArchiveCapabilityLimitsFailBeforeDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch {
		case request.URL.Path == "/ocs/v2.php/cloud/capabilities":
			writeAppOCS(writer, `{"capabilities":{"files":{"archivers":[{
				"enabled":true,"version":"2.0.0","formats":["zip"],
				"archiver_url":"/archiver","max_num_files":"1","max_size":"4"
			}]}}}`)
		case request.Method == "PROPFIND" && request.Header.Get("Depth") == "0":
			writer.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(writer, archiveDirectoryStat)
		case request.Method == "PROPFIND" && request.Header.Get("Depth") == "1":
			writer.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(writer, archiveDirectoryList)
		case request.URL.Path == "/archiver":
			t.Fatal("archive request sent despite preflight limit")
		}
	}))
	defer server.Close()
	configureSpaceTestProfile(t, server.URL, "")
	err := RunArchiveDownloadWithOptions(
		context.Background(), ArchiveDownloadRequest{
			Paths: []string{"/reports"}, Destination: filepath.Join(t.TempDir(), "archive.zip"),
		}, "", RunOptions{Out: io.Discard, Err: io.Discard},
	)
	if !apperror.IsKind(err, apperror.KindUsage) ||
		!strings.Contains(err.Error(), "server limit") {
		t.Fatalf("error: %v", err)
	}
}

func appArchiveZIP(t *testing.T, name, content string) []byte {
	t.Helper()
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	entry, err := archive.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(entry, content); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
