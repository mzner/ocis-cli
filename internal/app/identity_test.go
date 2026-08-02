package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appconfig "github.com/mzner/ocis-cli/internal/config"
	"github.com/mzner/ocis-cli/internal/credentials"
)

func TestProfileIdentityUsesStableOIDCSubject(t *testing.T) {
	first := profile{
		AuthType: "oidc", Issuer: "https://idp.example",
		Subject: "subject-1", Username: "same-name",
	}
	second := first
	second.Subject = "subject-2"
	if profileIdentity(first) == profileIdentity(second) {
		t.Fatal("different OIDC subjects produced the same account identity")
	}
	second = first
	second.Username = "renamed-user"
	if profileIdentity(first) != profileIdentity(second) {
		t.Fatal("OIDC display-name change altered the stable account identity")
	}
	if got := profileIdentity(profile{
		AuthType: "oidc", Issuer: "https://idp.example",
		Username: "name-without-subject",
	}); got != "" {
		t.Fatalf("OIDC identity without subject=%q", got)
	}
}

func TestStaleDefaultSpaceIsClearedAndCommandFails(t *testing.T) {
	davCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.URL.Path == "/graph/v1.0/me/drives" {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"value":[]}`)
			return
		}
		davCalled = true
		t.Fatalf("request continued after stale Space resolution: %s", request.URL.Path)
	}))
	defer server.Close()
	selected := profile{
		Server: server.URL, AuthType: "basic", Username: "alice",
		DefaultSpace: "missing-space",
	}
	selected.DefaultSpaceOwner = profileIdentity(selected)
	configRepository := &memoryConfig{store: &appconfig.Store{
		Version: appconfig.CurrentVersion, Current: "work",
		Profiles: map[string]appconfig.Profile{"work": selected},
	}}
	err := RunFilesystemWithOptions(context.Background(), FilesystemRequest{
		Operation: FilesystemList, Source: "/",
	}, "work", RunOptions{
		Out: io.Discard,
		Dependencies: Dependencies{
			Config: configRepository,
			Credentials: &memoryCredentials{
				secrets: map[string]credentials.Secret{"work": {Password: "secret"}},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "no longer available and was cleared") {
		t.Fatalf("unexpected error: %v", err)
	}
	persisted := configRepository.store.Profiles["work"]
	if persisted.DefaultSpace != "" || persisted.DefaultSpaceOwner != "" {
		t.Fatalf("stale selection remains: %#v", persisted)
	}
	if davCalled {
		t.Fatal("file operation reached DAV after stale Space resolution")
	}
}

func TestDifferentOIDCSubjectClearsAccountState(t *testing.T) {
	selected := profile{
		AuthType: "oidc", Issuer: "https://idp.example",
		Subject: "old-subject", Username: "same-name", DefaultSpace: "space-id",
	}
	selected.DefaultSpaceOwner = profileIdentity(selected)
	selected.Subject = "new-subject"
	clearDefaultSpaceAfterIdentityChange(&selected)
	if selected.DefaultSpace != "" || selected.DefaultSpaceOwner != "" {
		t.Fatalf("different subject retained account state: %#v", selected)
	}
}

func TestUnownedDefaultSpaceIsCleared(t *testing.T) {
	selected := profile{
		Server: "https://cloud.example", AuthType: "basic",
		Username: "alice", DefaultSpace: "space-id",
	}
	clearDefaultSpaceAfterIdentityChange(&selected)
	if selected.DefaultSpace != "" || selected.DefaultSpaceOwner != "" {
		t.Fatalf("unowned account state remains: %#v", selected)
	}
}

func TestSameOIDCSubjectPreservesAccountState(t *testing.T) {
	selected := profile{
		AuthType: "oidc", Issuer: "https://idp.example",
		Subject: "stable-subject", Username: "old-name", DefaultSpace: "space-id",
	}
	selected.DefaultSpaceOwner = profileIdentity(selected)
	selected.Username = "new-display-name"
	clearDefaultSpaceAfterIdentityChange(&selected)
	if selected.DefaultSpace != "space-id" ||
		selected.DefaultSpaceOwner != profileIdentity(selected) {
		t.Fatalf("same subject lost account state: %#v", selected)
	}
}
