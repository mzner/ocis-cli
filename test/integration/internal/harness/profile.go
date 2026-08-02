package harness

import (
	"encoding/json"
	"fmt"
	"os"
)

type profileDocument struct {
	Version  int                       `json:"version"`
	Current  string                    `json:"current"`
	Profiles map[string]map[string]any `json:"profiles"`
}

// ExpireProfile forces the next command to exercise OIDC token refresh.
func ExpireProfile(configPath, profileName string) error {
	data, err := os.ReadFile(configPath) //nolint:gosec // integration-owned temporary config path
	if err != nil {
		return err
	}
	var document profileDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("decode integration config: %w", err)
	}
	profile, ok := document.Profiles[profileName]
	if !ok {
		return fmt.Errorf("profile %q is absent from integration config", profileName)
	}
	profile["expiresAt"] = int64(1)
	updated, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	updated = append(updated, '\n')
	if err := os.WriteFile(configPath, updated, 0600); err != nil {
		return fmt.Errorf("write integration config: %w", err)
	}
	return nil
}

// ProfileExpiry reads a profile's non-secret token expiry timestamp.
func ProfileExpiry(configPath, profileName string) (int64, error) {
	data, err := os.ReadFile(configPath) //nolint:gosec // integration-owned temporary config path
	if err != nil {
		return 0, err
	}
	var document profileDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return 0, fmt.Errorf("decode integration config: %w", err)
	}
	profile, ok := document.Profiles[profileName]
	if !ok {
		return 0, fmt.Errorf("profile %q is absent from integration config", profileName)
	}
	value, ok := profile["expiresAt"].(float64)
	if !ok {
		return 0, fmt.Errorf("profile %q has no numeric expiresAt", profileName)
	}
	return int64(value), nil
}
