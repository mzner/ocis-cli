package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newListCommand(options *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use: "ls [REMOTE_PATH]", Aliases: []string{"list"}, Short: "List a remote directory", Args: maximumArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			remote := "/"
			if len(args) == 1 {
				remote = args[0]
			}
			return runFilesystem(command, options, app.FilesystemRequest{Operation: app.FilesystemList, Source: remote})
		},
	}
}
