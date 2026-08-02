package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newServerCommand(options *globalOptions) *cobra.Command {
	command := &cobra.Command{Use: "server", Short: "Manage oCIS server profiles"}
	command.AddCommand(
		newServerAddCommand(options),
		&cobra.Command{
			Use: "list", Aliases: []string{"ls"}, Short: "List configured servers", Args: noArgs,
			RunE: func(command *cobra.Command, _ []string) error {
				return runServer(command, options, app.ServerRequest{Operation: app.ServerList})
			},
		},
		&cobra.Command{
			Use: "use NAME", Short: "Select the default server", Args: exactArgs(1),
			RunE: func(command *cobra.Command, args []string) error {
				return runServer(command, options, app.ServerRequest{Operation: app.ServerUse, Name: args[0]})
			},
		},
		&cobra.Command{
			Use: "remove NAME", Aliases: []string{"rm"}, Short: "Remove a server profile", Args: exactArgs(1),
			RunE: func(command *cobra.Command, args []string) error {
				return runServer(command, options, app.ServerRequest{Operation: app.ServerRemove, Name: args[0]})
			},
		},
	)
	return command
}

func newServerAddCommand(options *globalOptions) *cobra.Command {
	var insecure bool
	var clientID string
	command := &cobra.Command{
		Use: "add NAME URL", Short: "Add an oCIS server profile", Args: exactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			return runServer(command, options, app.ServerRequest{
				Operation: app.ServerAdd, Name: args[0], Server: args[1],
				ClientID: clientID, Insecure: insecure,
			})
		},
	}
	command.Flags().BoolVar(&insecure, "insecure", false, "skip TLS certificate verification for this profile")
	command.Flags().StringVar(&clientID, "client-id", "", "OIDC client ID")
	return command
}
