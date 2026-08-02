package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newTouchCommand(options *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "touch REMOTE_PATH",
		Short: "Create an empty remote file if missing",
		Args:  exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runFilesystem(command, options, app.FilesystemRequest{
				Operation: app.FilesystemTouch, Source: args[0],
			})
		},
	}
}
