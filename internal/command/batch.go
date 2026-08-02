package command

import (
	"fmt"
	"io"
	"os"

	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

const defaultBatchMaxOperations = 1000

func newBatchCommand(options *globalOptions) *cobra.Command {
	var dryRun, yes, continueOnError bool
	var maxOperations int
	command := &cobra.Command{
		Use:   "batch [JSONL_FILE]",
		Short: "Execute file operations from JSONL",
		Long: "Validate a complete JSONL document, then execute its file " +
			"operations sequentially. Batches are not atomic. Execution stops " +
			"at the first runtime failure unless --continue-on-error is set.",
		Args: maximumArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if maxOperations < 1 {
				return usageError(
					"batch", "--max-operations must be at least 1",
				)
			}
			if !dryRun && !yes {
				return usageError(
					"batch",
					"execution requires --yes; use --dry-run to preview",
				)
			}
			input := io.Reader(command.InOrStdin())
			if len(args) == 1 && args[0] != "-" {
				file, err := os.Open(args[0]) //nolint:gosec // path is the user-selected batch manifest
				if err != nil {
					return usageError(
						"batch", fmt.Sprintf("open %s: %v", args[0], err),
					)
				}
				defer func() { _ = file.Close() }()
				input = file
			}
			return app.RunBatchWithOptions(
				command.Context(), app.BatchRequest{
					Input: input, DryRun: dryRun, Confirmed: yes,
					ContinueOnError: continueOnError,
					MaxOperations:   maxOperations,
				}, options.profile, options.runOptions(command),
			)
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"validate and print the complete plan without changing files",
	)
	command.Flags().BoolVar(
		&yes, "yes", false, "confirm execution of the reviewed manifest",
	)
	command.Flags().BoolVar(
		&continueOnError, "continue-on-error", false,
		"continue after runtime failures and return the first failure's exit code",
	)
	command.Flags().IntVar(
		&maxOperations, "max-operations", defaultBatchMaxOperations,
		"maximum number of non-empty JSONL records",
	)
	return command
}
