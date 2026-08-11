package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newActivityCommand(options *globalOptions) *cobra.Command {
	command := &cobra.Command{
		Use: "activity", Aliases: []string{"activities"},
		Short: "Inspect file, folder, and Space activity history",
	}
	command.AddCommand(newActivityListCommand(options))
	return command
}

func newActivityListCommand(options *globalOptions) *cobra.Command {
	var depth, limit int
	var sortOrder string
	command := &cobra.Command{
		Use: "list [REMOTE_PATH]", Aliases: []string{"ls"},
		Short: "List activity history", Args: maximumArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			remote := ""
			if len(args) == 1 {
				remote = args[0]
			}
			return app.RunActivityWithOptions(
				command.Context(), app.ActivityRequest{
					Path: remote, Depth: depth,
					DepthSet: command.Flags().Changed("depth"),
					Limit:    limit, Sort: sortOrder,
				}, options.profile, options.runOptions(command),
			)
		},
	}
	command.Flags().IntVar(
		&depth, "depth", -1,
		"include descendants to this depth (-1 for the complete subtree)",
	)
	command.Flags().IntVar(
		&limit, "limit", 100,
		"maximum activities to return (-1 for the server limit)",
	)
	command.Flags().StringVar(
		&sortOrder, "sort", "desc", "sort by recorded time (asc or desc)",
	)
	return command
}
