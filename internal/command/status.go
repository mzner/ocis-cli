package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newStatusCommand(options *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use: "status [PROFILE]", Short: "Show authentication status", Args: maximumArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			profile := ""
			if len(args) == 1 {
				profile = args[0]
			}
			return runAuth(command, options, app.AuthRequest{Operation: app.AuthStatus, Profile: profile})
		},
	}
}
