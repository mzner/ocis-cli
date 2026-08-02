package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func runMetadata(
	command *cobra.Command,
	options *globalOptions,
	request app.MetadataRequest,
) error {
	return app.RunMetadataWithOptions(
		command.Context(), request, options.profile,
		options.runOptions(command),
	)
}
