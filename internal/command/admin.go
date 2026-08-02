package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newAdminCommand(options *globalOptions) *cobra.Command {
	command := &cobra.Command{
		Use:   "admin",
		Short: "Manage accounts, groups, and server-visible Spaces",
		Long: "Manage administrative resources exposed by the oCIS LibreGraph API. " +
			"The server authorizes every operation; Space administration and " +
			"account administration are separate permissions.",
	}
	command.AddCommand(
		newAdminUserCommand(options),
		newAdminGroupCommand(options),
		newAdminSpaceCommand(options),
	)
	return command
}

func runAdmin(
	command *cobra.Command,
	options *globalOptions,
	request app.AdminRequest,
) error {
	return app.RunAdminWithOptions(
		command.Context(), request, options.profile, options.runOptions(command),
	)
}
