package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newMoveCommand(options *globalOptions) *cobra.Command {
	return newTransferCommand(options, app.FilesystemMove, "move", "Move a remote resource")
}
