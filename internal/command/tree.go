package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

const (
	defaultTreeMaxDepth   = 10
	defaultTreeMaxEntries = 10000
)

func newTreeCommand(options *globalOptions) *cobra.Command {
	var maxDepth, maxEntries int
	command := &cobra.Command{
		Use:   "tree [REMOTE_PATH]",
		Short: "List a bounded remote directory tree",
		Args:  maximumArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if maxDepth < 0 {
				return usageError("tree", "--max-depth cannot be negative")
			}
			if maxEntries < 1 {
				return usageError("tree", "--max-entries must be at least 1")
			}
			remote := "/"
			if len(args) == 1 {
				remote = args[0]
			}
			return runFilesystem(command, options, app.FilesystemRequest{
				Operation: app.FilesystemTree,
				Source:    remote, MaxDepth: maxDepth, MaxEntries: maxEntries,
			})
		},
	}
	command.Flags().IntVar(
		&maxDepth, "max-depth", defaultTreeMaxDepth,
		"maximum child depth below the requested path",
	)
	command.Flags().IntVar(
		&maxEntries, "max-entries", defaultTreeMaxEntries,
		"maximum number of resources including the root",
	)
	return command
}
