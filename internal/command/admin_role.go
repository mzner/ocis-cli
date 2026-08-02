package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newAdminUserRoleCommand(options *globalOptions) *cobra.Command {
	command := &cobra.Command{
		Use:     "role",
		Aliases: []string{"roles"},
		Short:   "Inspect and manage server-advertised user roles",
		Long: "Inspect and manage roles advertised by the server. Role UUIDs " +
			"are discovered at runtime and are never hard-coded by the CLI.",
	}
	command.AddCommand(
		newAdminRoleAvailableCommand(options),
		newAdminRoleListCommand(options),
		newAdminRoleGrantCommand(options),
		newAdminRoleRevokeCommand(options),
	)
	return command
}

func newAdminRoleAvailableCommand(options *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "available",
		Short: "List roles advertised by the server",
		Args:  noArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runAdminRole(command, options, app.AdminRoleRequest{
				Operation: app.AdminRoleAvailable,
			})
		},
	}
}

func newAdminRoleListCommand(options *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "list USERNAME_OR_ID",
		Aliases: []string{"ls"},
		Short:   "List a user's assigned roles",
		Args:    exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runAdminRole(command, options, app.AdminRoleRequest{
				Operation: app.AdminRoleList, User: args[0],
			})
		},
	}
}

func newAdminRoleGrantCommand(options *globalOptions) *cobra.Command {
	var dryRun bool
	command := &cobra.Command{
		Use:   "grant USERNAME_OR_ID ROLE",
		Short: "Assign a server-advertised role to a user",
		Long: "Assign a role by exact display name or role ID. Current oCIS " +
			"may replace the user's existing role when a new role is assigned.",
		Args: exactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			return runAdminRole(command, options, app.AdminRoleRequest{
				Operation: app.AdminRoleGrant, User: args[0],
				Role: args[1], DryRun: dryRun,
			})
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"resolve the user and role without assigning it",
	)
	return command
}

func newAdminRoleRevokeCommand(options *globalOptions) *cobra.Command {
	var dryRun, yes bool
	command := &cobra.Command{
		Use:   "revoke USERNAME_OR_ID ROLE_OR_ASSIGNMENT_ID",
		Short: "Revoke an assigned role from a user",
		Args:  exactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			if !yes && !dryRun {
				confirmed, err := confirmAction(
					command, "Revoke role "+args[1]+" from user "+args[0]+"?",
				)
				if err != nil || !confirmed {
					return err
				}
			}
			return runAdminRole(command, options, app.AdminRoleRequest{
				Operation: app.AdminRoleRevoke, User: args[0],
				Role: args[1], DryRun: dryRun,
			})
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"resolve the assignment without revoking it",
	)
	command.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	return command
}

func runAdminRole(
	command *cobra.Command,
	options *globalOptions,
	request app.AdminRoleRequest,
) error {
	return app.RunAdminRoleWithOptions(
		command.Context(), request, options.profile, options.runOptions(command),
	)
}
