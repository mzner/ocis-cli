package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newConfigCommand(options *globalOptions) *cobra.Command {
	command := &cobra.Command{
		Use:   "config",
		Short: "Inspect local CLI configuration",
	}
	command.AddCommand(
		&cobra.Command{
			Use:   "path",
			Short: "Print the active configuration-file path",
			Args:  noArgs,
			RunE: func(command *cobra.Command, _ []string) error {
				return runConfig(command, options, app.ConfigRequest{
					Operation: app.ConfigPath,
				})
			},
		},
		&cobra.Command{
			Use:   "paths",
			Short: "Show all local storage paths",
			Args:  noArgs,
			RunE: func(command *cobra.Command, _ []string) error {
				return runConfig(command, options, app.ConfigRequest{
					Operation: app.ConfigPaths,
				})
			},
		},
		&cobra.Command{
			Use:   "show",
			Short: "Show effective non-secret configuration",
			Args:  noArgs,
			RunE: func(command *cobra.Command, _ []string) error {
				return runConfig(command, options, app.ConfigRequest{
					Operation: app.ConfigShow, Profile: options.profile,
				})
			},
		},
	)
	return command
}
