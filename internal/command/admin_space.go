package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newAdminSpaceCommand(options *globalOptions) *cobra.Command {
	create := guardAdminSpaceMutation(
		newSpaceCreateCommand(options), options,
	)
	update := guardAdminSpaceMutation(
		newSpaceUpdateCommand(options), options,
	)
	members := guardAdminSpaceMutation(
		newSpaceMemberCommand(options), options,
	)
	disable := guardAdminSpaceMutation(
		newSpaceDisableCommand(options), options,
	)
	restore := guardAdminSpaceMutation(
		newSpaceRestoreCommand(options), options,
	)
	deleteCommand := guardAdminSpaceMutation(
		newSpaceDeleteCommand(options), options,
	)
	command := &cobra.Command{
		Use:     "space",
		Aliases: []string{"spaces"},
		Short:   "Manage Spaces visible through the global drives endpoint",
	}
	command.AddCommand(
		create,
		update,
		members,
		disable,
		restore,
		deleteCommand,
		&cobra.Command{
			Use:     "list",
			Aliases: []string{"ls"},
			Short:   "List Spaces visible through the global drives endpoint",
			Args:    noArgs,
			RunE: func(command *cobra.Command, _ []string) error {
				return runAdmin(command, options, app.AdminRequest{
					Operation: app.AdminSpaceList,
				})
			},
		},
		&cobra.Command{
			Use:     "info SPACE",
			Aliases: []string{"stat"},
			Short:   "Show server-wide Space metadata",
			Long: "Show Space metadata by exact name, alias, or ID. Member " +
				"permissions are included only when the server grants access.",
			Args: exactArgs(1),
			RunE: func(command *cobra.Command, args []string) error {
				return runAdmin(command, options, app.AdminRequest{
					Operation:  app.AdminSpaceInfo,
					Identifier: args[0],
				})
			},
		},
	)
	return command
}

func guardAdminSpaceMutation(
	command *cobra.Command,
	options *globalOptions,
) *cobra.Command {
	if command.RunE != nil {
		run := command.RunE
		command.RunE = func(command *cobra.Command, args []string) error {
			if err := app.RunAdminSpaceMFACheckWithOptions(
				command.Context(), options.profile,
				options.runOptions(command),
			); err != nil {
				return err
			}
			return run(command, args)
		}
	}
	for _, child := range command.Commands() {
		guardAdminSpaceMutation(child, options)
	}
	return command
}
