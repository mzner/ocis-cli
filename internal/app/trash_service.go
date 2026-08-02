package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
	"github.com/mzner/ocis-cli/internal/trash"
)

func runTrash(
	ctx context.Context,
	request TrashRequest,
	selectedProfile string,
	options RunOptions,
) error {
	options.Logger.Debug("run trash operation", "operation", request.Operation)
	if (request.Operation == TrashRemove || request.Operation == TrashEmpty) &&
		!request.Permanent {
		return apperror.Wrap(
			apperror.KindUsage, "trash "+string(request.Operation),
			fmt.Errorf("permanent deletion requires explicit confirmation"),
		)
	}
	client, err := newClientWithOptions(ctx, selectedProfile, options)
	if err != nil {
		return err
	}
	if err := client.selectSpace(options.Space); err != nil {
		return err
	}
	if client.space != nil && client.space.DriveType != "personal" &&
		client.space.DriveType != "project" {
		return apperror.Wrap(
			apperror.KindUsage, "trash",
			fmt.Errorf(
				"space %q has type %q; trash requires a personal or project Space",
				client.space.Name, client.space.DriveType,
			),
		)
	}
	switch request.Operation {
	case TrashList:
		return listTrash(ctx, client, options)
	case TrashRestore:
		return restoreTrash(ctx, client, request, options)
	case TrashRemove:
		return removeTrash(ctx, client, request, options)
	case TrashEmpty:
		return emptyTrash(ctx, client, request, options)
	default:
		return apperror.Wrap(
			apperror.KindUsage, "trash",
			fmt.Errorf("unknown trash command %q", request.Operation),
		)
	}
}

func (client *client) trashClient() *trash.Client {
	if client.recycle == nil {
		client.recycle = trash.NewClient(trash.Config{
			API: client.apiConfig(), Server: client.profile.Server,
			Username: client.profile.Username, SpaceID: client.selectedSpaceID(),
		}, client.http)
	}
	return client.recycle
}

func listTrash(
	ctx context.Context, client *client, options RunOptions,
) error {
	items, err := client.trashClient().List(ctx)
	if err != nil {
		return err
	}
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "trash-item", items)
	}
	for _, item := range items {
		size := "-"
		if item.Type == "file" {
			size = strconv.FormatInt(item.Size, 10)
		}
		if _, err := fmt.Fprintf(
			options.Out, "%-10s %10s  %-31s  %-32s  %s\n",
			item.Type, size, item.DeletedAt, item.OriginalPath, item.ID,
		); err != nil {
			return err
		}
	}
	return nil
}

func restoreTrash(
	ctx context.Context,
	client *client,
	request TrashRequest,
	options RunOptions,
) error {
	item, err := resolveTrashItem(ctx, client, request.ItemID)
	if err != nil {
		return err
	}
	if request.DryRun {
		return output(
			options, "trash-item",
			map[string]any{
				"operation": "restore", "item": item,
				"overwrite": request.Overwrite, "dryRun": true,
			},
			"Would restore %s to %s\n", item.ID, item.OriginalPath,
		)
	}
	if err := client.trashClient().Restore(
		ctx, item, request.Overwrite,
	); err != nil {
		return err
	}
	return output(
		options, "trash-item", item,
		"Restored %s to %s\n", item.ID, item.OriginalPath,
	)
}

func removeTrash(
	ctx context.Context,
	client *client,
	request TrashRequest,
	options RunOptions,
) error {
	item, err := resolveTrashItem(ctx, client, request.ItemID)
	if err != nil {
		return err
	}
	if request.DryRun {
		return output(
			options, "trash-item",
			map[string]any{
				"operation": "remove", "item": item,
				"permanent": true, "dryRun": true,
			},
			"Would permanently delete %s (%s)\n", item.OriginalPath, item.ID,
		)
	}
	if err := client.trashClient().Remove(ctx, item.ID); err != nil {
		return err
	}
	return output(
		options, "trash-item",
		map[string]any{
			"removed": item.ID, "originalPath": item.OriginalPath,
			"permanent": true,
		},
		"Permanently deleted %s (%s)\n", item.OriginalPath, item.ID,
	)
}

func emptyTrash(
	ctx context.Context,
	client *client,
	request TrashRequest,
	options RunOptions,
) error {
	items, err := client.trashClient().List(ctx)
	if err != nil {
		return err
	}
	if request.DryRun {
		return output(
			options, "trash",
			map[string]any{
				"operation": "empty", "items": len(items),
				"permanent": true, "dryRun": true,
			},
			"Would permanently delete %d trash item(s)\n", len(items),
		)
	}
	if len(items) == 0 {
		return output(
			options, "trash",
			map[string]any{"removed": 0, "permanent": true},
			"Trash is already empty\n",
		)
	}
	if err := client.trashClient().Empty(ctx); err != nil {
		return err
	}
	return output(
		options, "trash",
		map[string]any{"removed": len(items), "permanent": true},
		"Permanently deleted %d trash item(s)\n", len(items),
	)
}

func resolveTrashItem(
	ctx context.Context, client *client, itemID string,
) (trash.Item, error) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return trash.Item{}, apperror.Wrap(
			apperror.KindUsage, "trash", fmt.Errorf("trash item ID must not be empty"),
		)
	}
	items, err := client.trashClient().List(ctx)
	if err != nil {
		return trash.Item{}, err
	}
	for _, item := range items {
		if item.ID == itemID {
			return item, nil
		}
	}
	return trash.Item{}, apperror.Wrap(
		apperror.KindNotFound, "trash",
		fmt.Errorf("unknown trash item %q; run ocis trash list", itemID),
	)
}
