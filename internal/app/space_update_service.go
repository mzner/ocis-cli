package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/mzner/ocis-cli/internal/apperror"
	"github.com/mzner/ocis-cli/internal/graph"
)

func runSpaceUpdate(
	ctx context.Context,
	request SpaceUpdateRequest,
	selectedProfile string,
	options RunOptions,
) error {
	options.Logger.Debug("run space update")
	if request.Name == nil && request.Description == nil &&
		request.Alias == nil && request.Quota == nil {
		return apperror.Wrap(
			apperror.KindUsage, "space update",
			fmt.Errorf("at least one of --name, --description, --alias, or --quota is required"),
		)
	}
	if request.Name != nil {
		trimmed := strings.TrimSpace(*request.Name)
		if trimmed == "" {
			return apperror.Wrap(
				apperror.KindUsage, "space update",
				fmt.Errorf("space name must not be empty"),
			)
		}
		request.Name = &trimmed
	}
	if request.Quota != nil && *request.Quota < 0 {
		return apperror.Wrap(
			apperror.KindUsage, "space update",
			fmt.Errorf("space quota must not be negative"),
		)
	}

	client, selected, err := resolveProjectSpace(
		ctx, request.Identifier, selectedProfile, options,
	)
	if err != nil {
		return err
	}
	update := graph.UpdateDriveRequest{
		Name: request.Name, Description: request.Description,
		DriveAlias: request.Alias,
	}
	if request.Quota != nil {
		update.Quota = &graph.CreateQuota{Total: *request.Quota}
	}
	if request.DryRun {
		return output(
			options, "space",
			map[string]any{
				"operation": "update", "id": selected.ID, "name": selected.Name,
				"changes": update, "dryRun": true,
			},
			"Would update project space %s (%s)\n", selected.Name, selected.ID,
		)
	}
	updated, err := client.graphClient().UpdateDrive(ctx, selected.ID, update)
	if err != nil {
		return err
	}
	return output(
		options, "space", updated,
		"Updated project space %s (%s)\n", updated.Name, updated.ID,
	)
}

func resolveProjectSpace(
	ctx context.Context,
	identifier string,
	selectedProfile string,
	options RunOptions,
) (*client, space, error) {
	client, err := newClientWithOptions(ctx, selectedProfile, options)
	if err != nil {
		return nil, space{}, err
	}
	spaces, err := client.graphClient().ListDrives(ctx)
	if err != nil {
		return nil, space{}, err
	}
	selected, err := resolveSpace(spaces, identifier)
	if err != nil {
		return nil, space{}, err
	}
	if selected.DriveType != "project" {
		return nil, space{}, apperror.Wrap(
			apperror.KindUsage, "space",
			fmt.Errorf(
				"space %q has type %q; this operation requires a project space",
				selected.Name, selected.DriveType,
			),
		)
	}
	return client, selected, nil
}
