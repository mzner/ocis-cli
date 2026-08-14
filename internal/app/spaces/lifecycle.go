package spaces

import (
	"context"
	"fmt"
	"strings"

	"github.com/mzner/ocis-cli/internal/apperror"
)

func RunLifecycle(
	ctx context.Context,
	request LifecycleRequest,
	selectedProfile string,
	options Options,
) error {
	options.Logger.Debug(
		"run space lifecycle operation", "operation", request.Operation,
	)
	request.Identifier = strings.TrimSpace(request.Identifier)
	if request.Identifier == "" {
		return apperror.Wrap(
			apperror.KindUsage, "space lifecycle",
			fmt.Errorf("space identifier must not be empty"),
		)
	}
	switch request.Operation {
	case Disable:
		return disableSpace(ctx, request, selectedProfile, options)
	case Restore:
		return restoreSpace(ctx, request, selectedProfile, options)
	case Delete:
		if !request.Permanent {
			return apperror.Wrap(
				apperror.KindUsage, "space delete",
				fmt.Errorf(
					"permanent deletion requires explicit confirmation; use space disable for reversible removal",
				),
			)
		}
		return permanentlyDeleteSpace(ctx, request, selectedProfile, options)
	default:
		return apperror.Wrap(
			apperror.KindUsage, "space lifecycle",
			fmt.Errorf("unknown space lifecycle operation %q", request.Operation),
		)
	}
}

func disableSpace(
	ctx context.Context,
	request LifecycleRequest,
	selectedProfile string,
	options Options,
) error {
	client, selected, err := resolveProjectSpace(
		ctx, request.Identifier, selectedProfile, options,
	)
	if err != nil {
		return err
	}
	if request.DryRun {
		return output(
			options, "space",
			map[string]any{
				"operation": "disable", "id": selected.ID,
				"name": selected.Name, "dryRun": true,
			},
			"Would disable project space %s (%s)\n", selected.Name, selected.ID,
		)
	}
	if err := client.Graph().DeleteDrive(ctx, selected.ID, false); err != nil {
		return err
	}
	if err := client.ClearDefaultSpace(selected.ID); err != nil {
		return fmt.Errorf(
			"space was disabled but default-space configuration could not be cleared: %w",
			err,
		)
	}
	return output(
		options, "space",
		map[string]any{
			"operation": "disable", "id": selected.ID, "name": selected.Name,
		},
		"Disabled project space %s (%s)\n"+
			"Use this ID to restore it: %s\n",
		selected.Name, selected.ID, selected.ID,
	)
}

func restoreSpace(
	ctx context.Context,
	request LifecycleRequest,
	selectedProfile string,
	options Options,
) error {
	if request.DryRun {
		return output(
			options, "space",
			map[string]any{
				"operation": "restore", "id": request.Identifier, "dryRun": true,
			},
			"Would restore project space %s\n", request.Identifier,
		)
	}
	client, err := options.NewClient(ctx, selectedProfile)
	if err != nil {
		return err
	}
	restored, err := client.Graph().RestoreDrive(ctx, request.Identifier)
	if err != nil {
		return err
	}
	return output(
		options, "space", restored,
		"Restored project space %s (%s)\n", restored.Name, restored.ID,
	)
}

func permanentlyDeleteSpace(
	ctx context.Context,
	request LifecycleRequest,
	selectedProfile string,
	options Options,
) error {
	if request.DryRun {
		return output(
			options, "space",
			map[string]any{
				"operation": "delete", "id": request.Identifier,
				"permanent": true, "dryRun": true,
			},
			"Would permanently delete disabled project space %s\n",
			request.Identifier,
		)
	}
	client, err := options.NewClient(ctx, selectedProfile)
	if err != nil {
		return err
	}
	if err := client.Graph().DeleteDrive(
		ctx, request.Identifier, true,
	); err != nil {
		return err
	}
	return output(
		options, "space",
		map[string]any{
			"operation": "delete", "id": request.Identifier, "permanent": true,
		},
		"Permanently deleted project space %s\n", request.Identifier,
	)
}
