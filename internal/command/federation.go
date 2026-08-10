package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newFederationCommand(options *globalOptions) *cobra.Command {
	command := &cobra.Command{
		Use: "federation", Aliases: []string{"federated", "ocm"},
		Short: "Manage Open Cloud Mesh invitations and connections",
	}
	command.AddCommand(
		newFederationInviteCommand(options),
		newFederationConnectionCommand(options),
	)
	return command
}

func newFederationInviteCommand(options *globalOptions) *cobra.Command {
	command := &cobra.Command{
		Use: "invite", Aliases: []string{"invitation"},
		Short: "Manage federation invitations",
	}
	command.AddCommand(
		newFederationInviteCreateCommand(options),
		newFederationInviteListCommand(options),
		newFederationInviteAcceptCommand(options),
	)
	return command
}

func newFederationInviteCreateCommand(options *globalOptions) *cobra.Command {
	var email, description string
	command := &cobra.Command{
		Use: "create", Short: "Create a federation invitation", Args: noArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runFederation(command, options, app.FederationRequest{
				Operation: app.FederationInviteCreate,
				Email:     email, Description: description,
			})
		},
	}
	command.Flags().StringVar(&email, "email", "", "optional recipient email")
	command.Flags().StringVar(
		&description, "description", "", "optional invitation description",
	)
	return command
}

func newFederationInviteListCommand(options *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use: "list", Aliases: []string{"ls"},
		Short: "List active federation invitations", Args: noArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runFederation(command, options, app.FederationRequest{
				Operation: app.FederationInviteList,
			})
		},
	}
}

func newFederationInviteAcceptCommand(options *globalOptions) *cobra.Command {
	var provider string
	command := &cobra.Command{
		Use: "accept [TOKEN]", Short: "Accept a federation invitation",
		Args: maximumArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			token := ""
			var err error
			if len(args) == 1 {
				token = args[0]
			} else {
				token, err = readSecret(
					command, "OCIS_FEDERATION_INVITE_TOKEN", "Invitation token: ",
				)
				if err != nil {
					return err
				}
			}
			return runFederation(command, options, app.FederationRequest{
				Operation: app.FederationInviteAccept,
				Token:     token, Provider: provider,
			})
		},
	}
	command.Flags().StringVar(
		&provider, "provider", "",
		"invitation issuer host, URL, or host:port (required)",
	)
	_ = command.MarkFlagRequired("provider")
	return command
}

func newFederationConnectionCommand(options *globalOptions) *cobra.Command {
	command := &cobra.Command{
		Use: "connection", Aliases: []string{"connections"},
		Short: "Manage accepted federation connections",
	}
	command.AddCommand(
		newFederationConnectionListCommand(options),
		newFederationConnectionRemoveCommand(options),
	)
	return command
}

func newFederationConnectionListCommand(options *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use: "list [SEARCH]", Aliases: []string{"ls"},
		Short: "List accepted federation connections", Args: maximumArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			search := ""
			if len(args) == 1 {
				search = args[0]
			}
			return runFederation(command, options, app.FederationRequest{
				Operation: app.FederationConnectionList, Identifier: search,
			})
		},
	}
}

func newFederationConnectionRemoveCommand(options *globalOptions) *cobra.Command {
	var provider string
	var userID, dryRun, yes bool
	command := &cobra.Command{
		Use: "remove USER", Aliases: []string{"rm"},
		Short: "Remove an accepted federation connection", Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if !yes && !dryRun {
				confirmed, err := confirmAction(
					command, "Remove federation connection "+args[0]+"?",
				)
				if err != nil || !confirmed {
					return err
				}
			}
			return runFederation(command, options, app.FederationRequest{
				Operation:  app.FederationConnectionRemove,
				Identifier: args[0], Provider: provider, UserID: userID,
				Confirmed: true, DryRun: dryRun,
			})
		},
	}
	command.Flags().StringVar(
		&provider, "provider", "", "restrict the match to one provider",
	)
	command.Flags().BoolVar(
		&userID, "user-id", false, "treat USER as an opaque federated user ID",
	)
	command.Flags().BoolVar(
		&dryRun, "dry-run", false, "resolve the connection without removing it",
	)
	command.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	return command
}

func runFederation(
	command *cobra.Command,
	options *globalOptions,
	request app.FederationRequest,
) error {
	return app.RunFederationWithOptions(
		command.Context(), request, options.profile, options.runOptions(command),
	)
}
