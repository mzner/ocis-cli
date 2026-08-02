package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newTagCommand(options *globalOptions) *cobra.Command {
	command := &cobra.Command{
		Use: "tag", Short: "Manage remote resource tags",
	}
	command.AddCommand(
		newTagListCommand(options),
		newTagAddCommand(options),
		newTagRemoveCommand(options),
	)
	return command
}

func newTagListCommand(options *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use: "list REMOTE_PATH", Aliases: []string{"ls"},
		Short: "List tags on a remote resource", Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runMetadata(command, options, app.MetadataRequest{
				Operation: app.MetadataTagList, Path: args[0],
			})
		},
	}
}

func newTagAddCommand(options *globalOptions) *cobra.Command {
	var dryRun bool
	command := &cobra.Command{
		Use:   "add REMOTE_PATH TAG [TAG...]",
		Short: "Add tags to a remote resource", Args: minimumArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			return runMetadata(command, options, app.MetadataRequest{
				Operation: app.MetadataTagAdd, Path: args[0],
				Tags: args[1:], DryRun: dryRun,
			})
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"resolve the resource and show the change without applying it",
	)
	return command
}

func newTagRemoveCommand(options *globalOptions) *cobra.Command {
	var dryRun bool
	command := &cobra.Command{
		Use: "remove REMOTE_PATH TAG [TAG...]", Aliases: []string{"rm"},
		Short: "Remove tags from a remote resource", Args: minimumArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			return runMetadata(command, options, app.MetadataRequest{
				Operation: app.MetadataTagRemove, Path: args[0],
				Tags: args[1:], DryRun: dryRun,
			})
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"resolve the resource and show the change without applying it",
	)
	return command
}
