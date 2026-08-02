package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newSpaceUpdateCommand(options *globalOptions) *cobra.Command {
	var name, description, alias, quota string
	var dryRun bool
	command := &cobra.Command{
		Use: "update SPACE", Short: "Update project space metadata",
		Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			request := app.SpaceUpdateRequest{
				Identifier: args[0], DryRun: dryRun,
			}
			if command.Flags().Changed("name") {
				request.Name = &name
			}
			if command.Flags().Changed("description") {
				request.Description = &description
			}
			if command.Flags().Changed("alias") {
				request.Alias = &alias
			}
			if command.Flags().Changed("quota") {
				value, err := parseQuota(quota)
				if err != nil {
					return usageError("space update", err.Error())
				}
				if value == nil {
					return usageError(
						"space update",
						"default quota is only valid when creating a Space; omit --quota to leave it unchanged",
					)
				}
				request.Quota = value
			}
			return runSpaceUpdate(command, options, request)
		},
	}
	command.Flags().StringVar(&name, "name", "", "new Space name")
	command.Flags().StringVar(
		&description, "description", "", "new description; an empty value clears it",
	)
	command.Flags().StringVar(
		&alias, "alias", "", "new alias; an empty value clears it",
	)
	command.Flags().StringVar(
		&quota, "quota", "", "new quota such as 10GB or unlimited",
	)
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"resolve the Space and show changes without applying them",
	)
	return command
}
