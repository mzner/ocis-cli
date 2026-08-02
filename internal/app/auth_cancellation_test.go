package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mzner/ocis-cli/internal/auth"
)

func TestOIDCLoginCancellationClosesCallbackListener(t *testing.T) {
	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.URL.Path != "/.well-known/openid-configuration" {
			t.Fatalf("unexpected request: %s", request.URL.Path)
		}
		_ = json.NewEncoder(writer).Encode(auth.Metadata{
			Issuer: provider.URL, AuthorizationEndpoint: provider.URL + "/authorize",
			TokenEndpoint:    provider.URL + "/token",
			UserInfoEndpoint: provider.URL + "/userinfo",
		})
	}))
	defer provider.Close()
	ctx, cancel := context.WithCancel(context.Background())
	writer := &cancelAuthorizationWriter{cancel: cancel}
	err := oidcLogin(ctx, &profile{
		Server: provider.URL, ClientID: "client", Insecure: true,
	}, true, "", RunOptions{
		Out: writer, Err: io.Discard, Timeout: time.Second,
	}.normalized())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context cancellation", err)
	}
	callback := writer.callbackURL()
	if callback == "" {
		t.Fatal("authorization URL did not expose the callback URI")
	}
	request, requestErr := http.NewRequest(http.MethodGet, callback, nil)
	if requestErr != nil {
		t.Fatal(requestErr)
	}
	client := &http.Client{Timeout: 200 * time.Millisecond}
	if response, requestErr := client.Do(request); requestErr == nil {
		_ = response.Body.Close()
		t.Fatal("callback listener remained reachable after cancellation")
	}
}

type cancelAuthorizationWriter struct {
	bytes.Buffer
	cancel   context.CancelFunc
	once     sync.Once
	callback string
}

func (writer *cancelAuthorizationWriter) Write(data []byte) (int, error) {
	count, err := writer.Buffer.Write(data)
	target := strings.TrimSpace(string(data))
	parsed, parseErr := url.Parse(target)
	if parseErr == nil && parsed.Scheme != "" &&
		strings.HasSuffix(parsed.Path, "/authorize") {
		writer.once.Do(func() {
			writer.callback = parsed.Query().Get("redirect_uri")
			writer.cancel()
		})
	}
	return count, err
}

func (writer *cancelAuthorizationWriter) callbackURL() string {
	return writer.callback
}
