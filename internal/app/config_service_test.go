package app

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mzner/ocis-cli/internal/apperror"
	appconfig "github.com/mzner/ocis-cli/internal/config"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

func TestConfigPathUsesExplicitOverrideWithoutLoadingConfiguration(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "custom.json")
	t.Setenv("OCIS_CONFIG", configPath)
	var output bytes.Buffer
	if err := RunConfigWithOptions(
		context.Background(),
		ConfigRequest{Operation: ConfigPath},
		RunOptions{Out: &output},
	); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != configPath+"\n" {
		t.Fatalf("output=%q want=%q", got, configPath+"\n")
	}
}

func TestConfigPathsReportsEffectiveSources(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "profiles", "config.json")
	statePath := filepath.Join(directory, "state")
	t.Setenv("OCIS_CONFIG", configPath)
	t.Setenv("OCIS_SYNC_JOBS", "")
	t.Setenv("OCIS_STATE_DIR", statePath)
	var output bytes.Buffer
	if err := RunConfigWithOptions(
		context.Background(),
		ConfigRequest{Operation: ConfigPaths},
		RunOptions{Out: &output, OutputMode: appoutput.JSON},
	); err != nil {
		t.Fatal(err)
	}
	rendered := output.String()
	for _, expected := range []string{
		`"type": "config-paths"`,
		configPath,
		filepath.Join(filepath.Dir(configPath), "sync-jobs.json"),
		statePath,
		`"source": "OCIS_CONFIG"`,
		`"source": "OCIS_STATE_DIR"`,
		`"credentialBackend"`,
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("output does not contain %q:\n%s", expected, rendered)
		}
	}
}

func TestConfigPathsPrefersExplicitJobOverride(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("OCIS_CONFIG", filepath.Join(directory, "config.json"))
	jobsPath := filepath.Join(directory, "elsewhere", "jobs.json")
	t.Setenv("OCIS_SYNC_JOBS", jobsPath)
	var output bytes.Buffer
	if err := RunConfigWithOptions(
		context.Background(),
		ConfigRequest{Operation: ConfigPaths},
		RunOptions{Out: &output},
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), jobsPath) ||
		!strings.Contains(output.String(), "(from OCIS_SYNC_JOBS)") {
		t.Fatalf("unexpected output:\n%s", output.String())
	}
}

func TestConfigShowAllowlistExcludesEverySecret(t *testing.T) {
	configRepository := &memoryConfig{store: &appconfig.Store{
		Version: appconfig.CurrentVersion,
		Current: "work",
		Profiles: map[string]appconfig.Profile{
			"work": {
				Server: "https://cloud.example", Username: "alice",
				Subject: "stable-subject", AuthType: "oidc",
				ClientID: "public-client", Issuer: "https://idp.example",
				TokenURL:     "https://idp.example/token",
				UserInfoURL:  "https://idp.example/userinfo",
				ExpiresAt:    1_800_000_000,
				DefaultSpace: "space-id", DefaultSpaceOwner: "v1:owner",
				Password: "password-secret", ClientSecret: "client-secret",
				AccessToken: "access-secret", RefreshToken: "refresh-secret",
			},
			"other": {Server: "https://other.example"},
		},
	}}
	var output bytes.Buffer
	if err := RunConfigWithOptions(
		context.Background(),
		ConfigRequest{Operation: ConfigShow, Profile: "work"},
		RunOptions{
			Out: &output, OutputMode: appoutput.JSON,
			Dependencies: Dependencies{Config: configRepository},
		},
	); err != nil {
		t.Fatal(err)
	}
	rendered := output.String()
	for _, secret := range []string{
		"password-secret", "client-secret", "access-secret", "refresh-secret",
	} {
		if strings.Contains(rendered, secret) {
			t.Fatalf("secret %q leaked:\n%s", secret, rendered)
		}
	}
	for _, expected := range []string{
		`"type": "config"`, `"name": "work"`, `"current": true`,
		`"subject": "stable-subject"`, `"defaultSpace": "space-id"`,
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("output does not contain %q:\n%s", expected, rendered)
		}
	}
	if strings.Contains(rendered, "other.example") {
		t.Fatalf("profile filter was ignored:\n%s", rendered)
	}
}

func TestConfigShowRejectsUnknownProfile(t *testing.T) {
	err := RunConfigWithOptions(
		context.Background(),
		ConfigRequest{Operation: ConfigShow, Profile: "missing"},
		RunOptions{Dependencies: Dependencies{
			Config: &memoryConfig{store: &appconfig.Store{
				Version:  appconfig.CurrentVersion,
				Profiles: map[string]appconfig.Profile{},
			}},
		}},
	)
	if apperror.ExitCode(err) != 2 ||
		!strings.Contains(err.Error(), `unknown server profile "missing"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConfigShowHumanOutputHandlesEmptyStore(t *testing.T) {
	var output bytes.Buffer
	if err := RunConfigWithOptions(
		context.Background(),
		ConfigRequest{Operation: ConfigShow},
		RunOptions{
			Out: &output,
			Dependencies: Dependencies{
				Config: &memoryConfig{store: &appconfig.Store{
					Version:  appconfig.CurrentVersion,
					Profiles: map[string]appconfig.Profile{},
				}},
			},
		},
	); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"Schema version: 3", "Current profile: (none)", "Profiles: (none)",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("output does not contain %q:\n%s", expected, output.String())
		}
	}
}

func TestConfigShowHumanOutputRendersPersistedMetadata(t *testing.T) {
	var output bytes.Buffer
	if err := RunConfigWithOptions(
		context.Background(),
		ConfigRequest{Operation: ConfigShow},
		RunOptions{
			Out: &output,
			Dependencies: Dependencies{
				Config: &memoryConfig{store: &appconfig.Store{
					Version: appconfig.CurrentVersion, Current: "work",
					Profiles: map[string]appconfig.Profile{"work": {
						Server: "https://cloud.example", Username: "alice",
						AuthType: "oidc", Insecure: true,
						ExpiresAt: 1_800_000_000,
					}},
				}},
			},
		},
	); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"* work", "Server:", "https://cloud.example", "Username:",
		"alice", "Insecure TLS:", "true", "Token expiry:", "2027-01-15",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("output does not contain %q:\n%s", expected, output.String())
		}
	}
}

func TestConfigRejectsUnknownOperation(t *testing.T) {
	err := RunConfigWithOptions(
		context.Background(),
		ConfigRequest{Operation: "invalid"},
		RunOptions{},
	)
	if apperror.ExitCode(err) != 2 ||
		!strings.Contains(err.Error(), `unknown config command "invalid"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}
