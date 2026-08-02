package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newLoginCommand(options *globalOptions) *cobra.Command {
	var server, name, authType, username, clientID, acr string
	var insecure, noBrowser, mfa bool
	command := &cobra.Command{
		Use: "login [PROFILE]", Short: "Authenticate with an oCIS server", Args: maximumArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			profile := ""
			if len(args) == 1 {
				profile = args[0]
			}
			return runAuth(command, options, app.AuthRequest{
				Operation: app.AuthLogin, Profile: profile, Server: server, Name: name,
				Mode: authType, Username: username, ClientID: clientID,
				ACR: acr, Insecure: insecure, NoBrowser: noBrowser, MFA: mfa,
			})
		},
	}
	flags := command.Flags()
	flags.StringVar(&server, "server", "", "oCIS server URL")
	flags.StringVar(&name, "name", "", "name for a new server profile")
	flags.StringVar(&authType, "auth", "", "authentication mode: oidc or basic")
	flags.StringVar(&username, "username", "", "username for Basic authentication")
	flags.StringVar(&clientID, "client-id", "", "OIDC client ID")
	flags.BoolVar(&insecure, "insecure", false, "skip TLS certificate verification")
	flags.BoolVar(&noBrowser, "no-browser", false, "print the authorization URL without opening it")
	flags.BoolVar(
		&mfa, "mfa", false,
		"request the server-advertised MFA authentication level",
	)
	flags.StringVar(
		&acr, "acr", "",
		"explicit OIDC ACR value to request with --mfa",
	)
	return command
}
