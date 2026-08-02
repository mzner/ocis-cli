package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newSpaceMemberCommand(options *globalOptions) *cobra.Command {
	command := &cobra.Command{
		Use: "member", Aliases: []string{"members"},
		Short: "Manage project space members",
	}
	command.AddCommand(
		newSpaceMemberListCommand(options),
		newSpaceMemberAddCommand(options),
		newSpaceMemberUpdateCommand(options),
		newSpaceMemberRemoveCommand(options),
	)
	return command
}

func newSpaceMemberListCommand(options *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use: "list SPACE", Aliases: []string{"ls"},
		Short: "List Space members", Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runSpaceMember(command, options, app.SpaceMemberRequest{
				Operation: app.SpaceMemberList, Space: args[0],
			})
		},
	}
}

func newSpaceMemberAddCommand(options *globalOptions) *cobra.Command {
	var role, recipientType string
	var dryRun, recipientIsID bool
	command := &cobra.Command{
		Use:   "add SPACE RECIPIENT",
		Short: "Add a user or group to a Space",
		Long: "Add a user or group to a project Space. RECIPIENT is searched by " +
			"username, email, or display name unless --recipient-id is set.",
		Args: exactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			return runSpaceMember(command, options, app.SpaceMemberRequest{
				Operation: app.SpaceMemberAdd, Space: args[0],
				RecipientID: args[1], RecipientIsID: recipientIsID,
				RecipientType: recipientType, Role: role, DryRun: dryRun,
			})
		},
	}
	command.Flags().StringVar(
		&role, "role", "viewer", "Space role name or server role ID",
	)
	command.Flags().StringVar(
		&recipientType, "type", "user", "recipient type: user or group",
	)
	command.Flags().BoolVar(
		&recipientIsID, "recipient-id", false,
		"treat RECIPIENT as an opaque Graph ID instead of searching",
	)
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"resolve the Space and role without adding the member",
	)
	return command
}

func newSpaceMemberUpdateCommand(options *globalOptions) *cobra.Command {
	var role string
	var dryRun bool
	command := &cobra.Command{
		Use: "update SPACE PERMISSION_ID", Short: "Change a Space member role",
		Args: exactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			return runSpaceMember(command, options, app.SpaceMemberRequest{
				Operation: app.SpaceMemberUpdate, Space: args[0],
				PermissionID: args[1], Role: role, DryRun: dryRun,
			})
		},
	}
	command.Flags().StringVar(
		&role, "role", "", "new Space role name or server role ID",
	)
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"resolve the Space and role without updating the member",
	)
	return command
}

func newSpaceMemberRemoveCommand(options *globalOptions) *cobra.Command {
	var dryRun, yes bool
	command := &cobra.Command{
		Use: "remove SPACE PERMISSION_ID", Aliases: []string{"rm"},
		Short: "Remove a member from a Space", Args: exactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			if !yes && !dryRun {
				confirmed, err := confirmAction(
					command,
					"Remove permission "+args[1]+" from Space "+args[0]+"?",
				)
				if err != nil || !confirmed {
					return err
				}
			}
			return runSpaceMember(command, options, app.SpaceMemberRequest{
				Operation: app.SpaceMemberRemove, Space: args[0],
				PermissionID: args[1], DryRun: dryRun,
			})
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"resolve the Space without removing the member",
	)
	command.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	return command
}
