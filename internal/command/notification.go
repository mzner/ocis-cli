package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newNotificationCommand(options *globalOptions) *cobra.Command {
	command := &cobra.Command{
		Use: "notification", Aliases: []string{"notifications"},
		Short: "Manage unread in-app notifications",
	}
	command.AddCommand(
		newNotificationListCommand(options),
		newNotificationInfoCommand(options),
		newNotificationDismissCommand(options),
		newNotificationClearCommand(options),
	)
	return command
}

func newNotificationListCommand(options *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use: "list [SEARCH]", Aliases: []string{"ls"},
		Short: "List unread notifications", Args: maximumArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			search := ""
			if len(args) == 1 {
				search = args[0]
			}
			return runNotification(command, options, app.NotificationRequest{
				Operation: app.NotificationList, Search: search,
			})
		},
	}
}

func newNotificationInfoCommand(options *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use: "info ID", Short: "Show one unread notification", Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runNotification(command, options, app.NotificationRequest{
				Operation: app.NotificationInfo, IDs: []string{args[0]},
			})
		},
	}
}

func newNotificationDismissCommand(options *globalOptions) *cobra.Command {
	var dryRun bool
	command := &cobra.Command{
		Use: "dismiss ID [ID...]", Aliases: []string{"read", "remove", "rm", "delete"},
		Short: "Dismiss notifications from the unread list", Args: minimumArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runNotification(command, options, app.NotificationRequest{
				Operation: app.NotificationDismiss, IDs: args, DryRun: dryRun,
			})
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"resolve notifications without dismissing them",
	)
	return command
}

func newNotificationClearCommand(options *globalOptions) *cobra.Command {
	var dryRun, yes bool
	command := &cobra.Command{
		Use: "clear", Aliases: []string{"read-all"},
		Short: "Dismiss every unread notification", Args: noArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if !yes && !dryRun {
				confirmed, err := confirmAction(
					command, "Dismiss every unread notification?",
				)
				if err != nil || !confirmed {
					return err
				}
			}
			return runNotification(command, options, app.NotificationRequest{
				Operation: app.NotificationClear, Confirmed: true, DryRun: dryRun,
			})
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"count unread notifications without dismissing them",
	)
	command.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	return command
}

func runNotification(
	command *cobra.Command,
	options *globalOptions,
	request app.NotificationRequest,
) error {
	return app.RunNotificationWithOptions(
		command.Context(), request, options.profile, options.runOptions(command),
	)
}
