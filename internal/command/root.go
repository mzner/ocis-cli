// Package command defines the Cobra adapter for the ocis application.
package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mzner/ocis-cli/internal/app"
	"github.com/mzner/ocis-cli/internal/apperror"
	"github.com/mzner/ocis-cli/internal/logging"
	appoutput "github.com/mzner/ocis-cli/internal/output"
	"github.com/spf13/cobra"
)

type globalOptions struct {
	json        bool
	jsonl       bool
	quiet       bool
	verbose     bool
	profile     string
	space       string
	timeout     time.Duration
	retries     int
	concurrency int
}

func (options *globalOptions) runOptions(command *cobra.Command) app.RunOptions {
	mode := appoutput.Human
	if options.json {
		mode = appoutput.JSON
	}
	if options.jsonl {
		mode = appoutput.JSONL
	}
	runOptions := app.RunOptions{
		OutputMode: mode, In: command.InOrStdin(),
		Out: command.OutOrStdout(), Err: command.ErrOrStderr(),
		Timeout: options.timeout, Retries: options.retries,
		Concurrency: options.concurrency, Quiet: options.quiet,
		Space: options.space,
	}
	if options.verbose {
		runOptions.Logger = logging.NewText(command.ErrOrStderr())
	}
	return runOptions
}

// OutputMode returns the machine-output mode selected on command.
func OutputMode(command *cobra.Command) appoutput.Mode {
	jsonOutput, _ := command.Flags().GetBool("json")
	jsonLines, _ := command.Flags().GetBool("jsonl")
	if jsonLines {
		return appoutput.JSONL
	}
	if jsonOutput {
		return appoutput.JSON
	}
	return appoutput.Human
}

// NewRootCommand constructs an isolated command tree.
func NewRootCommand() *cobra.Command {
	options := &globalOptions{}
	root := &cobra.Command{
		Use:           "ocis",
		Short:         "Manage files on an oCIS-compatible server",
		Version:       app.Version,
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(command *cobra.Command, _ []string) error {
			switch {
			case errors.Is(command.Context().Err(), context.Canceled):
				return apperror.Wrap(
					apperror.KindCanceled, command.CommandPath(), context.Canceled,
				)
			case options.json && options.jsonl:
				return apperror.Wrap(apperror.KindUsage, "output", errors.New("--json and --jsonl are mutually exclusive"))
			case options.retries < 0:
				return apperror.Wrap(apperror.KindUsage, "retries", errors.New("--retries cannot be negative"))
			case options.concurrency < 1:
				return apperror.Wrap(apperror.KindUsage, "concurrency", errors.New("--concurrency must be at least 1"))
			case options.timeout <= 0:
				return apperror.Wrap(apperror.KindUsage, "timeout", errors.New("--timeout must be greater than zero"))
			default:
				return nil
			}
		},
	}
	root.PersistentFlags().BoolVar(&options.json, "json", false, "write machine-readable JSON")
	root.PersistentFlags().BoolVar(&options.jsonl, "jsonl", false, "write newline-delimited JSON")
	root.PersistentFlags().BoolVarP(&options.quiet, "quiet", "q", false, "suppress transfer progress")
	root.PersistentFlags().BoolVar(&options.verbose, "verbose", false, "write diagnostic details to stderr")
	root.PersistentFlags().StringVar(&options.profile, "profile", "", "use a named server profile")
	root.PersistentFlags().StringVar(&options.space, "space", "", "use a space by name, alias, or ID")
	root.PersistentFlags().DurationVar(&options.timeout, "timeout", 5*time.Minute, "HTTP operation timeout")
	root.PersistentFlags().IntVar(&options.retries, "retries", 3, "retry temporary network and server failures")
	root.PersistentFlags().IntVar(&options.concurrency, "concurrency", 4, "maximum parallel file transfers")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return apperror.Wrap(apperror.KindUsage, "flags", err)
	})
	defaultHelp := root.HelpFunc()
	root.SetHelpFunc(func(command *cobra.Command, args []string) {
		if !command.HasAvailableSubCommands() {
			defaultHelp(command, args)
			return
		}
		_ = writeCommandHelp(command.OutOrStdout(), command)
	})

	root.AddCommand(
		newConfigCommand(options),
		newServerCommand(options),
		newAuthCommand(options),
		newFilesystemCommand(options),
		newLoginCommand(options),
		newStatusCommand(options),
		newLogoutCommand(options),
		newDoctorCommand(options),
		newAdminCommand(options),
		newSpaceCommand(options),
		newTrashCommand(options),
		newVersionCommand(options),
		newShareCommand(options),
		newSearchCommand(options),
		newSyncCommand(options),
		newTagCommand(options),
		newFavoriteCommand(options),
		newPropertyCommand(options),
		newListCommand(options),
		newStatCommand(options),
		newCatCommand(options),
		newTreeCommand(options),
		newDUCommand(options),
		newBatchCommand(options),
		newUploadCommand(options),
		newDownloadCommand(options),
		newMkdirCommand(options),
		newTouchCommand(options),
		newMoveCommand(options),
		newCopyCommand(options),
		newRemoveCommand(options),
		newCompletionCommand(root),
	)
	return root
}

func exactArgs(count int) cobra.PositionalArgs {
	return func(command *cobra.Command, args []string) error {
		if len(args) != count {
			return apperror.Wrap(apperror.KindUsage, command.CommandPath(), fmt.Errorf(
				"accepts %d arg(s), received %d", count, len(args),
			))
		}
		return nil
	}
}

func noArgs(command *cobra.Command, args []string) error {
	if len(args) != 0 {
		return apperror.Wrap(apperror.KindUsage, command.CommandPath(), fmt.Errorf(
			"accepts no arguments, received %d", len(args),
		))
	}
	return nil
}

func maximumArgs(count int) cobra.PositionalArgs {
	return func(command *cobra.Command, args []string) error {
		if len(args) > count {
			return apperror.Wrap(apperror.KindUsage, command.CommandPath(), fmt.Errorf(
				"accepts at most %d arg(s), received %d", count, len(args),
			))
		}
		return nil
	}
}

func minimumArgs(count int) cobra.PositionalArgs {
	return func(command *cobra.Command, args []string) error {
		if len(args) < count {
			return apperror.Wrap(
				apperror.KindUsage, command.CommandPath(),
				fmt.Errorf(
					"accepts at least %d arg(s), received %d",
					count, len(args),
				),
			)
		}
		return nil
	}
}

func writeCommandHelp(output io.Writer, command *cobra.Command) error {
	if _, err := fmt.Fprintf(
		output, "%s\n\nUsage:\n  %s [command]\n\nAvailable Commands:\n",
		command.Short, command.CommandPath(),
	); err != nil {
		return err
	}
	table := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	for _, child := range command.Commands() {
		if !child.IsAvailableCommand() || child.IsAdditionalHelpTopicCommand() {
			continue
		}
		name := child.Name()
		if len(child.Aliases) > 0 {
			name += ", " + strings.Join(child.Aliases, ", ")
		}
		if _, err := fmt.Fprintf(table, "  %s\t%s\n", name, child.Short); err != nil {
			return err
		}
	}
	if err := table.Flush(); err != nil {
		return err
	}
	localFlags := command.NonInheritedFlags()
	if localFlags.HasAvailableFlags() {
		if _, err := fmt.Fprintln(output, "\nFlags:"); err != nil {
			return err
		}
		if _, err := fmt.Fprint(output, localFlags.FlagUsages()); err != nil {
			return err
		}
	}
	inheritedFlags := command.InheritedFlags()
	if inheritedFlags.HasAvailableFlags() {
		if _, err := fmt.Fprintln(output, "\nGlobal Flags:"); err != nil {
			return err
		}
		if _, err := fmt.Fprint(output, inheritedFlags.FlagUsages()); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(
		output, "\nUse %q for more information about a command.\n",
		command.CommandPath()+" [command] --help",
	)
	return err
}
