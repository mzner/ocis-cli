// Package syncrecovery persists non-secret reports for interrupted
// bidirectional synchronization runs.
package syncrecovery

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	syncmodel "github.com/mzner/ocis-cli/internal/sync"
	"github.com/mzner/ocis-cli/internal/syncstate"
)

const (
	// CurrentVersion is the recovery-journal schema version.
	CurrentVersion  = 1
	pathEnvironment = "OCIS_SYNC_RECOVERY_DIR"
)

// Status describes why a journal remains on disk.
type Status string

const (
	Running  Status = "running"
	Failed   Status = "failed"
	Canceled Status = "canceled"
	Conflict Status = "conflict"
)

// Journal records enough non-secret information to re-scan and re-plan a
// failed run. It never authorizes replay of its stored actions.
type Journal struct {
	Version    int                `json:"version"`
	ID         string             `json:"id"`
	Binding    syncmodel.Binding  `json:"binding"`
	MaxEntries int                `json:"maxEntries"`
	StartedAt  time.Time          `json:"startedAt"`
	UpdatedAt  time.Time          `json:"updatedAt"`
	Status     Status             `json:"status"`
	Plan       syncmodel.Plan     `json:"plan"`
	Completed  []syncmodel.Action `json:"completed,omitempty"`
	Current    *syncmodel.Action  `json:"current,omitempty"`
	Failure    string             `json:"failure,omitempty"`
}

// New creates a running journal for an already validated plan.
func New(
	binding syncmodel.Binding,
	maxEntries int,
	plan syncmodel.Plan,
	now time.Time,
) Journal {
	return Journal{
		Version: CurrentVersion, ID: binding.Key(), Binding: binding,
		MaxEntries: maxEntries, StartedAt: now.UTC(), UpdatedAt: now.UTC(),
		Status: Running, Plan: plan,
	}
}

// Keys returns sorted journal IDs.
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

// Load reads one recovery journal.
func Load(key string) (Journal, bool, error) {
	name, err := journalPath(key)
	if err != nil {
		return Journal{}, false, err
	}
	data, err := os.ReadFile(name) //nolint:gosec // validated key is resolved below the recovery directory
	if errors.Is(err, os.ErrNotExist) {
		return Journal{}, false, nil
	}
	if err != nil {
		return Journal{}, false, err
	}
	var journal Journal
	if err := json.Unmarshal(data, &journal); err != nil {
		return Journal{}, false, fmt.Errorf("decode sync recovery journal: %w", err)
	}
	if err := Validate(journal); err != nil {
		return Journal{}, false, err
	}
	return journal, true, nil
}

// Save atomically writes one recovery journal with owner-only permissions.
func Save(journal Journal) error {
	journal.UpdatedAt = journal.UpdatedAt.UTC()
	if err := Validate(journal); err != nil {
		return err
	}
	name, err := journalPath(journal.ID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(name), ".sync-recovery-*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()
	if err := temporary.Chmod(0600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, name)
}

// Delete removes one journal.
func Delete(key string) (bool, error) {
	name, err := journalPath(key)
	if err != nil {
		return false, err
	}
	if err := os.Remove(name); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

// Directory returns the active recovery-journal directory.
func Directory() (string, error) {
	if configured := os.Getenv(pathEnvironment); configured != "" {
		return configured, nil
	}
	directory, err := syncstate.Directory()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(directory), "sync-recovery"), nil
}

// Validate checks the portable journal schema.
func Validate(journal Journal) error {
	switch {
	case journal.Version != CurrentVersion:
		return fmt.Errorf(
			"sync recovery version %d is unsupported; expected %d",
			journal.Version, CurrentVersion,
		)
	case validateKey(journal.ID) != nil:
		return errors.New("invalid sync recovery ID")
	case journal.ID != journal.Binding.Key():
		return errors.New("sync recovery ID does not match its binding")
	case journal.Binding.Direction != syncmodel.Bidirectional:
		return errors.New("sync recovery journal is not bidirectional")
	case journal.MaxEntries < 1:
		return errors.New("sync recovery maximum entries must be at least 1")
	case journal.Status != Running && journal.Status != Failed &&
		journal.Status != Canceled && journal.Status != Conflict:
		return fmt.Errorf("invalid sync recovery status %q", journal.Status)
	}
	return nil
}

func journalPath(key string) (string, error) {
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
	if len(key) != 64 || strings.Trim(key, "0123456789abcdef") != "" {
		return errors.New("invalid sync recovery ID")
	}
	return nil
}
