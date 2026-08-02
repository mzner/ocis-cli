package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newFavoriteCommand(options *globalOptions) *cobra.Command {
	command := &cobra.Command{
		Use: "favorite", Short: "Manage remote resource favorites",
	}
	command.AddCommand(
		newFavoriteSetCommand(options),
		newFavoriteUnsetCommand(options),
	)
	return command
}

func newFavoriteSetCommand(options *globalOptions) *cobra.Command {
	var dryRun bool
	command := &cobra.Command{
		Use:   "set REMOTE_PATH",
		Short: "Mark a remote resource as a favorite", Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runMetadata(command, options, app.MetadataRequest{
				Operation: app.MetadataFavoriteSet,
				Path:      args[0], DryRun: dryRun,
			})
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"resolve the resource and show the change without applying it",
	)
	return command
}

func newFavoriteUnsetCommand(options *globalOptions) *cobra.Command {
	var dryRun bool
	command := &cobra.Command{
		Use:   "unset REMOTE_PATH",
		Short: "Unmark a remote resource as a favorite", Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runMetadata(command, options, app.MetadataRequest{
				Operation: app.MetadataFavoriteUnset,
				Path:      args[0], DryRun: dryRun,
			})
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"resolve the resource and show the change without applying it",
	)
	return command
}
