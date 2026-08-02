package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newRemoveCommand(options *globalOptions) *cobra.Command {
	var recursive, dryRun, interactive bool
	command := &cobra.Command{
		Use: "rm REMOTE_PATH", Aliases: []string{"remove"}, Short: "Delete a remote resource", Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if interactive && !dryRun {
				confirmed, err := confirmAction(command, "Delete "+args[0]+"?")
				if err != nil || !confirmed {
					return err
				}
			}
			return runFilesystem(command, options, app.FilesystemRequest{
				Operation: app.FilesystemRemove, Source: args[0], Recursive: recursive, DryRun: dryRun,
			})
		},
	}
	command.Flags().BoolVarP(&recursive, "recursive", "r", false, "allow deleting directories")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "show the planned deletion without changing files")
	command.Flags().BoolVarP(&interactive, "interactive", "i", false, "confirm before deleting")
	return command
}
