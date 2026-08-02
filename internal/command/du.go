package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

const (
	defaultDUMaxDepth   = 100
	defaultDUMaxEntries = 100000
)

func newDUCommand(options *globalOptions) *cobra.Command {
	var maxDepth, maxEntries int
	command := &cobra.Command{
		Use:   "du [REMOTE_PATH]",
		Short: "Summarize logical remote file size",
		Args:  maximumArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if maxDepth < 0 {
				return usageError("du", "--max-depth cannot be negative")
			}
			if maxEntries < 1 {
				return usageError("du", "--max-entries must be at least 1")
			}
			remote := "/"
			if len(args) == 1 {
				remote = args[0]
			}
			return runFilesystem(command, options, app.FilesystemRequest{
				Operation: app.FilesystemDU, Source: remote,
				MaxDepth: maxDepth, MaxEntries: maxEntries,
			})
		},
	}
	command.Flags().IntVar(
		&maxDepth, "max-depth", defaultDUMaxDepth,
		"maximum child depth below the requested path",
	)
	command.Flags().IntVar(
		&maxEntries, "max-entries", defaultDUMaxEntries,
		"maximum number of resources including the root",
	)
	return command
}
