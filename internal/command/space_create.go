package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newSpaceCreateCommand(options *globalOptions) *cobra.Command {
	var description, quota string
	var dryRun bool
	command := &cobra.Command{
		Use: "create NAME", Short: "Create a project space", Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			quotaBytes, err := parseQuota(quota)
			if err != nil {
				return usageError("space create", err.Error())
			}
			return runSpaceCreate(command, options, app.SpaceCreateRequest{
				Name: args[0], Description: description,
				Quota: quotaBytes, DryRun: dryRun,
			})
		},
	}
	command.Flags().StringVar(
		&description, "description", "", "human-readable space description",
	)
	command.Flags().StringVar(
		&quota, "quota", "default",
		"quota in bytes or units such as 500MB, 10GB, or unlimited",
	)
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"show the planned space without creating it",
	)
	return command
}
