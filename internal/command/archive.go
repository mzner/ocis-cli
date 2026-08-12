package command

import (
	"strings"

	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newArchiveCommand(options *globalOptions) *cobra.Command {
	command := &cobra.Command{
		Use: "archive", Aliases: []string{"archives"},
		Short: "Download server-created ZIP or TAR archives",
	}
	command.AddCommand(
		newArchiveDownloadCommand(options),
		&cobra.Command{
			Use: "formats", Short: "List server-supported archive formats", Args: noArgs,
			RunE: func(command *cobra.Command, _ []string) error {
				return app.RunArchiveFormatsWithOptions(
					command.Context(), options.profile, options.runOptions(command),
				)
			},
		},
	)
	return command
}

func newArchiveDownloadCommand(options *globalOptions) *cobra.Command {
	var destination, format string
	var overwrite, dryRun bool
	command := &cobra.Command{
		Use:   "download REMOTE_PATH...",
		Short: "Download selected resources as one archive",
		Args:  minimumArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if strings.TrimSpace(destination) == "" {
				return usageError("archive download", "--output is required")
			}
			return app.RunArchiveDownloadWithOptions(
				command.Context(), app.ArchiveDownloadRequest{
					Paths: append([]string(nil), args...), Destination: destination,
					Format: format, Overwrite: overwrite, DryRun: dryRun,
				}, options.profile, options.runOptions(command),
			)
		},
	}
	command.Flags().StringVarP(
		&destination, "output", "o", "", "local archive destination (required)",
	)
	command.Flags().StringVar(
		&format, "format", "",
		"archive format: zip or tar (inferred from --output; default zip)",
	)
	command.Flags().BoolVar(
		&overwrite, "overwrite", false,
		"explicitly allow replacing the local destination",
	)
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"resolve and measure resources without downloading an archive",
	)
	_ = command.RegisterFlagCompletionFunc(
		"format", func(
			_ *cobra.Command, _ []string, _ string,
		) ([]string, cobra.ShellCompDirective) {
			return []string{
				"zip\tZIP archive", "tar\tTAR archive",
			}, cobra.ShellCompDirectiveNoFileComp
		},
	)
	return command
}
