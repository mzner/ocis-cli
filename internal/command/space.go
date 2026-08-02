package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newSpaceCommand(options *globalOptions) *cobra.Command {
	command := &cobra.Command{
		Use: "space", Short: "Manage oCIS spaces",
	}
	command.AddCommand(
		newSpaceCreateCommand(options),
		newSpaceUpdateCommand(options),
		newSpaceMemberCommand(options),
		newSpaceDisableCommand(options),
		newSpaceRestoreCommand(options),
		newSpaceDeleteCommand(options),
		&cobra.Command{
			Use: "current", Short: "Show the current default Space selection",
			Args: noArgs,
			RunE: func(command *cobra.Command, _ []string) error {
				return runSpace(command, options, app.SpaceRequest{
					Operation: app.SpaceCurrent,
				})
			},
		},
		&cobra.Command{
			Use: "unset", Aliases: []string{"clear"},
			Short: "Return to the implicit personal-file root", Args: noArgs,
			RunE: func(command *cobra.Command, _ []string) error {
				return runSpace(command, options, app.SpaceRequest{
					Operation: app.SpaceUnset,
				})
			},
		},
		&cobra.Command{
			Use: "list", Aliases: []string{"ls"}, Short: "List available spaces",
			Args: noArgs,
			RunE: func(command *cobra.Command, _ []string) error {
				return runSpace(command, options, app.SpaceRequest{
					Operation: app.SpaceList,
				})
			},
		},
		&cobra.Command{
			Use: "info SPACE", Aliases: []string{"stat"},
			Short: "Show Space metadata, quota, members, and permissions",
			Args:  exactArgs(1),
			RunE: func(command *cobra.Command, args []string) error {
				return runSpace(command, options, app.SpaceRequest{
					Operation: app.SpaceInfo, Identifier: args[0],
				})
			},
		},
		&cobra.Command{
			Use: "use SPACE", Short: "Select the default space for this profile",
			Args: exactArgs(1),
			RunE: func(command *cobra.Command, args []string) error {
				return runSpace(command, options, app.SpaceRequest{
					Operation: app.SpaceUse, Identifier: args[0],
				})
			},
		},
	)
	return command
}
