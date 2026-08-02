package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegisterClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.Method != http.MethodPost ||
			request.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("request: %s %q", request.Method, request.Header.Get("Content-Type"))
		}
		var registration ClientRegistration
		if err := json.NewDecoder(request.Body).Decode(&registration); err != nil {
			t.Fatal(err)
		}
		if registration.ApplicationType != "native" ||
			registration.TokenEndpointAuthMethod != "client_secret_basic" ||
			len(registration.RedirectURIs) != 1 ||
			registration.RedirectURIs[0] != "http://127.0.0.1" {
			t.Fatalf("registration: %#v", registration)
		}
		writer.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(writer).Encode(RegisteredClient{
			ClientID: "generated-client", ClientSecret: "generated-secret",
		})
	}))
	defer server.Close()

	registered, err := RegisterClient(
		context.Background(), server.Client(), server.URL,
	)
	if err != nil {
		t.Fatal(err)
	}
	if registered.ClientID != "generated-client" ||
		registered.ClientSecret != "generated-secret" {
		t.Fatalf("registered client: %#v", registered)
	}
}

func TestRegisterClientRejectsMalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		_, _ = io.WriteString(writer, `{}`)
	}))
	defer server.Close()
	if _, err := RegisterClient(
		context.Background(), server.Client(), server.URL,
	); err == nil || !strings.Contains(err.Error(), "no client ID") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegisterClientReportsHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		writer.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(writer, "registration disabled")
	}))
	defer server.Close()
	if _, err := RegisterClient(
		context.Background(), server.Client(), server.URL,
	); err == nil || !strings.Contains(err.Error(), "registration disabled") {
		t.Fatalf("unexpected error: %v", err)
	}
}
