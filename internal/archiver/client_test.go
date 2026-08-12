package archiver

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mzner/ocis-cli/internal/httpapi"
)

func TestDownloadAuthenticatesAndStreamsSelectedIDs(t *testing.T) {
	payload := testZIP(t, map[string]string{"reports/report.txt": "hello"})
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if attempts.Add(1) == 1 {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if request.URL.Path != "/archiver" ||
			request.URL.Query().Get("existing") != "value" ||
			request.URL.Query().Get("output-format") != "zip" ||
			request.Header.Get("Authorization") != "Bearer token" ||
			request.Header.Get("Accept") != "application/zip" {
			t.Fatalf("request: %s headers=%v", request.URL.String(), request.Header)
		}
		ids := request.URL.Query()["id"]
		if len(ids) != 2 || ids[0] != "storage$space!one" ||
			ids[1] != "storage$space!two" {
			t.Fatalf("IDs: %v", ids)
		}
		_, _ = writer.Write(payload)
	}))
	defer server.Close()
	client, err := NewClient(httpapi.Config{
		Server: server.URL, AuthType: "oidc", AccessToken: "token", Retries: 1,
		RetryWait: 1,
	}, "/archiver?existing=value", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	var progress int64
	result, err := client.Download(
		context.Background(), DownloadRequest{
			ResourceIDs: []string{"storage$space!one", "storage$space!two"},
			Format:      "zip",
		}, &output, func(written int64) { progress = written },
	)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), payload) || result.Bytes != int64(len(payload)) ||
		progress != int64(len(payload)) || attempts.Load() != 2 {
		t.Fatalf(
			"bytes=%d progress=%d attempts=%d", result.Bytes, progress, attempts.Load(),
		)
	}
}

func TestNewClientRejectsUnsafeAdvertisedURLs(t *testing.T) {
	for _, endpoint := range []string{
		"https://attacker.example/archiver",
		"file:///tmp/archive",
		"https://user@example.test/archiver",
		"https://example.test/archiver#fragment",
		"",
		"   ",
	} {
		_, err := NewClient(
			httpapi.Config{Server: "https://example.test"}, endpoint, nil,
		)
		if err == nil {
			t.Fatalf("unsafe endpoint accepted: %q", endpoint)
		}
	}
	client, err := NewClient(
		httpapi.Config{Server: "https://example.test"},
		"https://example.test/archiver", nil,
	)
	if err != nil || client.resource != "/archiver" {
		t.Fatalf("same-origin endpoint: %#v, %v", client, err)
	}
}

func TestDownloadValidationAndHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		writer.WriteHeader(http.StatusRequestEntityTooLarge)
		_, _ = io.WriteString(writer, "reached max total files size")
	}))
	defer server.Close()
	client, err := NewClient(
		httpapi.Config{Server: server.URL}, "/archiver", server.Client(),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []DownloadRequest{
		{Format: "zip"},
		{ResourceIDs: []string{"id"}, Format: "rar"},
		{ResourceIDs: []string{""}, Format: "zip"},
	} {
		if _, err := client.Download(
			context.Background(), request, io.Discard, nil,
		); err == nil {
			t.Fatalf("invalid request accepted: %#v", request)
		}
	}
	_, err = client.Download(
		context.Background(), DownloadRequest{
			ResourceIDs: []string{"id"}, Format: "zip",
		}, io.Discard, nil,
	)
	if err == nil || !strings.Contains(err.Error(), "413") ||
		!strings.Contains(err.Error(), "max total") {
		t.Fatalf("HTTP error: %v", err)
	}
}

func TestDownloadHonorsCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		writer.WriteHeader(http.StatusOK)
		writer.(http.Flusher).Flush()
		close(started)
		<-request.Context().Done()
	}))
	defer server.Close()
	client, err := NewClient(
		httpapi.Config{Server: server.URL}, "/archiver", server.Client(),
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := client.Download(ctx, DownloadRequest{
			ResourceIDs: []string{"id"}, Format: "zip",
		}, io.Discard, nil)
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("error: %v", err)
	}
}

func TestValidateFileAcceptsCompleteZIPAndTAR(t *testing.T) {
	root := t.TempDir()
	zipName := filepath.Join(root, "archive.zip")
	if err := os.WriteFile(
		zipName, testZIP(t, map[string]string{"file.txt": "hello"}), 0600,
	); err != nil {
		t.Fatal(err)
	}
	if err := ValidateFile(zipName, "zip", ValidationLimits{}); err != nil {
		t.Fatal(err)
	}
	tarName := filepath.Join(root, "archive.tar")
	if err := os.WriteFile(
		tarName, testTAR(t, map[string]string{"file.txt": "hello"}), 0600,
	); err != nil {
		t.Fatal(err)
	}
	if err := ValidateFile(tarName, "tar", ValidationLimits{}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		format string
	}{
		{zipName, "tar"}, {tarName, "zip"}, {zipName, "rar"},
	} {
		if err := ValidateFile(test.name, test.format, ValidationLimits{}); err == nil {
			t.Fatalf("invalid %s as %s accepted", test.name, test.format)
		}
	}
}

func TestValidateFileEnforcesDecodedLimits(t *testing.T) {
	root := t.TempDir()
	zipName := filepath.Join(root, "large.zip")
	if err := os.WriteFile(
		zipName, testZIP(t, map[string]string{
			"one.txt": "12345", "two.txt": "67890",
		}), 0600,
	); err != nil {
		t.Fatal(err)
	}
	if err := ValidateFile(zipName, "zip", ValidationLimits{
		MaxEntries: 1, MaxBytes: 100,
	}); err == nil || !strings.Contains(err.Error(), "more than 1 entries") {
		t.Fatalf("entry limit error: %v", err)
	}
	if err := ValidateFile(zipName, "zip", ValidationLimits{
		MaxEntries: 10, MaxBytes: 9,
	}); err == nil || !strings.Contains(err.Error(), "exceeds 9 bytes") {
		t.Fatalf("byte limit error: %v", err)
	}

	tarName := filepath.Join(root, "large.tar")
	if err := os.WriteFile(
		tarName, testTAR(t, map[string]string{
			"one.txt": "12345", "two.txt": "67890",
		}), 0600,
	); err != nil {
		t.Fatal(err)
	}
	if err := ValidateFile(tarName, "tar", ValidationLimits{
		MaxEntries: 1, MaxBytes: 100,
	}); err == nil || !strings.Contains(err.Error(), "more than 1 entries") {
		t.Fatalf("entry limit error: %v", err)
	}
	if err := ValidateFile(tarName, "tar", ValidationLimits{
		MaxEntries: 10, MaxBytes: 9,
	}); err == nil || !strings.Contains(err.Error(), "exceeds 9 bytes") {
		t.Fatalf("byte limit error: %v", err)
	}
}

func testZIP(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	for name, content := range files {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(entry, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func testTAR(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var output bytes.Buffer
	archive := tar.NewWriter(&output)
	for name, content := range files {
		if err := archive.WriteHeader(&tar.Header{
			Name: name, Mode: 0600, Size: int64(len(content)),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(archive, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
