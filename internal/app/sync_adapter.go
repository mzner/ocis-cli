package app

import (
	"context"

	syncapp "github.com/mzner/ocis-cli/internal/app/sync"
	"github.com/mzner/ocis-cli/internal/webdav"
)

type syncClientAdapter struct{ client *client }

func (adapter syncClientAdapter) ProfileName() string { return adapter.client.name }
func (adapter syncClientAdapter) AccountID() string   { return profileIdentity(adapter.client.profile) }
func (adapter syncClientAdapter) SelectSpace(value string) error {
	return adapter.client.selectSpace(value)
}
func (adapter syncClientAdapter) SelectedSpaceID() string  { return adapter.client.selectedSpaceID() }
func (adapter syncClientAdapter) Context() context.Context { return adapter.client.context() }
func (adapter syncClientAdapter) List(remote string) ([]webdav.Item, error) {
	return adapter.client.list(remote)
}
func (adapter syncClientAdapter) Stat(remote string) (webdav.Item, error) {
	return adapter.client.stat(remote)
}
func (adapter syncClientAdapter) EnsureCollection(remote string) error {
	return adapter.client.ensureCollection(remote)
}
func (adapter syncClientAdapter) DiscoverUploadCapabilities(ctx context.Context) webdav.TUSCapabilities {
	return discoverUploadCapabilities(ctx, adapter.client)
}
func (adapter syncClientAdapter) DAV() syncapp.DAVClient { return adapter.client.davClient() }

func syncOptions(options RunOptions) syncapp.Options {
	return syncapp.Options{
		OutputMode: options.OutputMode,
		Out:        options.Out,
		Space:      options.Space,
		Logger:     options.Logger,
		NewClient: func(ctx context.Context, selectedProfile string) (syncapp.Client, error) {
			selected, err := newClientWithOptions(ctx, selectedProfile, options)
			if err != nil {
				return nil, err
			}
			return syncClientAdapter{client: selected}, nil
		},
		SyncStates:     options.Dependencies.SyncStates,
		SyncJobs:       options.Dependencies.SyncJobs,
		SyncRecoveries: options.Dependencies.SyncRecoveries,
	}
}
