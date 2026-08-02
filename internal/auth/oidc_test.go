package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestDiscoverRequiresEndpoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"issuer":"https://issuer.example"}`))
	}))
	defer server.Close()
	if _, err := Discover(context.Background(), server.Client(), server.URL); err == nil {
		t.Fatal("Discover accepted incomplete metadata")
	}
}

func TestDiscoverRejectsNonHTTPEndpoints(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Metadata)
	}{
		{
			name: "issuer",
			mutate: func(metadata *Metadata) {
				metadata.Issuer = "custom:issuer"
			},
		},
		{
			name: "authorization endpoint",
			mutate: func(metadata *Metadata) {
				metadata.AuthorizationEndpoint = "file:///tmp/authorization"
			},
		},
		{
			name: "token endpoint",
			mutate: func(metadata *Metadata) {
				metadata.TokenEndpoint = "custom:token"
			},
		},
		{
			name: "userinfo endpoint",
			mutate: func(metadata *Metadata) {
				metadata.UserInfoEndpoint = "/relative/userinfo"
			},
		},
		{
			name: "registration endpoint",
			mutate: func(metadata *Metadata) {
				metadata.RegistrationEndpoint = "file:///tmp/register"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(
				writer http.ResponseWriter, _ *http.Request,
			) {
				metadata := Metadata{
					Issuer:                "https://idp.example",
					AuthorizationEndpoint: "https://idp.example/authorize",
					TokenEndpoint:         "https://idp.example/token",
					UserInfoEndpoint:      "https://idp.example/userinfo",
				}
				test.mutate(&metadata)
				_ = json.NewEncoder(writer).Encode(metadata)
			}))
			defer server.Close()
			if _, err := Discover(
				context.Background(), server.Client(), server.URL,
			); err == nil || !strings.Contains(err.Error(), "OIDC") {
				t.Fatalf("Discover error: %v", err)
			}
		})
	}
}

func TestValidateTransportSecurity(t *testing.T) {
	metadata := Metadata{
		Issuer:                "http://idp.example",
		AuthorizationEndpoint: "http://idp.example/authorize",
		TokenEndpoint:         "http://idp.example/token",
		UserInfoEndpoint:      "http://idp.example/userinfo",
		RegistrationEndpoint:  "http://idp.example/register",
	}
	if err := ValidateTransportSecurity(metadata, false); err == nil {
		t.Fatal("clear-text metadata was accepted without explicit insecure mode")
	}
	if err := ValidateTransportSecurity(metadata, true); err != nil {
		t.Fatalf("explicit insecure development metadata was rejected: %v", err)
	}
}

func TestDiscoverAndUserInfo(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(writer).Encode(Metadata{
				Issuer: server.URL, AuthorizationEndpoint: server.URL + "/authorize",
				TokenEndpoint: server.URL + "/token", UserInfoEndpoint: server.URL + "/userinfo",
			})
		case "/userinfo":
			if request.Header.Get("Authorization") != "Bearer access" {
				t.Fatalf("authorization: %q", request.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(writer).Encode(UserInfo{PreferredUsername: "alice"})
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()
	metadata, err := Discover(context.Background(), server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	info, err := FetchUserInfo(
		context.Background(), server.Client(), metadata.UserInfoEndpoint, "access",
	)
	if err != nil {
		t.Fatal(err)
	}
	if info.PreferredUsername != "alice" {
		t.Fatalf("userinfo: %#v", info)
	}
}

func TestExchangeCodeUsesPKCEAndClientAuthentication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		user, secret, ok := request.BasicAuth()
		if !ok || user != "client" || secret != "secret" {
			t.Fatalf("client authentication: %q %q %t", user, secret, ok)
		}
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		want := url.Values{
			"grant_type": {"authorization_code"}, "code": {"code"},
			"redirect_uri": {"http://127.0.0.1/callback"}, "client_id": {"client"},
			"code_verifier": {"verifier"},
		}
		for key := range want {
			if request.Form.Get(key) != want.Get(key) {
				t.Fatalf("%s: got %q, want %q", key, request.Form.Get(key), want.Get(key))
			}
		}
		_ = json.NewEncoder(writer).Encode(Token{
			AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 3600,
		})
	}))
	defer server.Close()
	token, err := ExchangeCode(
		context.Background(), server.Client(), server.URL, "client", "secret",
		"code", "verifier", "http://127.0.0.1/callback",
	)
	if err != nil {
		t.Fatal(err)
	}
	if token.AccessToken != "access" {
		t.Fatalf("token: %#v", token)
	}
}

func TestRefreshReportsOAuthError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(writer, `{"error":"invalid_grant","error_description":"expired"}`)
	}))
	defer server.Close()
	_, err := Refresh(
		context.Background(), server.Client(), server.URL, "client", "", "refresh",
	)
	if err == nil || !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("unexpected error: %v", err)
	}
	var oauthErr *OAuthError
	if !errors.As(err, &oauthErr) || oauthErr.Code != "invalid_grant" {
		t.Fatalf("typed OAuth error was not preserved: %T %v", err, err)
	}
}

func TestFetchUserInfoReportsBoundedHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(writer, "denied")
	}))
	defer server.Close()
	_, err := FetchUserInfo(context.Background(), server.Client(), server.URL, "bad")
	if err == nil || !strings.Contains(err.Error(), "401 Unauthorized: denied") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDiscoverUsesContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Discover(ctx, http.DefaultClient, "https://example.invalid"); err == nil {
		t.Fatal("Discover succeeded with canceled context")
	}
}
