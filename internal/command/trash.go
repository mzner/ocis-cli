package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newTrashCommand(options *globalOptions) *cobra.Command {
	command := &cobra.Command{
		Use: "trash", Short: "Manage deleted resources in the selected Space",
	}
	command.AddCommand(
		&cobra.Command{
			Use: "list", Aliases: []string{"ls"}, Short: "List deleted resources",
			Args: noArgs,
			RunE: func(command *cobra.Command, _ []string) error {
				return runTrash(command, options, app.TrashRequest{
					Operation: app.TrashList,
				})
			},
		},
		newTrashRestoreCommand(options),
		newTrashRemoveCommand(options),
		newTrashEmptyCommand(options),
	)
	return command
}

func newTrashRestoreCommand(options *globalOptions) *cobra.Command {
	var overwrite, dryRun bool
	command := &cobra.Command{
		Use: "restore ITEM_ID", Short: "Restore a resource to its original path",
		Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runTrash(command, options, app.TrashRequest{
				Operation: app.TrashRestore, ItemID: args[0],
				Overwrite: overwrite, DryRun: dryRun,
			})
		},
	}
	command.Flags().BoolVar(
		&overwrite, "overwrite", false,
		"replace an existing resource at the original path",
	)
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"resolve the item and show its destination without restoring it",
	)
	return command
}

func newTrashRemoveCommand(options *globalOptions) *cobra.Command {
	var dryRun, yes bool
	command := &cobra.Command{
		Use: "remove ITEM_ID", Aliases: []string{"rm", "delete"},
		Short: "Permanently delete one trash item", Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if !yes && !dryRun {
				confirmed, err := confirmAction(
					command,
					"Permanently delete trash item "+args[0]+"?",
				)
				if err != nil || !confirmed {
					return err
				}
			}
			return runTrash(command, options, app.TrashRequest{
				Operation: app.TrashRemove, ItemID: args[0],
				Permanent: true, DryRun: dryRun,
			})
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"show the item without permanently deleting it",
	)
	command.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	return command
}

func newTrashEmptyCommand(options *globalOptions) *cobra.Command {
	var dryRun, yes bool
	command := &cobra.Command{
		Use: "empty", Short: "Permanently delete every item in the selected trash",
		Args: noArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if !yes && !dryRun {
				confirmed, err := confirmAction(
					command,
					"Permanently delete every item in the selected trash?",
				)
				if err != nil || !confirmed {
					return err
				}
			}
			return runTrash(command, options, app.TrashRequest{
				Operation: app.TrashEmpty, Permanent: true, DryRun: dryRun,
			})
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"count items without permanently deleting them",
	)
	command.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	return command
}
