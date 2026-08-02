package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newUploadCommand(options *globalOptions) *cobra.Command {
	var recursive, noClobber, overwrite, dryRun, interactive bool
	verify := true
	command := &cobra.Command{
		Use: "upload LOCAL_PATH REMOTE_PATH", Short: "Upload a file or directory", Args: exactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			if noClobber && overwrite {
				return usageError("upload", "--no-clobber and --overwrite are mutually exclusive")
			}
			if interactive && !dryRun {
				confirmed, err := confirmAction(command, "Upload to "+args[1]+"?")
				if err != nil || !confirmed {
					return err
				}
			}
			return runFilesystem(command, options, app.FilesystemRequest{
				Operation: app.FilesystemUpload, Source: args[0], Destination: args[1],
				Recursive: recursive, NoClobber: noClobber, Overwrite: overwrite,
				DryRun: dryRun, Verify: verify,
			})
		},
	}
	command.Flags().BoolVarP(&recursive, "recursive", "r", false, "upload directories recursively")
	command.Flags().BoolVar(&noClobber, "no-clobber", false, "fail if the remote destination exists")
	command.Flags().BoolVar(&overwrite, "overwrite", false, "explicitly allow replacing the destination")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "show the planned transfer without changing files")
	command.Flags().BoolVarP(&interactive, "interactive", "i", false, "confirm before transferring")
	command.Flags().BoolVar(&verify, "verify", true, "verify the transferred size")
	return command
}
