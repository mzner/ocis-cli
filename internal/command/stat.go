package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newStatCommand(options *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use: "stat REMOTE_PATH", Short: "Show remote resource metadata", Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runFilesystem(command, options, app.FilesystemRequest{Operation: app.FilesystemStat, Source: args[0]})
		},
	}
}
