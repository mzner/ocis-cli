package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newCopyCommand(options *globalOptions) *cobra.Command {
	return newTransferCommand(options, app.FilesystemCopy, "copy", "Copy a remote resource")
}

func newTransferCommand(options *globalOptions, operation app.FilesystemOperation, alias, short string) *cobra.Command {
	var overwrite, dryRun, interactive bool
	command := &cobra.Command{
		Use: string(operation) + " SOURCE DESTINATION", Aliases: []string{alias}, Short: short, Args: exactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			if interactive && !dryRun {
				confirmed, err := confirmAction(command, short+" to "+args[1]+"?")
				if err != nil || !confirmed {
					return err
				}
			}
			return runFilesystem(command, options, app.FilesystemRequest{
				Operation: operation, Source: args[0], Destination: args[1],
				Overwrite: overwrite, DryRun: dryRun,
			})
		},
	}
	command.Flags().BoolVar(&overwrite, "overwrite", false, "replace an existing destination")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "show the planned operation without changing files")
	command.Flags().BoolVarP(&interactive, "interactive", "i", false, "confirm before changing files")
	return command
}
