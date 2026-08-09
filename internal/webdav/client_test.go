package webdav

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mzner/ocis-cli/internal/logging"
	"github.com/mzner/ocis-cli/internal/retry"
)

func TestClientRetriesTemporaryResponse(t *testing.T) {
	var attempts atomic.Int64
	var diagnostics bytes.Buffer
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if attempts.Add(1) < 3 {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeDAVFile(writer, request.URL.Path, 5)
	}))
	defer server.Close()

	client := NewClient(Config{
		Server: server.URL, Username: "alice", AuthType: "basic",
		Password: "secret", Retries: 2, RetryWait: time.Millisecond,
		Logger: logging.NewText(&diagnostics),
	}, server.Client())
	if _, err := client.Stat(context.Background(), "/report.txt"); err != nil {
		t.Fatal(err)
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("attempts: got %d, want 3", got)
	}
	if !strings.Contains(diagnostics.String(), "attempt=2 reason=503 Service Unavailable") {
		t.Fatalf("diagnostics: %q", diagnostics.String())
	}
}

// TestClientRetryAppliesBoundedServerRequestedDelay proves the WebDAV retry
// loop routes Retry-After through the shared bounded policy: the header is
// honored, and the wait cannot exceed the ceiling regardless of the value sent.
// internal/retry covers clamping of an excessive value directly, because
// observing the full ceiling here would mean a test that sleeps for it.
func TestClientRetryAppliesBoundedServerRequestedDelay(t *testing.T) {
	var attempts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if attempts.Add(1) == 1 {
			writer.Header().Set("Retry-After", "1")
			writer.WriteHeader(http.StatusTooManyRequests)
			return
		}
		writeDAVFile(writer, request.URL.Path, 4)
	}))
	defer server.Close()
	client := NewClient(Config{
		Server: server.URL, Username: "alice", Retries: 1,
		RetryWait: time.Millisecond,
	}, server.Client())
	started := time.Now()
	if _, err := client.Stat(context.Background(), "/report.txt"); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(started)
	if elapsed < time.Second || elapsed > retry.MaxDelay {
		t.Fatalf(
			"elapsed: got %v, want the one-second Retry-After honored within %v",
			elapsed, retry.MaxDelay,
		)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("attempts: got %d, want 2", got)
	}
}

// TestClientStopsWhenRetryAfterExceedsTheCeiling proves the WebDAV retry loop
// neither waits out an excessive Retry-After nor retries before it expires: the
// throttled endpoint must receive no follow-up request.
func TestClientStopsWhenRetryAfterExceedsTheCeiling(t *testing.T) {
	var attempts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		writer.Header().Set("Retry-After", "86400")
		writer.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	client := NewClient(Config{
		Server: server.URL, Username: "alice", Retries: 3,
		RetryWait: time.Millisecond,
	}, server.Client())
	started := time.Now()
	_, err := client.Stat(context.Background(), "/report.txt")
	var excessive *retry.DelayTooLongError
	if !errors.As(err, &excessive) {
		t.Fatalf("error: got %v, want a refused retry delay", err)
	}
	if elapsed := time.Since(started); elapsed > retry.MaxDelay {
		t.Fatalf("elapsed: got %v, want a prompt refusal", elapsed)
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts: got %d, want no follow-up request", got)
	}
}

func TestClientRetryHonorsContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client := NewClient(Config{
		Server: server.URL, Username: "alice", Retries: 3, RetryWait: time.Hour,
	}, server.Client())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Stat(ctx, "/report.txt"); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context cancellation", err)
	}
}

func TestUploadNoClobberAndVerification(t *testing.T) {
	var uploaded bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodPut:
			if !strings.Contains(request.URL.Path, ".ocis-cli-") {
				t.Fatalf("PUT path is not temporary: %s", request.URL.Path)
			}
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != "hello" {
				t.Fatalf("body: %q", body)
			}
			uploaded = true
			writer.WriteHeader(http.StatusCreated)
		case "PROPFIND":
			writeDAVFile(writer, request.URL.Path, 5)
		case "MOVE":
			if request.Header.Get("Overwrite") != "F" {
				t.Fatal("MOVE must fail closed with Overwrite: F")
			}
			if !strings.HasSuffix(
				request.Header.Get("Destination"), "/report.txt",
			) {
				t.Fatalf(
					"unexpected destination: %s",
					request.Header.Get("Destination"),
				)
			}
			writer.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected method: %s", request.Method)
		}
	}))
	defer server.Close()
	local := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(local, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	client := NewClient(Config{Server: server.URL, Username: "alice"}, server.Client())
	if err := client.UploadWithOptions(
		context.Background(), local, "/report.txt",
		TransferOptions{
			NoClobber: true, Verify: true,
			TUS: TUSCapabilities{
				Version: "1.0.0", Resumable: "1.0.0",
				Extensions: []string{"creation"}, MaxChunkSize: 5,
			},
		},
	); err != nil {
		t.Fatal(err)
	}
	if !uploaded {
		t.Fatal("upload was not received")
	}
}

func TestUploadEmptyFileUsesFixedLengthBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.Method != http.MethodPut {
			t.Fatalf("unexpected method: %s", request.Method)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil || len(body) != 0 || request.ContentLength != 0 ||
			len(request.TransferEncoding) != 0 {
			t.Fatalf(
				"body=%q content-length=%d transfer-encoding=%v err=%v",
				body, request.ContentLength, request.TransferEncoding, err,
			)
		}
		writer.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	local := filepath.Join(t.TempDir(), "empty.txt")
	if err := os.WriteFile(local, nil, 0600); err != nil {
		t.Fatal(err)
	}
	client := NewClient(
		Config{Server: server.URL, Username: "alice"}, server.Client(),
	)
	if err := client.Upload(
		context.Background(), local, "/empty.txt",
	); err != nil {
		t.Fatal(err)
	}
}

func TestUploadReturnsTypedConflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodPut:
			writer.WriteHeader(http.StatusCreated)
		case "MOVE":
			writer.WriteHeader(http.StatusPreconditionFailed)
			_, _ = io.WriteString(writer, "already exists")
		case "PROPFIND":
			writeDAVFile(writer, request.URL.Path, 5)
		case http.MethodDelete:
			writer.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected method: %s", request.Method)
		}
	}))
	defer server.Close()
	local := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(local, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	client := NewClient(Config{Server: server.URL, Username: "alice"}, server.Client())
	err := client.UploadWithOptions(
		context.Background(), local, "/report.txt", TransferOptions{NoClobber: true},
	)
	if StatusCode(err) != http.StatusPreconditionFailed {
		t.Fatalf("status: got %d from %v", StatusCode(err), err)
	}
}

func TestDownloadResumesPartFileAndVerifies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodGet:
			if got := request.Header.Get("Range"); got != "bytes=6-" {
				t.Fatalf("range: got %q", got)
			}
			writer.Header().Set("Content-Range", "bytes 6-10/11")
			writer.WriteHeader(http.StatusPartialContent)
			_, _ = io.WriteString(writer, "world")
		case "PROPFIND":
			writeDAVFile(writer, request.URL.Path, 11)
		default:
			t.Fatalf("unexpected method: %s", request.Method)
		}
	}))
	defer server.Close()
	local := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(local+".part", []byte("hello "), 0600); err != nil {
		t.Fatal(err)
	}
	client := NewClient(Config{Server: server.URL, Username: "alice"}, server.Client())
	if err := client.DownloadWithOptions(
		context.Background(), "/report.txt", local,
		TransferOptions{Resume: true, Verify: true},
	); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(local)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "hello world" {
		t.Fatalf("download: got %q", got)
	}
	if _, err := os.Stat(local + ".part"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("part file remains: %v", err)
	}
}

func TestDownloadToWriterStreamsAndRetriesResponseStatus(t *testing.T) {
	var attempts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		if attempts.Add(1) == 1 {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writer.Header().Set("Content-Length", "5")
		_, _ = io.WriteString(writer, "hello")
	}))
	defer server.Close()
	client := NewClient(Config{
		Server: server.URL, Username: "alice", AuthType: "basic",
		Password: "secret", Retries: 1, RetryWait: time.Millisecond,
	}, server.Client())
	var output bytes.Buffer
	if err := client.DownloadToWriter(
		context.Background(), "/report.txt", &output,
	); err != nil {
		t.Fatal(err)
	}
	if output.String() != "hello" || attempts.Load() != 2 {
		t.Fatalf("output=%q attempts=%d", output.String(), attempts.Load())
	}
}

func TestDownloadToWriterReturnsHTTPErrorWithoutWriting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		writer.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(writer, "missing")
	}))
	defer server.Close()
	client := NewClient(
		Config{Server: server.URL, Username: "alice"}, server.Client(),
	)
	var output bytes.Buffer
	err := client.DownloadToWriter(
		context.Background(), "/missing.txt", &output,
	)
	if StatusCode(err) != http.StatusNotFound || output.Len() != 0 {
		t.Fatalf("error=%v output=%q", err, output.String())
	}
}

func TestDownloadIntoExistingDirectory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", request.Method)
		}
		writer.Header().Set("Content-Length", "5")
		_, _ = io.WriteString(writer, "hello")
	}))
	defer server.Close()
	destination := t.TempDir()
	client := NewClient(Config{Server: server.URL, Username: "alice"}, server.Client())
	if err := client.DownloadWithOptions(
		context.Background(), "/reports/report.txt", destination,
		TransferOptions{Resume: false},
	); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "report.txt"))
	if err != nil || string(data) != "hello" {
		t.Fatalf("download: %q, %v", data, err)
	}
}

func TestDownloadRejectsInvalidResumeRange(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Range") != "bytes=4-" {
			t.Fatalf("range: %q", request.Header.Get("Range"))
		}
		writer.Header().Set("Content-Range", "bytes 3-4/5")
		writer.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(writer, "o")
	}))
	defer server.Close()
	local := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(local+".part", []byte("hell"), 0600); err != nil {
		t.Fatal(err)
	}
	client := NewClient(Config{Server: server.URL, Username: "alice"}, server.Client())
	err := client.DownloadWithOptions(
		context.Background(), "/report.txt", local, TransferOptions{Resume: true},
	)
	if err == nil || !strings.Contains(err.Error(), "invalid Content-Range") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDownloadRetriesTemporaryResponse(t *testing.T) {
	var attempts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writer.Header().Set("Content-Length", "5")
		_, _ = io.WriteString(writer, "hello")
	}))
	defer server.Close()
	local := filepath.Join(t.TempDir(), "report.txt")
	client := NewClient(Config{
		Server: server.URL, Username: "alice", Retries: 1,
		RetryWait: time.Millisecond,
	}, server.Client())
	if err := client.DownloadWithOptions(
		context.Background(), "/report.txt", local, TransferOptions{},
	); err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts: got %d, want 2", attempts.Load())
	}
}

func TestDownloadNoClobber(t *testing.T) {
	local := filepath.Join(t.TempDir(), "existing.txt")
	if err := os.WriteFile(local, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	client := NewClient(Config{Server: "https://invalid.example", Username: "alice"}, nil)
	err := client.DownloadWithOptions(
		context.Background(), "/report.txt", local, TransferOptions{NoClobber: true},
	)
	if StatusCode(err) != http.StatusPreconditionFailed {
		t.Fatalf("status: got %d from %v", StatusCode(err), err)
	}
	data, readErr := os.ReadFile(local)
	if readErr != nil || string(data) != "keep" {
		t.Fatalf("destination changed: %q, %v", data, readErr)
	}
}

func TestParseContentRange(t *testing.T) {
	start, size, ok := parseContentRange("bytes 6-10/11")
	if !ok || start != 6 || size != 11 {
		t.Fatalf("parsed: %d %d %t", start, size, ok)
	}
	for _, value := range []string{"", "items 1-2/3", "bytes bad/3", "bytes 1-2/bad"} {
		if _, _, ok := parseContentRange(value); ok {
			t.Errorf("accepted invalid range %q", value)
		}
	}
}

func TestProgressReaderAndWriterReportCompletedBytes(t *testing.T) {
	var readProgress int64
	reader := &progressReader{
		reader: io.NopCloser(strings.NewReader("hello")),
		report: func(completed int64) { readProgress = completed },
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" || readProgress != 5 {
		t.Fatalf("read: %q, progress %d", data, readProgress)
	}

	var destination strings.Builder
	var writeProgress int64
	writer := &progressWriter{
		writer: &destination, completed: 2,
		report: func(completed int64) { writeProgress = completed },
	}
	if _, err := writer.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	if destination.String() != "abc" || writeProgress != 5 {
		t.Fatalf("write: %q, progress %d", destination.String(), writeProgress)
	}
}

func TestClientAuthenticationAndHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		user, password, ok := request.BasicAuth()
		if !ok || user != "alice" || password != "secret" {
			t.Fatalf("basic auth: %q %q %t", user, password, ok)
		}
		if request.Header.Get("User-Agent") != "ocis-cli/test" {
			t.Fatalf("user agent: %q", request.Header.Get("User-Agent"))
		}
		if request.Header.Get("Depth") != "1" {
			t.Fatalf("depth: %q", request.Header.Get("Depth"))
		}
		writeDAVCollection(writer, request.URL.Path)
	}))
	defer server.Close()
	client := NewClient(Config{
		Server: server.URL, Username: "alice", AuthType: "basic",
		Password: "secret", UserAgent: "ocis-cli/test",
	}, server.Client())
	if _, err := client.List(context.Background(), "/"); err != nil {
		t.Fatal(err)
	}
}

func TestCollectionAndMutationMethods(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		methods = append(methods, request.Method)
		switch request.Method {
		case "MKCOL":
			writer.WriteHeader(http.StatusCreated)
		case "MOVE", "COPY":
			if request.Header.Get("Destination") == "" {
				t.Fatal("missing destination")
			}
			if request.Header.Get("Overwrite") != "F" {
				t.Fatalf("overwrite: %q", request.Header.Get("Overwrite"))
			}
			writer.WriteHeader(http.StatusCreated)
		case "PROPFIND":
			writeDAVFile(writer, request.URL.Path, 5)
		case http.MethodDelete:
			if request.Header.Get("If-Match") != `"copy-etag"` {
				t.Fatalf("delete If-Match: %q", request.Header.Get("If-Match"))
			}
			writer.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected method: %s", request.Method)
		}
	}))
	defer server.Close()
	client := NewClient(Config{Server: server.URL, Username: "alice"}, server.Client())
	ctx := context.Background()
	if err := client.Mkdir(ctx, "/archive"); err != nil {
		t.Fatal(err)
	}
	if err := client.Move(ctx, "/report.txt", "/archive/report.txt", false); err != nil {
		t.Fatal(err)
	}
	if err := client.Copy(ctx, "/archive/report.txt", "/copy.txt", false); err != nil {
		t.Fatal(err)
	}
	if err := client.RemoveWithOptions(
		ctx, "/copy.txt",
		RemoveOptions{
			ExpectedETag: `"copy-etag"`,
		},
	); err != nil {
		t.Fatal(err)
	}
	if len(methods) != 5 {
		t.Fatalf("methods: %#v", methods)
	}
}

func TestRemoveDirectoryRequiresRecursive(t *testing.T) {
	deleted := false
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case "PROPFIND":
			writeDAVCollection(writer, request.URL.Path)
		case http.MethodDelete:
			deleted = true
			writer.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	client := NewClient(Config{Server: server.URL, Username: "alice"}, server.Client())
	err := client.Remove(context.Background(), "/archive", false)
	if !errors.Is(err, ErrRemoteIsDirectory) || deleted {
		t.Fatalf("guard failed: %v, deleted=%t", err, deleted)
	}
}

func TestConvenienceUploadAndDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodPut:
			writer.WriteHeader(http.StatusCreated)
		case http.MethodGet:
			writer.Header().Set("Content-Length", "5")
			_, _ = io.WriteString(writer, "hello")
		default:
			t.Fatalf("unexpected method: %s", request.Method)
		}
	}))
	defer server.Close()
	root := t.TempDir()
	source := filepath.Join(root, "source.txt")
	destination := filepath.Join(root, "destination.txt")
	if err := os.WriteFile(source, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	client := NewClient(Config{Server: server.URL, Username: "alice"}, server.Client())
	if err := client.Upload(context.Background(), source, "/source.txt"); err != nil {
		t.Fatal(err)
	}
	if err := client.Download(context.Background(), "/source.txt", destination); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(destination); err != nil || string(data) != "hello" {
		t.Fatalf("download: %q, %v", data, err)
	}
}

func TestBearerAuthentication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("authorization: %q", request.Header.Get("Authorization"))
		}
		writeDAVFile(writer, request.URL.Path, 1)
	}))
	defer server.Close()
	client := NewClient(Config{
		Server: server.URL, Username: "alice", AuthType: "oidc", AccessToken: "token",
	}, server.Client())
	if _, err := client.Stat(context.Background(), "/file"); err != nil {
		t.Fatal(err)
	}
}

func TestTransferETagPreconditions(t *testing.T) {
	var putMatched, getMatched bool
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch request.Method {
		case http.MethodPut:
			putMatched = request.Header.Get("If-Match") == `"destination"`
			writer.WriteHeader(http.StatusNoContent)
		case http.MethodGet:
			getMatched = request.Header.Get("If-Match") == `"source"`
			writer.Header().Set("Content-Length", "4")
			_, _ = io.WriteString(writer, "data")
		default:
			t.Fatalf("method: %s", request.Method)
		}
	}))
	defer server.Close()
	client := NewClient(Config{Server: server.URL}, server.Client())
	source := filepath.Join(t.TempDir(), "source.txt")
	if err := os.WriteFile(source, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := client.UploadWithOptions(
		context.Background(), source, "/remote.txt",
		TransferOptions{
			ExpectedETag: `"destination"`,
			TUS: TUSCapabilities{
				Version: "1.0.0", Resumable: "1.0.0",
				Extensions: []string{"creation"}, MaxChunkSize: 1024,
			},
		},
	); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "destination.txt")
	if err := client.DownloadWithOptions(
		context.Background(), "/remote.txt", destination,
		TransferOptions{ExpectedETag: `"source"`},
	); err != nil {
		t.Fatal(err)
	}
	if !putMatched || !getMatched {
		t.Fatalf("put=%t get=%t", putMatched, getMatched)
	}
}

func TestSpaceEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.URL.Path != "/dav/spaces/storage-id/report.txt" {
			t.Fatalf("path: %s", request.URL.Path)
		}
		writeDAVFile(writer, request.URL.Path, 1)
	}))
	defer server.Close()
	client := NewClient(Config{
		Server: server.URL, Username: "alice", AuthType: "oidc",
		AccessToken: "token", SpaceID: "storage-id",
	}, server.Client())
	if _, err := client.Stat(context.Background(), "/report.txt"); err != nil {
		t.Fatal(err)
	}
}

func TestCapabilitiesParsesRepeatedHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodOptions {
			t.Fatalf("method: %s", request.Method)
		}
		writer.Header().Add("DAV", "1, 3")
		writer.Header().Add("DAV", "extended-mkcol")
		writer.Header().Set("Allow", "OPTIONS, PROPFIND")
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client := NewClient(Config{Server: server.URL, Username: "alice"}, server.Client())
	capabilities, err := client.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(capabilities.DAV) != 3 || len(capabilities.Allow) != 2 {
		t.Fatalf("capabilities: %#v", capabilities)
	}
}

func TestEscapeRemoteNormalizesAndEscapesSegments(t *testing.T) {
	tests := map[string]string{
		"/":                        "/",
		"reports/../hello.txt":     "/hello.txt",
		"/Project docs/a+b #1.txt": "/Project%20docs/a+b%20%231.txt",
	}
	for input, want := range tests {
		if got := escapeRemote(input); got != want {
			t.Errorf("escapeRemote(%q): got %q, want %q", input, got, want)
		}
	}
}

func writeDAVFile(writer http.ResponseWriter, href string, size int64) {
	writer.WriteHeader(http.StatusMultiStatus)
	_, _ = io.WriteString(writer,
		`<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"><d:response><d:href>`+
			href+`</d:href><d:propstat><d:status>HTTP/1.1 200 OK</d:status><d:prop>`+
			`<d:displayname>report.txt</d:displayname><d:getcontentlength>`+
			strconv.FormatInt(size, 10)+
			`</d:getcontentlength><d:getetag>"etag"</d:getetag><d:resourcetype/>`+
			`</d:prop></d:propstat></d:response></d:multistatus>`)
}

func writeDAVCollection(writer http.ResponseWriter, href string) {
	writer.WriteHeader(http.StatusMultiStatus)
	_, _ = io.WriteString(writer,
		`<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"><d:response><d:href>`+
			href+`</d:href><d:propstat><d:status>HTTP/1.1 200 OK</d:status><d:prop>`+
			`<d:resourcetype><d:collection/></d:resourcetype>`+
			`</d:prop></d:propstat></d:response></d:multistatus>`)
}
