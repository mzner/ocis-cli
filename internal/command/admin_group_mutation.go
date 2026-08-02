package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newAdminGroupCreateCommand(options *globalOptions) *cobra.Command {
	var dryRun bool
	command := &cobra.Command{
		Use:   "create NAME",
		Short: "Create a server group",
		Args:  exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runAdminGroupCreate(
				command, options, app.AdminGroupCreateRequest{
					Name: args[0], DryRun: dryRun,
				},
			)
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false, "show the operation without creating the group",
	)
	return command
}

func newAdminGroupUpdateCommand(options *globalOptions) *cobra.Command {
	var name string
	var dryRun bool
	command := &cobra.Command{
		Use:   "update NAME_OR_ID",
		Short: "Rename a server group",
		Args:  exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runAdminGroupUpdate(
				command, options, app.AdminGroupUpdateRequest{
					Identifier: args[0], Name: name, DryRun: dryRun,
				},
			)
		},
	}
	command.Flags().StringVar(&name, "name", "", "new group name (required)")
	command.Flags().BoolVar(
		&dryRun, "dry-run", false, "show the operation without renaming the group",
	)
	_ = command.MarkFlagRequired("name")
	return command
}

func newAdminGroupDeleteCommand(options *globalOptions) *cobra.Command {
	var dryRun, yes bool
	command := &cobra.Command{
		Use:   "delete NAME_OR_ID",
		Short: "Permanently delete a server group",
		Args:  exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if !yes && !dryRun {
				confirmed, err := confirmAction(
					command, "Permanently delete group "+args[0]+"?",
				)
				if err != nil || !confirmed {
					return err
				}
			}
			return runAdminGroupDelete(
				command, options, app.AdminGroupDeleteRequest{
					Identifier: args[0], DryRun: dryRun,
				},
			)
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"resolve the group and show the operation without deleting it",
	)
	command.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	return command
}

func newAdminGroupMemberAddCommand(options *globalOptions) *cobra.Command {
	var dryRun bool
	command := &cobra.Command{
		Use:   "add GROUP USER",
		Short: "Add a direct user member to a group",
		Long: "Add a direct user member to a group. oCIS does not support " +
			"nested groups as members.",
		Args: exactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			return runAdminGroupMemberMutation(
				command, options, app.AdminGroupMemberMutationRequest{
					Group: args[0], User: args[1], DryRun: dryRun,
				},
			)
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"resolve the group and user without adding the member",
	)
	return command
}

func newAdminGroupMemberRemoveCommand(options *globalOptions) *cobra.Command {
	var dryRun, yes bool
	command := &cobra.Command{
		Use:     "remove GROUP USER",
		Aliases: []string{"rm"},
		Short:   "Remove a direct user member from a group",
		Args:    exactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			if !yes && !dryRun {
				confirmed, err := confirmAction(
					command, "Remove user "+args[1]+" from group "+args[0]+"?",
				)
				if err != nil || !confirmed {
					return err
				}
			}
			return runAdminGroupMemberMutation(
				command, options, app.AdminGroupMemberMutationRequest{
					Group: args[0], User: args[1], Remove: true,
					DryRun: dryRun,
				},
			)
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"resolve the group and user without removing the member",
	)
	command.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	return command
}

func runAdminGroupCreate(
	command *cobra.Command,
	options *globalOptions,
	request app.AdminGroupCreateRequest,
) error {
	return app.RunAdminGroupCreateWithOptions(
		command.Context(), request, options.profile, options.runOptions(command),
	)
}

func runAdminGroupUpdate(
	command *cobra.Command,
	options *globalOptions,
	request app.AdminGroupUpdateRequest,
) error {
	return app.RunAdminGroupUpdateWithOptions(
		command.Context(), request, options.profile, options.runOptions(command),
	)
}

func runAdminGroupDelete(
	command *cobra.Command,
	options *globalOptions,
	request app.AdminGroupDeleteRequest,
) error {
	return app.RunAdminGroupDeleteWithOptions(
		command.Context(), request, options.profile, options.runOptions(command),
	)
}

func runAdminGroupMemberMutation(
	command *cobra.Command,
	options *globalOptions,
	request app.AdminGroupMemberMutationRequest,
) error {
	return app.RunAdminGroupMemberMutationWithOptions(
		command.Context(), request, options.profile, options.runOptions(command),
	)
}
