// Package config owns persisted CLI configuration and server profiles.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
)

// CurrentVersion is the latest supported configuration schema version.
const CurrentVersion = 3

// Store is the version-independent persisted configuration model.
type Store struct {
	Version  int                `json:"version"`
	Current  string             `json:"current"`
	Profiles map[string]Profile `json:"profiles"`
}

// Profile contains connection and authentication state for one oCIS server.
//
// Authentication material is held at runtime but never serialized by Save.
type Profile struct {
	Server            string `json:"server"`
	Username          string `json:"username,omitempty"`
	Subject           string `json:"subject,omitempty"`
	AuthType          string `json:"authType,omitempty"`
	Password          string `json:"-"`
	Insecure          bool   `json:"insecure,omitempty"`
	ClientID          string `json:"clientId"`
	ClientSecret      string `json:"-"`
	Issuer            string `json:"issuer,omitempty"`
	TokenURL          string `json:"tokenUrl,omitempty"`
	UserInfoURL       string `json:"userInfoUrl,omitempty"`
	AccessToken       string `json:"-"`
	RefreshToken      string `json:"-"`
	ExpiresAt         int64  `json:"expiresAt,omitempty"`
	DefaultSpace      string `json:"defaultSpace,omitempty"`
	DefaultSpaceOwner string `json:"defaultSpaceOwner,omitempty"`
}

type diskStore struct {
	Version  int                    `json:"version"`
	Current  string                 `json:"current"`
	Profiles map[string]diskProfile `json:"profiles"`
}

type diskProfile struct {
	Server            string `json:"server"`
	Username          string `json:"username,omitempty"`
	Subject           string `json:"subject,omitempty"`
	AuthType          string `json:"authType,omitempty"`
	Insecure          bool   `json:"insecure,omitempty"`
	ClientID          string `json:"clientId"`
	Issuer            string `json:"issuer,omitempty"`
	TokenURL          string `json:"tokenUrl,omitempty"`
	UserInfoURL       string `json:"userInfoUrl,omitempty"`
	ExpiresAt         int64  `json:"expiresAt,omitempty"`
	DefaultSpace      string `json:"defaultSpace,omitempty"`
	DefaultSpaceOwner string `json:"defaultSpaceOwner,omitempty"`
}

// Path returns the active configuration path.
func Path() (string, error) {
	if configured := os.Getenv("OCIS_CONFIG"); configured != "" {
		return configured, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ocis-cli", "config.json"), nil
}

// Load reads the store, returning an empty initialized store when none exists.
func Load() (*Store, error) {
	configPath, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(configPath) //nolint:gosec // path is the resolved user configuration path
	if errors.Is(err, os.ErrNotExist) {
		return &Store{Version: CurrentVersion, Profiles: map[string]Profile{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var persisted diskStore
	if err := json.Unmarshal(data, &persisted); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	if persisted.Version != CurrentVersion {
		return nil, fmt.Errorf(
			"config schema version %d is unsupported; expected version %d",
			persisted.Version, CurrentVersion,
		)
	}
	store := &Store{
		Version: CurrentVersion, Current: persisted.Current,
		Profiles: make(map[string]Profile, len(persisted.Profiles)),
	}
	for name, profile := range persisted.Profiles {
		store.Profiles[name] = Profile{
			Server: profile.Server, Username: profile.Username, Subject: profile.Subject,
			AuthType: profile.AuthType,
			Insecure: profile.Insecure, ClientID: profile.ClientID,
			Issuer: profile.Issuer, TokenURL: profile.TokenURL,
			UserInfoURL: profile.UserInfoURL, ExpiresAt: profile.ExpiresAt,
			DefaultSpace: profile.DefaultSpace, DefaultSpaceOwner: profile.DefaultSpaceOwner,
		}
	}
	return store, nil
}

// Save writes the store atomically with owner-only permissions.
func Save(store *Store) error {
	configPath, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		return err
	}
	persisted := diskStore{
		Version: CurrentVersion, Current: store.Current,
		Profiles: make(map[string]diskProfile, len(store.Profiles)),
	}
	for name, profile := range store.Profiles {
		persisted.Profiles[name] = diskProfile{
			Server: profile.Server, Username: profile.Username, Subject: profile.Subject,
			AuthType: profile.AuthType,
			Insecure: profile.Insecure, ClientID: profile.ClientID, Issuer: profile.Issuer,
			TokenURL: profile.TokenURL, UserInfoURL: profile.UserInfoURL, ExpiresAt: profile.ExpiresAt,
			DefaultSpace: profile.DefaultSpace, DefaultSpaceOwner: profile.DefaultSpaceOwner,
		}
	}
	data, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	temp, err := os.CreateTemp(filepath.Dir(configPath), ".config-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if err := temp.Chmod(0600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, configPath)
}

// ValidateServerURL validates a user-provided oCIS base URL and requires TLS.
// Every authenticated request carries a Basic password or a bearer token, so a
// cleartext server URL would expose the credential to anyone on the path. Use
// ValidateInsecureServerURL when the caller explicitly accepted that risk.
func ValidateServerURL(server string) error {
	parsed, err := parseServerURL(server)
	if err != nil {
		return err
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf(
			"server URL %q must use https; "+
				"pass --insecure to allow a cleartext connection to an "+
				"explicitly trusted development server",
			server,
		)
	}
	return nil
}

// ValidateInsecureServerURL validates a user-provided oCIS base URL, permitting
// cleartext http. It is reached only after an explicit insecure opt-in; the URL
// must still be usable.
func ValidateInsecureServerURL(server string) error {
	_, err := parseServerURL(server)
	return err
}

func parseServerURL(server string) (*url.URL, error) {
	parsed, err := url.ParseRequestURI(server)
	if err != nil || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf(
			"invalid server URL %q; expected http:// or https:// URL", server,
		)
	}
	return parsed, nil
}
