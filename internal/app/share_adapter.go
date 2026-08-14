package app

import (
	"context"

	shareapp "github.com/mzner/ocis-cli/internal/app/share"
	"github.com/mzner/ocis-cli/internal/webdav"
)

type shareClientAdapter struct{ client *client }

func (adapter shareClientAdapter) SelectSpace(identifier string) error {
	return adapter.client.selectSpace(identifier)
}

func (adapter shareClientAdapter) SelectedSpaceID() string {
	return adapter.client.selectedSpaceID()
}

func (adapter shareClientAdapter) Stat(path string) (webdav.Item, error) {
	return adapter.client.stat(path)
}

func (adapter shareClientAdapter) Graph() shareapp.GraphClient {
	return adapter.client.graphClient()
}

func (adapter shareClientAdapter) Sharing() shareapp.SharingClient {
	return adapter.client.sharingClient()
}

func shareOptions(options RunOptions) shareapp.Options {
	return shareapp.Options{
		OutputMode: options.OutputMode, Out: options.Out,
		Space: options.Space, Logger: options.Logger,
		NewClient: func(
			ctx context.Context, selectedProfile string,
		) (shareapp.Client, error) {
			selected, err := newClientWithOptions(ctx, selectedProfile, options)
			if err != nil {
				return nil, err
			}
			return shareClientAdapter{client: selected}, nil
		},
	}
}

func permissionName(permissions int) string {
	return shareapp.PermissionName(permissions)
}
