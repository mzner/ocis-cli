package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newVersionCommand(options *globalOptions) *cobra.Command {
	command := &cobra.Command{
		Use: "version", Aliases: []string{"versions"},
		Short: "Inspect, download, and restore historical file versions",
	}
	command.AddCommand(
		&cobra.Command{
			Use: "list REMOTE_PATH", Aliases: []string{"ls"},
			Short: "List historical versions of a file", Args: exactArgs(1),
			RunE: func(command *cobra.Command, args []string) error {
				return runVersion(command, options, app.VersionRequest{
					Operation: app.VersionList, Path: args[0],
				})
			},
		},
		&cobra.Command{
			Use: "info REMOTE_PATH VERSION_ID", Aliases: []string{"stat"},
			Short: "Show historical version metadata", Args: exactArgs(2),
			RunE: func(command *cobra.Command, args []string) error {
				return runVersion(command, options, app.VersionRequest{
					Operation: app.VersionInfo, Path: args[0],
					VersionID: args[1],
				})
			},
		},
		newVersionDownloadCommand(options),
		newVersionRestoreCommand(options),
	)
	return command
}

func newVersionDownloadCommand(options *globalOptions) *cobra.Command {
	var noClobber, dryRun bool
	verify := true
	command := &cobra.Command{
		Use:     "download REMOTE_PATH VERSION_ID LOCAL_PATH",
		Aliases: []string{"get"}, Short: "Download a historical file version",
		Args: exactArgs(3),
		RunE: func(command *cobra.Command, args []string) error {
			if args[2] == "-" && (options.json || options.jsonl) {
				return usageError(
					"version download",
					"--json and --jsonl cannot be used when downloading to stdout",
				)
			}
			if args[2] == "-" && noClobber {
				return usageError(
					"version download",
					"--no-clobber cannot be used when downloading to stdout",
				)
			}
			return runVersion(command, options, app.VersionRequest{
				Operation: app.VersionDownload, Path: args[0],
				VersionID: args[1], Destination: args[2],
				NoClobber: noClobber, Verify: verify, DryRun: dryRun,
			})
		},
	}
	command.Flags().BoolVar(
		&noClobber, "no-clobber", false,
		"fail if the local destination exists",
	)
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"resolve the version without downloading content",
	)
	command.Flags().BoolVar(
		&verify, "verify", true,
		"verify downloaded size and available ETag consistency",
	)
	return command
}

func newVersionRestoreCommand(options *globalOptions) *cobra.Command {
	var dryRun, yes bool
	command := &cobra.Command{
		Use:   "restore REMOTE_PATH VERSION_ID",
		Short: "Make a historical version the current file content",
		Args:  exactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			if !yes && !dryRun {
				confirmed, err := confirmAction(
					command,
					"Restore version "+args[1]+" over the current "+args[0]+"?",
				)
				if err != nil || !confirmed {
					return err
				}
			}
			return runVersion(command, options, app.VersionRequest{
				Operation: app.VersionRestore, Path: args[0],
				VersionID: args[1], Confirmed: true, DryRun: dryRun,
			})
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"resolve the version without restoring it",
	)
	command.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	return command
}
