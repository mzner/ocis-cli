// Package syncjob persists non-secret named synchronization configurations.
package syncjob

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	appconfig "github.com/mzner/ocis-cli/internal/config"
	syncmodel "github.com/mzner/ocis-cli/internal/sync"
)

const (
	// CurrentVersion is the current named-job file schema.
	CurrentVersion  = 1
	pathEnvironment = "OCIS_SYNC_JOBS"
)

var validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`) //nolint:gochecknoglobals // immutable validation expression

// Job is one reusable, account- and Space-bound sync configuration.
type Job struct {
	Name       string              `json:"name"`
	Profile    string              `json:"profile"`
	AccountID  string              `json:"accountId"`
	SpaceID    string              `json:"spaceId,omitempty"`
	Direction  syncmodel.Direction `json:"direction"`
	LocalRoot  string              `json:"localRoot"`
	RemoteRoot string              `json:"remoteRoot"`
	Includes   []string            `json:"includes,omitempty"`
	Excludes   []string            `json:"excludes,omitempty"`
	Delete     bool                `json:"delete,omitempty"`
	Overwrite  bool                `json:"overwrite,omitempty"`
	MaxEntries int                 `json:"maxEntries"`
}

// Store is the versioned named-job document.
type Store struct {
	Version int            `json:"version"`
	Jobs    map[string]Job `json:"jobs"`
}

// Empty returns an initialized current-version store.
func Empty() Store {
	return Store{Version: CurrentVersion, Jobs: map[string]Job{}}
}

// Load reads named jobs, returning an initialized store when none exists.
func Load() (Store, error) {
	storePath, err := Path()
	if err != nil {
		return Store{}, err
	}
	data, err := os.ReadFile(storePath) //nolint:gosec // path is the resolved user configuration path
	if errors.Is(err, os.ErrNotExist) {
		return Empty(), nil
	}
	if err != nil {
		return Store{}, err
	}
	var store Store
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&store); err != nil {
		return Store{}, fmt.Errorf("read sync jobs: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON documents")
		}
		return Store{}, fmt.Errorf("read sync jobs: %w", err)
	}
	if store.Version != CurrentVersion {
		return Store{}, fmt.Errorf(
			"sync-job schema version %d is unsupported; expected %d",
			store.Version, CurrentVersion,
		)
	}
	if store.Jobs == nil {
		store.Jobs = map[string]Job{}
	}
	for name, job := range store.Jobs {
		if err := Validate(job); err != nil {
			return Store{}, fmt.Errorf("invalid sync job %q: %w", name, err)
		}
		if name != job.Name {
			return Store{}, fmt.Errorf(
				"invalid sync job %q: stored name is %q", name, job.Name,
			)
		}
	}
	return store, nil
}

// Save atomically writes named jobs with owner-only permissions.
func Save(store Store) error {
	if store.Version != CurrentVersion {
		return fmt.Errorf(
			"cannot save sync-job schema version %d; expected %d",
			store.Version, CurrentVersion,
		)
	}
	if store.Jobs == nil {
		store.Jobs = map[string]Job{}
	}
	for name, job := range store.Jobs {
		if err := Validate(job); err != nil {
			return fmt.Errorf("invalid sync job %q: %w", name, err)
		}
		if name != job.Name {
			return fmt.Errorf(
				"invalid sync job %q: stored name is %q", name, job.Name,
			)
		}
	}
	storePath, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(storePath), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temp, err := os.CreateTemp(filepath.Dir(storePath), ".sync-jobs-*.tmp")
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
	return os.Rename(tempPath, storePath)
}

// Path returns the active named-job configuration path.
func Path() (string, error) {
	if configured := os.Getenv(pathEnvironment); configured != "" {
		return configured, nil
	}
	configPath, err := appconfig.Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(configPath), "sync-jobs.json"), nil
}

// Validate checks the portable named-job schema.
func Validate(job Job) error {
	switch {
	case !validName.MatchString(job.Name):
		return errors.New(
			"name must be 1-64 characters using letters, numbers, '.', '_', or '-'",
		)
	case job.Profile == "":
		return errors.New("profile is required")
	case job.AccountID == "":
		return errors.New("account identity is required")
	case job.Direction != syncmodel.Push && job.Direction != syncmodel.Pull &&
		job.Direction != syncmodel.Bidirectional:
		return fmt.Errorf("invalid direction %q", job.Direction)
	case !filepath.IsAbs(job.LocalRoot):
		return errors.New("local root must be absolute")
	case job.RemoteRoot == "" || !strings.HasPrefix(job.RemoteRoot, "/") ||
		path.Clean(job.RemoteRoot) != job.RemoteRoot:
		return errors.New("remote root must be an absolute normalized path")
	case job.MaxEntries < 1:
		return errors.New("maximum entries must be at least 1")
	}
	if _, _, err := syncmodel.NormalizePatterns(
		job.Includes, job.Excludes,
	); err != nil {
		return err
	}
	return nil
}

// ValidateName checks a job selector before repository access.
func ValidateName(name string) error {
	if !validName.MatchString(name) {
		return errors.New(
			"name must be 1-64 characters using letters, numbers, '.', '_', or '-'",
		)
	}
	return nil
}
