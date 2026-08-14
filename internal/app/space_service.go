package app

import (
	"context"
	"fmt"
	"strings"

	spacesapp "github.com/mzner/ocis-cli/internal/app/spaces"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

func runSpace(
	ctx context.Context, request SpaceRequest, selectedProfile string, options RunOptions,
) error {
	options.Logger.Debug("run space operation", "operation", request.Operation)
	switch request.Operation {
	case SpaceUnset:
		return unsetDefaultSpace(selectedProfile, options)
	case SpaceCurrent:
		return currentSpace(ctx, selectedProfile, options)
	}
	client, err := newClientWithOptions(ctx, selectedProfile, options)
	if err != nil {
		return err
	}
	switch request.Operation {
	case SpaceList:
		spaces, err := client.graphClient().ListMyDrives(ctx)
		if err != nil {
			return err
		}
		if options.OutputMode != appoutput.Human {
			return writeOutput(options, "space", spaces)
		}
		for _, value := range spaces {
			current := " "
			if value.ID == client.profile.DefaultSpace ||
				client.profile.DefaultSpace == "" && value.DriveType == "personal" {
				current = "*"
			}
			_, _ = fmt.Fprintf(
				options.Out, "%s %-12s %-24s %s\n",
				current, value.DriveType, value.Name, value.ID,
			)
		}
		return nil
	case SpaceInfo:
		spaces, err := client.graphClient().ListDrives(ctx)
		if err != nil {
			return err
		}
		value, err := resolveSpace(spaces, request.Identifier)
		if err != nil {
			return err
		}
		details, err := loadSpaceDetailsThroughDomain(ctx, client, value, options)
		if err != nil {
			return err
		}
		if options.OutputMode != appoutput.Human {
			return writeOutput(options, "space", details)
		}
		return spacesapp.WriteDetails(spacesOptions(options), details)
	case SpaceUse:
		spaces, err := client.graphClient().ListMyDrives(ctx)
		if err != nil {
			return err
		}
		value, err := resolveSpace(spaces, request.Identifier)
		if err != nil {
			return err
		}
		selected := client.store.Profiles[client.name]
		selected.DefaultSpace = value.ID
		selected.DefaultSpaceOwner = profileIdentity(selected)
		client.store.Profiles[client.name] = selected
		if err := saveStore(options.Dependencies, client.store); err != nil {
			return err
		}
		return output(
			options, "space",
			map[string]string{"id": value.ID, "name": value.Name},
			"Using space %s (%s)\n", value.Name, value.ID,
		)
	default:
		return apperror.Wrap(
			apperror.KindUsage, "space",
			fmt.Errorf("unknown space command %q", request.Operation),
		)
	}
}

func unsetDefaultSpace(selectedProfile string, options RunOptions) error {
	selectedStore, err := loadStore(options.Dependencies)
	if err != nil {
		return err
	}
	name, selected, err := selectProfile(selectedStore, selectedProfile)
	if err != nil {
		return err
	}
	previous := selected.DefaultSpace
	selected.DefaultSpace = ""
	selected.DefaultSpaceOwner = ""
	selectedStore.Profiles[name] = selected
	if err := saveStore(options.Dependencies, selectedStore); err != nil {
		return err
	}
	return output(
		options, "space-selection",
		map[string]any{
			"profile": name, "mode": "personal", "implicit": true,
			"previousSpaceId": previous,
		},
		"Using personal files for profile %s (no explicit default Space)\n", name,
	)
}

func currentSpace(
	ctx context.Context, selectedProfile string, options RunOptions,
) error {
	selectedStore, err := loadStore(options.Dependencies)
	if err != nil {
		return err
	}
	name, selected, err := selectProfile(selectedStore, selectedProfile)
	if err != nil {
		return err
	}
	if selected.DefaultSpace == "" {
		return output(
			options, "space-selection",
			map[string]any{
				"profile": name, "mode": "personal", "implicit": true,
			},
			"Profile: %s\nDefault: personal files (implicit)\n", name,
		)
	}
	client, err := newClientWithOptions(ctx, selectedProfile, options)
	if err != nil {
		return err
	}
	if err := client.selectSpace(""); err != nil {
		return err
	}
	current := *client.space
	return output(
		options, "space-selection",
		map[string]any{
			"profile": name, "mode": "space", "implicit": false,
			"space": current,
		},
		"Profile: %s\nDefault: %s (%s)\n", name, current.Name, current.ID,
	)
}

func (client *client) selectSpace(identifier string) error {
	storedDefault := identifier == "" && client.profile.DefaultSpace != ""
	if identifier == "" {
		identifier = client.profile.DefaultSpace
	}
	if identifier == "" {
		return nil
	}
	spaces, err := client.graphClient().ListMyDrives(client.context())
	if err != nil {
		return err
	}
	selected, err := resolveSpace(spaces, identifier)
	if err != nil {
		if storedDefault {
			profile := client.store.Profiles[client.name]
			profile.DefaultSpace = ""
			profile.DefaultSpaceOwner = ""
			client.store.Profiles[client.name] = profile
			client.profile = profile
			if saveErr := saveStore(client.dependencies, client.store); saveErr != nil {
				return fmt.Errorf(
					"selected Space %q is no longer available, and its stale selection could not be cleared: %w",
					identifier, saveErr,
				)
			}
			return apperror.Wrap(
				apperror.KindUsage, "space",
				fmt.Errorf(
					"selected Space %q is no longer available and was cleared; "+
						"choose another with `ocis space use`, then retry the command: %w",
					identifier, err,
				),
			)
		}
		return err
	}
	client.space = &selected
	return nil
}

func resolveSpace(spaces []space, identifier string) (space, error) {
	var matches []space
	for _, value := range spaces {
		if value.ID == identifier ||
			strings.EqualFold(value.Name, identifier) ||
			strings.EqualFold(value.DriveAlias, identifier) {
			matches = append(matches, value)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return space{}, apperror.Wrap(
			apperror.KindUsage, "space",
			fmt.Errorf("unknown space %q; run ocis space list", identifier),
		)
	default:
		return space{}, apperror.Wrap(
			apperror.KindUsage, "space",
			fmt.Errorf("space name %q is ambiguous; use its ID", identifier),
		)
	}
}
