package httpapi

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mzner/ocis-cli/internal/logging"
	"github.com/mzner/ocis-cli/internal/retry"
)

func TestClientAuthenticatesAndRetries(t *testing.T) {
	var attempts atomic.Int64
	var diagnostics bytes.Buffer
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		username, password, ok := request.BasicAuth()
		if !ok || username != "alice" || password != "secret" {
			t.Fatalf("authentication: %q %q %t", username, password, ok)
		}
		if request.Header.Get("User-Agent") != "ocis-cli/test" {
			t.Fatalf("user agent: %q", request.Header.Get("User-Agent"))
		}
		body, err := io.ReadAll(request.Body)
		if err != nil || string(body) != "payload" {
			t.Fatalf("body: %q, %v", body, err)
		}
		if attempts.Add(1) == 1 {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(writer, "ok")
	}))
	defer server.Close()
	client := NewClient(Config{
		Server: server.URL, Username: "alice", AuthType: "basic",
		Password: "secret", UserAgent: "ocis-cli/test", Retries: 1,
		RetryWait: time.Millisecond, Logger: logging.NewText(&diagnostics),
	}, server.Client())
	response, err := client.Do(
		context.Background(), http.MethodPost, "/resource",
		[]byte("payload"), http.Header{"Content-Type": {"text/plain"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if attempts.Load() != 2 ||
		!strings.Contains(diagnostics.String(), "attempt=2") {
		t.Fatalf("attempts=%d diagnostics=%q", attempts.Load(), diagnostics.String())
	}
}

func TestBearerAuthenticationAndResponseError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("authorization: %q", request.Header.Get("Authorization"))
		}
		writer.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(writer, "denied")
	}))
	defer server.Close()
	client := NewClient(Config{
		Server: server.URL, AuthType: "oidc", AccessToken: "token",
	}, server.Client())
	response, err := client.Do(
		context.Background(), http.MethodGet, "resource", nil, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	responseErr := ResponseError(response)
	statusErr, ok := responseErr.(interface{ HTTPStatusCode() int })
	if !ok || statusErr.HTTPStatusCode() != http.StatusForbidden ||
		!strings.Contains(responseErr.Error(), "denied") {
		t.Fatalf("response error: %v", responseErr)
	}
}

func TestResponseErrorExtractsDAVMessage(t *testing.T) {
	response := &http.Response{
		Status: "409 Conflict", StatusCode: http.StatusConflict,
		Body: io.NopCloser(strings.NewReader(
			`<d:error xmlns:d="DAV:" xmlns:s="http://sabredav.org/ns">` +
				`<s:message>Destination is missing</s:message></d:error>`,
		)),
	}
	if got := ResponseError(response).Error(); got !=
		"409 Conflict: Destination is missing" {
		t.Fatalf("response error: %s", got)
	}
}

func TestResponseErrorExplainsOcisMFARequirement(t *testing.T) {
	response := &http.Response{
		Status: "401 Unauthorized", StatusCode: http.StatusUnauthorized,
		Header: http.Header{"X-Ocis-Mfa-Required": {"true"}},
		Body:   io.NopCloser(strings.NewReader("Unauthorized")),
	}
	err := ResponseError(response)
	if got := err.Error(); got !=
		"401 Unauthorized: multi-factor authentication is required by the server" {
		t.Fatalf("response error: %s", got)
	}
	required, ok := err.(interface{ RequiresMFA() bool })
	if !ok || !required.RequiresMFA() {
		t.Fatalf("MFA marker missing: %#v", err)
	}
}

// TestRetryAppliesBoundedServerRequestedDelay proves the retry loop routes
// Retry-After through the shared bounded policy rather than sleeping for a
// server-chosen duration. An excessive header value is clamped, so the call
// completes far sooner than the day the server asked for; the exact ceiling is
// covered by the internal/retry tests.
func TestRetryAppliesBoundedServerRequestedDelay(t *testing.T) {
	var attempts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		if attempts.Add(1) == 1 {
			writer.Header().Set("Retry-After", "1")
			writer.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(writer, "ok")
	}))
	defer server.Close()
	client := NewClient(Config{
		Server: server.URL, Retries: 1, RetryWait: time.Millisecond,
	}, server.Client())
	started := time.Now()
	response, err := client.Do(context.Background(), http.MethodGet, "/", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	elapsed := time.Since(started)
	if elapsed < time.Second || elapsed > retry.MaxDelay {
		t.Fatalf(
			"elapsed: got %v, want the one-second Retry-After honored within %v",
			elapsed, retry.MaxDelay,
		)
	}
}

func TestRetryHonorsContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client := NewClient(Config{
		Server: server.URL, Retries: 1, RetryWait: time.Hour,
	}, server.Client())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Do(ctx, http.MethodGet, "/", nil, nil); err == nil {
		t.Fatal("cancelled request succeeded")
	}
}
