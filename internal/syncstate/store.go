// Package syncstate persists non-secret synchronization baselines outside the
// normal profile configuration.
package syncstate

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	syncmodel "github.com/mzner/ocis-cli/internal/sync"
)

const stateDirectoryEnvironment = "OCIS_STATE_DIR"

// Load reads one saved synchronization baseline.
func Load(key string) (syncmodel.State, bool, error) {
	statePath, err := statePath(key)
	if err != nil {
		return syncmodel.State{}, false, err
	}
	data, err := os.ReadFile(statePath) //nolint:gosec // key is validated and resolved under the state directory
	if errors.Is(err, os.ErrNotExist) {
		return syncmodel.State{}, false, nil
	}
	if err != nil {
		return syncmodel.State{}, false, err
	}
	var state syncmodel.State
	if err := json.Unmarshal(data, &state); err != nil {
		return syncmodel.State{}, false, fmt.Errorf("decode sync state: %w", err)
	}
	if state.Version != syncmodel.StateVersion {
		return syncmodel.State{}, false, fmt.Errorf(
			"sync state version %d is unsupported; expected %d",
			state.Version, syncmodel.StateVersion,
		)
	}
	return state, true, nil
}

// Save atomically writes one synchronization baseline.
func Save(key string, state syncmodel.State) error {
	if state.Version != syncmodel.StateVersion {
		return fmt.Errorf(
			"cannot save sync state version %d; expected %d",
			state.Version, syncmodel.StateVersion,
		)
	}
	statePath, err := statePath(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(statePath), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temp, err := os.CreateTemp(filepath.Dir(statePath), ".sync-*.tmp")
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
	return os.Rename(tempPath, statePath)
}

// Keys returns sorted IDs for every recognizable synchronization-state file.
// State contents are deliberately not decoded so corrupt entries remain
// discoverable and removable.
func Keys() ([]string, error) {
	directory, err := Directory()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		key := strings.TrimSuffix(entry.Name(), ".json")
		if validateKey(key) == nil {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

// Delete removes one synchronization-state file. Missing state is reported
// without an error so callers can map it to their public not-found contract.
func Delete(key string) (bool, error) {
	statePath, err := statePath(key)
	if err != nil {
		return false, err
	}
	if err := os.Remove(statePath); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

// Directory returns the directory containing synchronization state.
func Directory() (string, error) {
	if configured := os.Getenv(stateDirectoryEnvironment); configured != "" {
		return configured, nil
	}
	switch runtime.GOOS {
	case "linux":
		if stateHome := os.Getenv("XDG_STATE_HOME"); stateHome != "" {
			return filepath.Join(stateHome, "ocis-cli", "sync"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "state", "ocis-cli", "sync"), nil
	case "windows":
		local, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(local, "ocis-cli", "sync"), nil
	default:
		config, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(config, "ocis-cli", "sync"), nil
	}
}

func statePath(key string) (string, error) {
	if err := validateKey(key); err != nil {
		return "", err
	}
	directory, err := Directory()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, key+".json"), nil
}

func validateKey(key string) error {
	if len(key) != sha256.Size*2 ||
		strings.Trim(key, "0123456789abcdef") != "" {
		return errors.New("invalid synchronization state key")
	}
	return nil
}
