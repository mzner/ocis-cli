package command

import (
	"time"

	"github.com/mzner/ocis-cli/internal/app"
	"github.com/mzner/ocis-cli/internal/eventstream"
	"github.com/spf13/cobra"
)

func newEventCommand(options *globalOptions) *cobra.Command {
	command := &cobra.Command{
		Use: "event", Aliases: []string{"events"},
		Short: "Watch real-time server events",
	}
	command.AddCommand(
		newEventWatchCommand(options),
		&cobra.Command{
			Use: "types", Short: "List event names known by this CLI", Args: exactArgs(0),
			RunE: func(command *cobra.Command, _ []string) error {
				return app.RunEventTypesWithOptions(options.runOptions(command))
			},
		},
	)
	return command
}

func newEventWatchCommand(options *globalOptions) *cobra.Command {
	var eventTypes []string
	var once bool
	var maxWait time.Duration
	command := &cobra.Command{
		Use: "watch", Short: "Watch future events until interrupted", Args: exactArgs(0),
		RunE: func(command *cobra.Command, _ []string) error {
			return app.RunEventWatchWithOptions(
				command.Context(), app.EventWatchRequest{
					Types: eventTypes, Once: once, MaxWait: maxWait,
				}, options.profile, options.runOptions(command),
			)
		},
	}
	command.Flags().StringSliceVar(
		&eventTypes, "type", nil,
		"show only this event type (repeat or comma-separate)",
	)
	command.Flags().BoolVar(
		&once, "once", false, "exit after the first matching event",
	)
	command.Flags().DurationVar(
		&maxWait, "max-wait", 0,
		"stop waiting for the first matching event after this duration",
	)
	_ = command.RegisterFlagCompletionFunc(
		"type", func(
			_ *cobra.Command, _ []string, _ string,
		) ([]string, cobra.ShellCompDirective) {
			values := make([]string, 0, len(eventstream.KnownTypes()))
			for _, eventType := range eventstream.KnownTypes() {
				values = append(values, eventType.Name+"\t"+eventType.Description)
			}
			return values, cobra.ShellCompDirectiveNoFileComp
		},
	)
	return command
}
