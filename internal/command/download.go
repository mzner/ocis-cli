package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newDownloadCommand(options *globalOptions) *cobra.Command {
	var recursive, noClobber, overwrite, dryRun, interactive bool
	verify := true
	command := &cobra.Command{
		Use: "download REMOTE_PATH LOCAL_PATH", Short: "Download a file or directory", Args: exactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			if noClobber && overwrite {
				return usageError("download", "--no-clobber and --overwrite are mutually exclusive")
			}
			if args[1] == "-" && (options.json || options.jsonl) {
				return usageError("download", "--json and --jsonl cannot be used when downloading to stdout")
			}
			if args[1] == "-" && noClobber {
				return usageError("download", "--no-clobber cannot be used when downloading to stdout")
			}
			if interactive && !dryRun {
				confirmed, err := confirmAction(command, "Download to "+args[1]+"?")
				if err != nil || !confirmed {
					return err
				}
			}
			return runFilesystem(command, options, app.FilesystemRequest{
				Operation: app.FilesystemDownload, Source: args[0], Destination: args[1],
				Recursive: recursive, NoClobber: noClobber, Overwrite: overwrite,
				DryRun: dryRun, Verify: verify,
			})
		},
	}
	command.Flags().BoolVarP(&recursive, "recursive", "r", false, "download directories recursively")
	command.Flags().BoolVar(&noClobber, "no-clobber", false, "fail if the local destination exists")
	command.Flags().BoolVar(&overwrite, "overwrite", false, "explicitly allow replacing the destination")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "show the planned transfer without changing files")
	command.Flags().BoolVarP(&interactive, "interactive", "i", false, "confirm before transferring")
	command.Flags().BoolVar(&verify, "verify", true, "verify the transferred size")
	return command
}
