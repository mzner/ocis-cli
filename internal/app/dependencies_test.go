package app

import (
	"context"
	"errors"
	"io"
	"sort"
	"testing"

	appconfig "github.com/mzner/ocis-cli/internal/config"
	"github.com/mzner/ocis-cli/internal/credentials"
	syncmodel "github.com/mzner/ocis-cli/internal/sync"
	"github.com/mzner/ocis-cli/internal/syncjob"
	"github.com/mzner/ocis-cli/internal/syncrecovery"
)

type memoryConfig struct {
	store *appconfig.Store
	saves int
}

func (repository *memoryConfig) Load() (*appconfig.Store, error) {
	return repository.store, nil
}

func (repository *memoryConfig) Save(store *appconfig.Store) error {
	repository.store = store
	repository.saves++
	return nil
}

type memoryCredentials struct {
	secrets map[string]credentials.Secret
}

type memorySyncStates struct {
	states     map[string]syncmodel.State
	loadErrors map[string]error
	saves      int
	deletes    int
}

type memorySyncJobs struct {
	store syncjob.Store
	saves int
}

type memorySyncRecoveries struct {
	journals map[string]syncrecovery.Journal
	saves    int
	deletes  int
}

func (repository *memorySyncRecoveries) Keys() ([]string, error) {
	keys := make([]string, 0, len(repository.journals))
	for key := range repository.journals {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, nil
}

func (repository *memorySyncRecoveries) Load(
	key string,
) (syncrecovery.Journal, bool, error) {
	journal, found := repository.journals[key]
	return journal, found, nil
}

func (repository *memorySyncRecoveries) Save(
	journal syncrecovery.Journal,
) error {
	if repository.journals == nil {
		repository.journals = map[string]syncrecovery.Journal{}
	}
	repository.journals[journal.ID] = journal
	repository.saves++
	return nil
}

func (repository *memorySyncRecoveries) Delete(key string) (bool, error) {
	_, found := repository.journals[key]
	delete(repository.journals, key)
	if found {
		repository.deletes++
	}
	return found, nil
}

func (repository *memorySyncJobs) Load() (syncjob.Store, error) {
	return repository.store, nil
}

func (repository *memorySyncJobs) Save(store syncjob.Store) error {
	repository.store = store
	repository.saves++
	return nil
}

func (repository *memorySyncStates) Keys() ([]string, error) {
	keys := make([]string, 0, len(repository.states)+len(repository.loadErrors))
	seen := map[string]bool{}
	for key := range repository.states {
		keys = append(keys, key)
		seen[key] = true
	}
	for key := range repository.loadErrors {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func (repository *memorySyncStates) Load(
	key string,
) (syncmodel.State, bool, error) {
	if err := repository.loadErrors[key]; err != nil {
		return syncmodel.State{}, false, err
	}
	state, found := repository.states[key]
	return state, found, nil
}

func (repository *memorySyncStates) Save(
	key string, state syncmodel.State,
) error {
	repository.states[key] = state
	repository.saves++
	return nil
}

func (repository *memorySyncStates) Delete(key string) (bool, error) {
	_, found := repository.states[key]
	if !found {
		_, found = repository.loadErrors[key]
	}
	delete(repository.states, key)
	delete(repository.loadErrors, key)
	if found {
		repository.deletes++
	}
	return found, nil
}

func (repository *memoryCredentials) Get(name string) (credentials.Secret, error) {
	secret, ok := repository.secrets[name]
	if !ok {
		return credentials.Secret{}, credentials.ErrNotFound
	}
	return secret, nil
}

func (repository *memoryCredentials) Set(name string, secret credentials.Secret) error {
	repository.secrets[name] = secret
	return nil
}

func (repository *memoryCredentials) Delete(name string) error {
	delete(repository.secrets, name)
	return nil
}

func TestServerUseCaseUsesInjectedRepositories(t *testing.T) {
	t.Setenv("OCIS_CLIENT_SECRET", "client-secret")
	configRepository := &memoryConfig{
		store: &appconfig.Store{Version: 1, Profiles: map[string]appconfig.Profile{}},
	}
	credentialRepository := &memoryCredentials{
		secrets: map[string]credentials.Secret{},
	}
	options := RunOptions{
		Out: io.Discard,
		Dependencies: Dependencies{
			Config: configRepository, Credentials: credentialRepository,
		},
	}
	if err := RunServerWithOptions(context.Background(), ServerRequest{
		Operation: ServerAdd, Name: "work", Server: "https://cloud.example",
	}, options); err != nil {
		t.Fatal(err)
	}
	if configRepository.saves != 1 || configRepository.store.Current != "work" {
		t.Fatalf("config repository: %#v", configRepository)
	}
	if got := credentialRepository.secrets["work"].ClientSecret; got != "client-secret" {
		t.Fatalf("credential repository secret: %q", got)
	}
}

func TestInjectedCredentialRepositoryNotFound(t *testing.T) {
	dependencies := Dependencies{
		Config: &memoryConfig{store: &appconfig.Store{
			Version: 1, Current: "work",
			Profiles: map[string]appconfig.Profile{
				"work": {Server: "https://cloud.example", AuthType: "basic"},
			},
		}},
		Credentials: &memoryCredentials{secrets: map[string]credentials.Secret{}},
	}
	_, err := newClientWithOptions(context.Background(), "work", RunOptions{
		Dependencies: dependencies,
	}.normalized())
	if err == nil || errors.Is(err, credentials.ErrNotFound) {
		t.Fatalf("expected actionable authentication error, got %v", err)
	}
}
