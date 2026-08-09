package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoadMissingReturnsInitializedStore(t *testing.T) {
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "missing", "config.json"))
	store, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if store.Profiles == nil {
		t.Fatal("profiles map is nil")
	}
}

func TestSaveRoundTripAndPermissions(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "nested", "config.json")
	t.Setenv("OCIS_CONFIG", configPath)
	want := &Store{
		Current: "work",
		Profiles: map[string]Profile{
			"work": {
				Server:            "https://cloud.example",
				Username:          "alice",
				Subject:           "stable-subject",
				AuthType:          "basic",
				Password:          "secret",
				DefaultSpace:      "space-id",
				DefaultSpaceOwner: "account-key",
			},
		},
	}
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); runtime.GOOS != "windows" && got != 0600 {
		t.Fatalf("permissions: got %o, want 600", got)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Current != want.Current || got.Profiles["work"].Password != "" ||
		got.Profiles["work"].DefaultSpace != "space-id" ||
		got.Profiles["work"].DefaultSpaceOwner != "account-key" ||
		got.Profiles["work"].Subject != "stable-subject" {
		t.Fatalf("round trip mismatch: %#v", got)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" || containsSecret(string(data), "secret") {
		t.Fatalf("secret was written to config: %s", data)
	}
}

func containsSecret(value, secret string) bool {
	return strings.Contains(value, secret)
}

func TestValidateServerURL(t *testing.T) {
	for _, valid := range []string{"https://cloud.example", "https://localhost:9200"} {
		if err := ValidateServerURL(valid); err != nil {
			t.Errorf("expected %q to be valid: %v", valid, err)
		}
	}
	for _, invalid := range []string{"cloud.example", "ftp://cloud.example", "://bad"} {
		if err := ValidateServerURL(invalid); err == nil {
			t.Errorf("expected %q to be invalid", invalid)
		}
	}
}

// TestValidateServerURLRejectsCleartextWithoutOptIn covers the credential
// exposure: every authenticated request carries a Basic password or a bearer
// token, so a cleartext server URL leaks it on the wire. The scheme is accepted
// only when the caller opted into an insecure connection.
func TestValidateServerURLRejectsCleartextWithoutOptIn(t *testing.T) {
	cleartext := []string{"http://cloud.example", "http://localhost:9200"}
	for _, server := range cleartext {
		err := ValidateServerURL(server)
		if err == nil {
			t.Errorf("expected %q to require an explicit insecure opt-in", server)
			continue
		}
		if !strings.Contains(err.Error(), "--insecure") {
			t.Errorf("error for %q does not name the opt-in: %v", server, err)
		}
	}
	for _, server := range cleartext {
		if err := ValidateInsecureServerURL(server); err != nil {
			t.Errorf("expected %q to be allowed with the opt-in: %v", server, err)
		}
	}
}

func TestValidateInsecureServerURLStillRejectsUnusableURLs(t *testing.T) {
	for _, invalid := range []string{"cloud.example", "ftp://cloud.example", "://bad", ""} {
		if err := ValidateInsecureServerURL(invalid); err == nil {
			t.Errorf("expected %q to be invalid even with the opt-in", invalid)
		}
	}
}

func TestLoadRejectsUnsupportedSchema(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("OCIS_CONFIG", configPath)
	if err := os.WriteFile(configPath, []byte(`{"version":999,"profiles":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadRejectsUnversionedSchema(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("OCIS_CONFIG", configPath)
	if err := os.WriteFile(configPath, []byte(`{"current":"","profiles":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil || !strings.Contains(
		err.Error(), "unsupported",
	) {
		t.Fatalf("unexpected error: %v", err)
	}
}
