package command

import "github.com/spf13/cobra"

func newAuthCommand(options *globalOptions) *cobra.Command {
	command := &cobra.Command{Use: "auth", Short: "Authenticate server profiles"}
	command.AddCommand(
		newAuthSetupCommand(options),
		newLoginCommand(options),
		newStatusCommand(options),
		newLogoutCommand(options),
	)
	return command
}
