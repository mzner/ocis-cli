package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newLogoutCommand(options *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use: "logout [PROFILE]", Short: "Remove saved authentication", Args: maximumArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			profile := ""
			if len(args) == 1 {
				profile = args[0]
			}
			return runAuth(command, options, app.AuthRequest{Operation: app.AuthLogout, Profile: profile})
		},
	}
}
