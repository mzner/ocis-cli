package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newCatCommand(options *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "cat REMOTE_PATH",
		Short: "Write a remote file to stdout",
		Args:  exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if options.json || options.jsonl {
				return usageError(
					"cat",
					"--json and --jsonl cannot be used because cat writes raw file bytes to stdout",
				)
			}
			return runFilesystem(command, options, app.FilesystemRequest{
				Operation: app.FilesystemCat,
				Source:    args[0],
			})
		},
	}
}
