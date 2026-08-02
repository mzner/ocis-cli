package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mzner/ocis-cli/internal/apperror"
	"github.com/mzner/ocis-cli/internal/auth"
	appconfig "github.com/mzner/ocis-cli/internal/config"
	"github.com/mzner/ocis-cli/internal/credentials"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

func TestAuthSetupRegistersClientAndStoresSecret(t *testing.T) {
	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch request.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(writer).Encode(map[string]string{
				"issuer": provider.URL, "authorization_endpoint": provider.URL + "/authorize",
				"token_endpoint":        provider.URL + "/token",
				"userinfo_endpoint":     provider.URL + "/userinfo",
				"registration_endpoint": provider.URL + "/register",
			})
		case "/register":
			writer.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(writer).Encode(map[string]string{
				"client_id": "generated-client", "client_secret": "generated-secret",
			})
		default:
			t.Fatalf("unexpected request: %s", request.URL.Path)
		}
	}))
	defer provider.Close()

	configRepository := &memoryConfig{store: &appconfig.Store{
		Version: appconfig.CurrentVersion, Current: "work",
		Profiles: map[string]appconfig.Profile{"work": {
			Server: provider.URL, Insecure: true, ClientID: "existing-client",
			Username: "alice", Subject: "old-subject", AuthType: "oidc",
			DefaultSpace: "old-space", DefaultSpaceOwner: "old-owner",
		}},
	}}
	credentialRepository := &memoryCredentials{
		secrets: map[string]credentials.Secret{"work": {
			AccessToken: "old-access", RefreshToken: "old-refresh",
		}},
	}
	var output bytes.Buffer
	err := RunAuthWithOptions(context.Background(), AuthRequest{
		Operation: AuthSetup, Profile: "work",
	}, "", RunOptions{
		OutputMode: appoutput.JSON, Out: &output,
		Dependencies: Dependencies{
			Config: configRepository, Credentials: credentialRepository,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	selected := configRepository.store.Profiles["work"]
	if selected.ClientID != "generated-client" || selected.Username != "" ||
		selected.Subject != "" || selected.DefaultSpace != "" {
		t.Fatalf("profile: %#v", selected)
	}
	secret := credentialRepository.secrets["work"]
	if secret.ClientSecret != "generated-secret" ||
		secret.AccessToken != "" || secret.RefreshToken != "" {
		t.Fatalf("stored credential fields: %#v", secret)
	}
	if strings.Contains(output.String(), "generated-secret") {
		t.Fatalf("client secret leaked to output: %s", output.String())
	}
	selected.ClientSecret = ""
	configRepository.store.Profiles["work"] = selected
	reloaded, err := loadStore(Dependencies{
		Config: configRepository, Credentials: credentialRepository,
	})
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Profiles["work"].ClientSecret != "generated-secret" {
		t.Fatal("registered client secret did not survive repository reload")
	}
}

func TestAuthSetupPrintsAndPersistsStaticRegistration(t *testing.T) {
	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.URL.Path != "/.well-known/openid-configuration" {
			t.Fatalf("unexpected request: %s", request.URL.Path)
		}
		_ = json.NewEncoder(writer).Encode(map[string]string{
			"issuer": provider.URL, "authorization_endpoint": provider.URL + "/authorize",
			"token_endpoint":    provider.URL + "/token",
			"userinfo_endpoint": provider.URL + "/userinfo",
		})
	}))
	defer provider.Close()
	configRepository := &memoryConfig{store: &appconfig.Store{
		Version: appconfig.CurrentVersion, Current: "work",
		Profiles: map[string]appconfig.Profile{"work": {
			Server: provider.URL, Insecure: true, ClientID: "ocis-cli",
		}},
	}}
	credentialRepository := &memoryCredentials{
		secrets: map[string]credentials.Secret{"work": {ClientSecret: "old-secret"}},
	}
	var output bytes.Buffer
	err := RunAuthWithOptions(context.Background(), AuthRequest{
		Operation: AuthSetup, Profile: "work",
	}, "", RunOptions{
		Out: &output,
		Dependencies: Dependencies{
			Config: configRepository, Credentials: credentialRepository,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), staticClientRegistration) ||
		!strings.Contains(output.String(), "does not advertise dynamic") {
		t.Fatalf("output: %s", output.String())
	}
	if selected := configRepository.store.Profiles["work"]; selected.ClientID != defaultClientID {
		t.Fatalf("client ID: %q", selected.ClientID)
	}
	if secret := credentialRepository.secrets["work"]; !secret.Empty() {
		t.Fatalf("old secret remains: %#v", secret)
	}
}

func TestAuthSetupPropagatesCredentialStoreFailure(t *testing.T) {
	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch request.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(writer).Encode(map[string]string{
				"issuer": provider.URL, "authorization_endpoint": provider.URL + "/authorize",
				"token_endpoint":        provider.URL + "/token",
				"userinfo_endpoint":     provider.URL + "/userinfo",
				"registration_endpoint": provider.URL + "/register",
			})
		case "/register":
			_ = json.NewEncoder(writer).Encode(map[string]string{
				"client_id": "generated-client", "client_secret": "secret-never-output",
			})
		default:
			t.Fatalf("unexpected request: %s", request.URL.Path)
		}
	}))
	defer provider.Close()
	var output bytes.Buffer
	err := RunAuthWithOptions(context.Background(), AuthRequest{
		Operation: AuthSetup, Profile: "work",
	}, "", RunOptions{
		Out: &output,
		Dependencies: Dependencies{
			Config: &memoryConfig{store: &appconfig.Store{
				Current: "work", Profiles: map[string]appconfig.Profile{
					"work": {Server: provider.URL, Insecure: true},
				},
			}},
			Credentials: failingCredentialRepository{err: errors.New("keyring unavailable")},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "keyring unavailable") {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(output.String(), "secret-never-output") {
		t.Fatalf("secret leaked to output: %s", output.String())
	}
}

func TestExplainOIDCLoginError(t *testing.T) {
	cause := &auth.OAuthError{
		Code: "access_denied", Description: "invalid client_secret",
	}
	err := explainOIDCLoginError("work", defaultClientID, cause)
	if !errors.Is(err, cause) ||
		!apperror.IsKind(err, apperror.KindAuthentication) ||
		!strings.Contains(err.Error(), "ocis auth setup work") {
		t.Fatalf("unexpected actionable error: %v", err)
	}
}

type failingCredentialRepository struct {
	err error
}

func (repository failingCredentialRepository) Get(string) (credentials.Secret, error) {
	return credentials.Secret{}, credentials.ErrNotFound
}

func (repository failingCredentialRepository) Set(string, credentials.Secret) error {
	return repository.err
}

func (repository failingCredentialRepository) Delete(string) error {
	return repository.err
}
