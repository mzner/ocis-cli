package command

import "github.com/spf13/cobra"

func newFilesystemCommand(options *globalOptions) *cobra.Command {
	command := &cobra.Command{Use: "filesystem", Aliases: []string{"fs"}, Short: "Manage remote files"}
	command.AddCommand(
		newListCommand(options),
		newStatCommand(options),
		newCatCommand(options),
		newTreeCommand(options),
		newDUCommand(options),
		newBatchCommand(options),
		newUploadCommand(options),
		newDownloadCommand(options),
		newMkdirCommand(options),
		newTouchCommand(options),
		newMoveCommand(options),
		newCopyCommand(options),
		newRemoveCommand(options),
	)
	return command
}
