package webdav

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryUploadSessionStore struct {
	mu       sync.Mutex
	sessions map[string]UploadSession
}

func (store *memoryUploadSessionStore) Load(
	key string,
) (UploadSession, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	session, found := store.sessions[key]
	return session, found, nil
}

func (store *memoryUploadSessionStore) Save(
	key string, session UploadSession,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.sessions[key] = session
	return nil
}

func (store *memoryUploadSessionStore) Delete(key string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.sessions, key)
	return nil
}

func TestTUSUploadResumesAcrossClientInvocations(t *testing.T) {
	var uploaded bytes.Buffer
	var postCount int
	expiration := time.Now().Add(time.Hour).Unix()
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		username, password, ok := request.BasicAuth()
		if !ok || username != "alice" || password != "secret" {
			t.Fatalf("authentication: %q", request.Header.Get("Authorization"))
		}
		switch {
		case request.Method == http.MethodOptions:
			writer.Header().Set("Tus-Version", "1.0.0")
			writer.Header().Set(
				"Tus-Extension", "creation,creation-with-upload,expiration",
			)
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodPost &&
			request.Header.Get("Tus-Resumable") == "1.0.0":
			postCount++
			if request.URL.Path !=
				"/remote.php/dav/files/alice/reports" {
				t.Fatalf("creation path: %s", request.URL.Path)
			}
			wantFilename := base64.StdEncoding.EncodeToString(
				[]byte("report.bin"),
			)
			if !strings.Contains(
				request.Header.Get("Upload-Metadata"),
				"filename "+wantFilename,
			) {
				t.Fatalf(
					"metadata: %q",
					request.Header.Get("Upload-Metadata"),
				)
			}
			writer.Header().Set("Location", "/data/upload-session")
			writer.WriteHeader(http.StatusCreated)
		case request.Method == http.MethodHead:
			writer.Header().Set("Tus-Resumable", "1.0.0")
			writer.Header().Set(
				"Upload-Offset", strconv.Itoa(uploaded.Len()),
			)
			writer.Header().Set("Upload-Length", "10")
			writer.WriteHeader(http.StatusOK)
		case request.Method == http.MethodPatch:
			if request.URL.Path != "/data/upload-session" {
				t.Fatalf("patch path: %s", request.URL.Path)
			}
			wantOffset := strconv.Itoa(uploaded.Len())
			if request.Header.Get("Upload-Offset") != wantOffset {
				t.Fatalf(
					"offset: got %q, want %q",
					request.Header.Get("Upload-Offset"), wantOffset,
				)
			}
			data, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = uploaded.Write(data)
			writer.Header().Set("Tus-Resumable", "1.0.0")
			writer.Header().Set(
				"Upload-Offset", strconv.Itoa(uploaded.Len()),
			)
			// oCIS emits a Unix timestamp although the TUS specification uses
			// an HTTP date. The compatibility transport normalizes it.
			writer.Header().Set(
				"Upload-Expires", strconv.FormatInt(expiration, 10),
			)
			writer.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf(
				"unexpected request: %s %s",
				request.Method, request.URL.Path,
			)
		}
	}))
	defer server.Close()

	local := filepath.Join(t.TempDir(), "report.bin")
	if err := os.WriteFile(local, []byte("0123456789"), 0600); err != nil {
		t.Fatal(err)
	}
	store := &memoryUploadSessionStore{
		sessions: map[string]UploadSession{},
	}
	config := Config{
		Server: server.URL, Username: "alice", AuthType: "basic",
		AccountID: "alice-account", Password: "secret", Uploads: store,
	}
	capabilities := TUSCapabilities{
		Version: "1.0.0", Resumable: "1.0.0",
		Extensions:   []string{"creation", "creation-with-upload"},
		MaxChunkSize: 5,
	}

	firstCtx, cancel := context.WithCancel(context.Background())
	first := NewClient(config, server.Client())
	err := first.UploadWithOptions(
		firstCtx, local, "/reports/report.bin",
		TransferOptions{
			TUS: capabilities,
			Progress: func(offset int64) {
				if offset == 5 {
					cancel()
				}
			},
		},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("first upload: got %v, want context cancellation", err)
	}
	if uploaded.String() != "01234" {
		t.Fatalf("first upload content: %q", uploaded.String())
	}
	if len(store.sessions) != 1 {
		t.Fatalf("saved sessions: %#v", store.sessions)
	}
	for _, session := range store.sessions {
		if session.ExpiresAt != expiration ||
			!strings.Contains(session.UploadURL, "/data/upload-session") {
			t.Fatalf("saved session: %#v", session)
		}
	}

	second := NewClient(config, server.Client())
	if err := second.UploadWithOptions(
		context.Background(), local, "/reports/report.bin",
		TransferOptions{TUS: capabilities},
	); err != nil {
		t.Fatal(err)
	}
	if uploaded.String() != "0123456789" {
		t.Fatalf("resumed upload content: %q", uploaded.String())
	}
	if postCount != 1 {
		t.Fatalf("upload sessions created: got %d, want 1", postCount)
	}
	if len(store.sessions) != 0 {
		t.Fatalf("completed session remains: %#v", store.sessions)
	}
}

func TestTUSUploadSessionKeyIsAccountBound(t *testing.T) {
	first := NewClient(Config{
		Server: "https://cloud.test", AccountID: "alice", SpaceID: "space",
	}, nil)
	second := NewClient(Config{
		Server: "https://cloud.test", AccountID: "bob", SpaceID: "space",
	}, nil)

	if first.uploadSessionKey("/report.bin") ==
		second.uploadSessionKey("/report.bin") {
		t.Fatal("upload session keys for different accounts must differ")
	}
}

func TestTUSCapabilitiesRequireCreationAndChunkSize(t *testing.T) {
	valid := TUSCapabilities{
		Version: "1.0.0", Resumable: "1.0.0",
		Extensions: []string{"creation"}, MaxChunkSize: 10,
	}
	if !valid.Enabled() {
		t.Fatal("complete TUS capabilities should be enabled")
	}
	for name, mutate := range map[string]func(*TUSCapabilities){
		"version":    func(value *TUSCapabilities) { value.Version = "0.2.2" },
		"resumable":  func(value *TUSCapabilities) { value.Resumable = "" },
		"creation":   func(value *TUSCapabilities) { value.Extensions = nil },
		"chunk size": func(value *TUSCapabilities) { value.MaxChunkSize = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			capabilities := valid
			mutate(&capabilities)
			if capabilities.Enabled() {
				t.Fatal("incomplete TUS capabilities should be disabled")
			}
		})
	}
}

func TestTUSURLValidation(t *testing.T) {
	client := NewClient(Config{Server: "https://cloud.test"}, nil)
	for _, target := range []string{
		"/data/upload", "https://cloud.test/data/upload",
		"https://cloud.test:443/data/upload",
	} {
		resolved, err := client.resolveTUSURL(target)
		if err != nil || !strings.Contains(resolved, "/data/upload") {
			t.Fatalf("resolve %q: %q, %v", target, resolved, err)
		}
	}
	for _, target := range []string{
		"file:///tmp/upload", "https://other.test/data/upload",
		"https://cloud.test:8443/data/upload",
	} {
		if _, err := client.resolveTUSURL(target); err == nil {
			t.Fatalf("resolve %q should fail", target)
		}
	}
}

func TestTUSUploadRejectsCrossOriginLocation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch request.Method {
		case http.MethodOptions:
			writer.Header().Set("Tus-Version", "1.0.0")
			writer.Header().Set("Tus-Extension", "creation")
			writer.WriteHeader(http.StatusNoContent)
		case http.MethodPost:
			writer.Header().Set(
				"Location", "https://malicious.example/upload/token",
			)
			writer.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected method: %s", request.Method)
		}
	}))
	defer server.Close()
	local := filepath.Join(t.TempDir(), "report.bin")
	if err := os.WriteFile(local, []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}
	client := NewClient(Config{
		Server: server.URL, Username: "alice", AuthType: "basic",
		Password: "secret",
	}, server.Client())
	err := client.UploadWithOptions(
		context.Background(), local, "/report.bin",
		TransferOptions{TUS: TUSCapabilities{
			Version: "1.0.0", Resumable: "1.0.0",
			Extensions: []string{"creation"}, MaxChunkSize: 5,
		}},
	)
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("unexpected error: %v", err)
	}
}
