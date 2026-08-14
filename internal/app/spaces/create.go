package spaces

import (
	"context"
	"fmt"
	"strings"

	"github.com/mzner/ocis-cli/internal/apperror"
	"github.com/mzner/ocis-cli/internal/graph"
)

func RunCreate(
	ctx context.Context,
	request CreateRequest,
	selectedProfile string,
	options Options,
) error {
	options.Logger.Debug("run space create")
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" {
		return apperror.Wrap(
			apperror.KindUsage, "space create",
			fmt.Errorf("space name must not be empty"),
		)
	}
	if request.Quota != nil && *request.Quota < 0 {
		return apperror.Wrap(
			apperror.KindUsage, "space create",
			fmt.Errorf("space quota must not be negative"),
		)
	}

	createRequest := graph.CreateDriveRequest{
		Name: request.Name, Description: request.Description, DriveType: "project",
	}
	plannedQuota := any("server-default")
	if request.Quota != nil {
		createRequest.Quota = &graph.CreateQuota{Total: *request.Quota}
		plannedQuota = *request.Quota
	}
	if request.DryRun {
		return output(
			options, "space",
			map[string]any{
				"operation": "create", "name": request.Name,
				"description": request.Description, "driveType": "project",
				"quota": plannedQuota, "dryRun": true,
			},
			"Would create project space %s\n", request.Name,
		)
	}

	client, err := options.NewClient(ctx, selectedProfile)
	if err != nil {
		return err
	}
	created, err := client.Graph().CreateDrive(ctx, createRequest)
	if err != nil {
		return err
	}
	return output(
		options, "space", created,
		"Created project space %s (%s)\n", created.Name, created.ID,
	)
}
