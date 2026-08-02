package app

import (
	appconfig "github.com/mzner/ocis-cli/internal/config"
	"github.com/mzner/ocis-cli/internal/credentials"
	syncmodel "github.com/mzner/ocis-cli/internal/sync"
	"github.com/mzner/ocis-cli/internal/syncjob"
	"github.com/mzner/ocis-cli/internal/syncrecovery"
	"github.com/mzner/ocis-cli/internal/syncstate"
)

// ConfigRepository is the application port for non-secret profile settings.
type ConfigRepository interface {
	Load() (*appconfig.Store, error)
	Save(*appconfig.Store) error
}

// CredentialRepository is the application port for secret profile material.
type CredentialRepository interface {
	Get(string) (credentials.Secret, error)
	Set(string, credentials.Secret) error
	Delete(string) error
}

// UploadSessionRepository stores protected resumable-upload locations.
type UploadSessionRepository interface {
	Get(profileName, key string) (credentials.UploadSession, error)
	Set(profileName, key string, session credentials.UploadSession) error
	Delete(profileName, key string) error
}

// SyncStateRepository stores versioned, non-secret reconciliation baselines.
type SyncStateRepository interface {
	Keys() ([]string, error)
	Load(string) (syncmodel.State, bool, error)
	Save(string, syncmodel.State) error
	Delete(string) (bool, error)
}

// SyncJobRepository stores reusable, non-secret synchronization settings.
type SyncJobRepository interface {
	Load() (syncjob.Store, error)
	Save(syncjob.Store) error
}

// SyncRecoveryRepository stores non-secret interrupted-run journals.
type SyncRecoveryRepository interface {
	Keys() ([]string, error)
	Load(string) (syncrecovery.Journal, bool, error)
	Save(syncrecovery.Journal) error
	Delete(string) (bool, error)
}

// Dependencies contains application persistence ports.
type Dependencies struct {
	Config         ConfigRepository
	Credentials    CredentialRepository
	UploadSessions UploadSessionRepository
	SyncStates     SyncStateRepository
	SyncJobs       SyncJobRepository
	SyncRecoveries SyncRecoveryRepository
}

func (dependencies Dependencies) normalized() Dependencies {
	if dependencies.Config == nil {
		dependencies.Config = configAdapter{}
	}
	if dependencies.Credentials == nil {
		dependencies.Credentials = credentialAdapter{}
	}
	if dependencies.UploadSessions == nil {
		dependencies.UploadSessions = uploadSessionAdapter{}
	}
	if dependencies.SyncStates == nil {
		dependencies.SyncStates = syncStateAdapter{}
	}
	if dependencies.SyncJobs == nil {
		dependencies.SyncJobs = syncJobAdapter{}
	}
	if dependencies.SyncRecoveries == nil {
		dependencies.SyncRecoveries = syncRecoveryAdapter{}
	}
	return dependencies
}

func defaultDependencies() Dependencies {
	return (Dependencies{}).normalized()
}

type configAdapter struct{}

func (configAdapter) Load() (*appconfig.Store, error) {
	return appconfig.Load()
}

func (configAdapter) Save(store *appconfig.Store) error {
	return appconfig.Save(store)
}

type credentialAdapter struct{}

func (credentialAdapter) Get(profileName string) (credentials.Secret, error) {
	return credentials.Get(profileName)
}

func (credentialAdapter) Set(profileName string, secret credentials.Secret) error {
	return credentials.Set(profileName, secret)
}

func (credentialAdapter) Delete(profileName string) error {
	return credentials.Delete(profileName)
}

type uploadSessionAdapter struct{}

func (uploadSessionAdapter) Get(
	profileName, key string,
) (credentials.UploadSession, error) {
	return credentials.GetUploadSession(profileName, key)
}

func (uploadSessionAdapter) Set(
	profileName, key string, session credentials.UploadSession,
) error {
	return credentials.SetUploadSession(profileName, key, session)
}

func (uploadSessionAdapter) Delete(profileName, key string) error {
	return credentials.DeleteUploadSession(profileName, key)
}

type syncStateAdapter struct{}

func (syncStateAdapter) Keys() ([]string, error) {
	return syncstate.Keys()
}

func (syncStateAdapter) Load(
	key string,
) (syncmodel.State, bool, error) {
	return syncstate.Load(key)
}

func (syncStateAdapter) Save(
	key string, state syncmodel.State,
) error {
	return syncstate.Save(key, state)
}

func (syncStateAdapter) Delete(key string) (bool, error) {
	return syncstate.Delete(key)
}

type syncJobAdapter struct{}

func (syncJobAdapter) Load() (syncjob.Store, error) {
	return syncjob.Load()
}

func (syncJobAdapter) Save(store syncjob.Store) error {
	return syncjob.Save(store)
}

type syncRecoveryAdapter struct{}

func (syncRecoveryAdapter) Keys() ([]string, error) {
	return syncrecovery.Keys()
}

func (syncRecoveryAdapter) Load(
	key string,
) (syncrecovery.Journal, bool, error) {
	return syncrecovery.Load(key)
}

func (syncRecoveryAdapter) Save(journal syncrecovery.Journal) error {
	return syncrecovery.Save(journal)
}

func (syncRecoveryAdapter) Delete(key string) (bool, error) {
	return syncrecovery.Delete(key)
}
