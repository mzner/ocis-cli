package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newSpaceDisableCommand(options *globalOptions) *cobra.Command {
	var dryRun, yes bool
	command := &cobra.Command{
		Use: "disable SPACE", Short: "Disable a project space without deleting data",
		Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if !yes && !dryRun {
				confirmed, err := confirmAction(
					command,
					"Disable Space "+args[0]+" for all members?",
				)
				if err != nil || !confirmed {
					return err
				}
			}
			return runSpaceLifecycle(command, options, app.SpaceLifecycleRequest{
				Operation: app.SpaceDisable, Identifier: args[0], DryRun: dryRun,
			})
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"resolve the Space and show the operation without disabling it",
	)
	command.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	return command
}

func newSpaceRestoreCommand(options *globalOptions) *cobra.Command {
	var dryRun bool
	command := &cobra.Command{
		Use: "restore SPACE_ID", Short: "Restore a disabled project space",
		Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runSpaceLifecycle(command, options, app.SpaceLifecycleRequest{
				Operation: app.SpaceRestore, Identifier: args[0], DryRun: dryRun,
			})
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"show the operation without restoring the Space",
	)
	return command
}

func newSpaceDeleteCommand(options *globalOptions) *cobra.Command {
	var dryRun, permanent, yes bool
	command := &cobra.Command{
		Use: "delete SPACE_ID", Short: "Permanently delete a disabled project space",
		Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if !permanent {
				return usageError(
					"space delete",
					"--permanent is required; use space disable for reversible removal",
				)
			}
			if !yes && !dryRun {
				confirmed, err := confirmAction(
					command,
					"Permanently delete disabled Space "+args[0]+" and all its data?",
				)
				if err != nil || !confirmed {
					return err
				}
			}
			return runSpaceLifecycle(command, options, app.SpaceLifecycleRequest{
				Operation: app.SpaceDelete, Identifier: args[0],
				Permanent: true, DryRun: dryRun,
			})
		},
	}
	command.Flags().BoolVar(
		&permanent, "permanent", false,
		"confirm that permanent deletion is intended",
	)
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"show the operation without deleting the Space",
	)
	command.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	return command
}
