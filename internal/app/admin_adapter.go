package app

import (
	"context"

	adminapp "github.com/mzner/ocis-cli/internal/app/admin"
	"github.com/mzner/ocis-cli/internal/graph"
)

type adminClientAdapter struct{ client *client }

func (adapter adminClientAdapter) Graph() adminapp.GraphClient { return adapter.client.graphClient() }
func (adapter adminClientAdapter) Sharing() adminapp.SharingClient {
	return adapter.client.sharingClient()
}
func (adapter adminClientAdapter) ProfileName() string { return adapter.client.name }

func adminOptions(options RunOptions) adminapp.Options {
	return adminapp.Options{
		OutputMode: options.OutputMode,
		Out:        options.Out,
		Space:      options.Space,
		Logger:     options.Logger,
		NewClient: func(ctx context.Context, selectedProfile string) (adminapp.Client, error) {
			selected, err := newClientWithOptions(ctx, selectedProfile, options)
			if err != nil {
				return nil, err
			}
			return adminClientAdapter{client: selected}, nil
		},
		RequireAccountAdmin: func(ctx context.Context, selected adminapp.Client) error {
			adapter, ok := selected.(adminClientAdapter)
			if !ok {
				return requireAccountAdminThroughPort(ctx, selected)
			}
			return requireAccountAdminMFA(ctx, adapter.client)
		},
		WriteSpaceDetails: func(ctx context.Context, selected adminapp.Client, drive graph.Drive, _ adminapp.Options) error {
			adapter, ok := selected.(adminClientAdapter)
			if !ok {
				return writeAdminSpaceDetailsThroughPort(ctx, selected, drive, options)
			}
			details, err := loadSpaceDetails(ctx, adapter.client, drive)
			if err != nil {
				return err
			}
			if options.OutputMode != "human" {
				return writeOutput(options, "admin-space", details)
			}
			return writeSpaceDetails(options, details)
		},
	}
}

func requireAccountAdminThroughPort(ctx context.Context, selected adminapp.Client) error {
	if err := selected.Graph().CheckAdminMFA(ctx); err != nil {
		return err
	}
	return nil
}

func writeAdminSpaceDetailsThroughPort(ctx context.Context, selected adminapp.Client, drive graph.Drive, options RunOptions) error {
	// Production always uses adminClientAdapter. This fallback keeps the domain
	// port independently testable without exposing the concrete runtime client.
	permissions, err := selected.Graph().ListSpacePermissions(ctx, drive.ID)
	if err != nil {
		return err
	}
	return writeOutput(options, "admin-space", map[string]any{"space": drive, "permissions": permissions})
}
