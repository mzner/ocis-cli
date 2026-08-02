package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newPropertyCommand(options *globalOptions) *cobra.Command {
	command := &cobra.Command{
		Use: "property", Short: "Manage scalar custom WebDAV properties",
	}
	command.AddCommand(
		newPropertyGetCommand(options),
		newPropertySetCommand(options),
		newPropertyRemoveCommand(options),
	)
	return command
}

func newPropertyGetCommand(options *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "get REMOTE_PATH NAMESPACE NAME",
		Short: "Read a scalar custom WebDAV property", Args: exactArgs(3),
		RunE: func(command *cobra.Command, args []string) error {
			return runMetadata(command, options, app.MetadataRequest{
				Operation: app.MetadataPropertyGet, Path: args[0],
				Namespace: args[1], Name: args[2],
			})
		},
	}
}

func newPropertySetCommand(options *globalOptions) *cobra.Command {
	var dryRun bool
	command := &cobra.Command{
		Use:   "set REMOTE_PATH NAMESPACE NAME VALUE",
		Short: "Set a scalar custom WebDAV property", Args: exactArgs(4),
		RunE: func(command *cobra.Command, args []string) error {
			return runMetadata(command, options, app.MetadataRequest{
				Operation: app.MetadataPropertySet, Path: args[0],
				Namespace: args[1], Name: args[2], Value: args[3],
				DryRun: dryRun,
			})
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"resolve the resource and show the change without applying it",
	)
	return command
}

func newPropertyRemoveCommand(options *globalOptions) *cobra.Command {
	var dryRun bool
	command := &cobra.Command{
		Use: "remove REMOTE_PATH NAMESPACE NAME", Aliases: []string{"rm"},
		Short: "Remove a custom WebDAV property", Args: exactArgs(3),
		RunE: func(command *cobra.Command, args []string) error {
			return runMetadata(command, options, app.MetadataRequest{
				Operation: app.MetadataPropertyRemove, Path: args[0],
				Namespace: args[1], Name: args[2], DryRun: dryRun,
			})
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"resolve the resource and show the change without applying it",
	)
	return command
}
