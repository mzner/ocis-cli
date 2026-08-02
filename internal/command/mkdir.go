package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newMkdirCommand(options *globalOptions) *cobra.Command {
	var parents bool
	command := &cobra.Command{
		Use: "mkdir REMOTE_PATH", Short: "Create a remote directory", Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runFilesystem(command, options, app.FilesystemRequest{
				Operation: app.FilesystemMkdir, Source: args[0], Parents: parents,
			})
		},
	}
	command.Flags().BoolVarP(
		&parents, "parents", "p", false,
		"create missing parent directories and accept existing directories",
	)
	return command
}
