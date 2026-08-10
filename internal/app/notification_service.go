package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mzner/ocis-cli/internal/apperror"
	"github.com/mzner/ocis-cli/internal/notifications"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

func runNotification(
	ctx context.Context,
	request NotificationRequest,
	selectedProfile string,
	options RunOptions,
) error {
	if request.Operation == NotificationClear && !request.DryRun &&
		!request.Confirmed {
		return usageNotification(
			"clearing every notification requires explicit confirmation",
		)
	}
	client, err := newClientWithOptions(ctx, selectedProfile, options)
	if err != nil {
		return err
	}
	switch request.Operation {
	case NotificationList:
		return listNotifications(ctx, client, request.Search, options)
	case NotificationInfo:
		return showNotification(ctx, client, firstNotificationID(request.IDs), options)
	case NotificationDismiss:
		return dismissNotifications(ctx, client, request.IDs, request.DryRun, options)
	case NotificationClear:
		return clearNotifications(ctx, client, request.DryRun, options)
	default:
		return usageNotification(fmt.Sprintf(
			"unknown notification command %q", request.Operation,
		))
	}
}

func listNotifications(
	ctx context.Context,
	client *client,
	search string,
	options RunOptions,
) error {
	values, err := client.notificationsClient().List(ctx)
	if err != nil {
		return err
	}
	values = filterNotifications(values, search)
	sortNotifications(values)
	return writeNotifications(values, options)
}

func showNotification(
	ctx context.Context,
	client *client,
	id string,
	options RunOptions,
) error {
	value, err := resolveNotification(ctx, client, id)
	if err != nil {
		return err
	}
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "notification", value)
	}
	if _, err := fmt.Fprintf(options.Out, "ID:       %s\n", value.ID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(options.Out, "Date:     %s\n", value.DateTime); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(options.Out, "Subject:  %s\n", value.Subject); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(options.Out, "Message:  %s\n", oneLine(value.Message)); err != nil {
		return err
	}
	if value.User != "" {
		if _, err := fmt.Fprintf(options.Out, "Actor:    %s\n", value.User); err != nil {
			return err
		}
	}
	if value.ObjectType != "" || value.ObjectID != "" {
		_, err := fmt.Fprintf(
			options.Out, "Object:   %s %s\n", value.ObjectType, value.ObjectID,
		)
		return err
	}
	return nil
}

func dismissNotifications(
	ctx context.Context,
	client *client,
	ids []string,
	dryRun bool,
	options RunOptions,
) error {
	selected, err := resolveNotifications(ctx, client, ids)
	if err != nil {
		return err
	}
	result := map[string]any{
		"operation": "dismiss", "notifications": selected,
		"count": len(selected), "dryRun": dryRun,
	}
	if dryRun {
		return output(
			options, "notification-dismissal", result,
			"Would dismiss %d notification(s)\n", len(selected),
		)
	}
	selectedIDs := notificationIDs(selected)
	if len(selectedIDs) == 1 {
		err = client.notificationsClient().Dismiss(ctx, selectedIDs[0])
	} else {
		err = client.notificationsClient().DismissMany(ctx, selectedIDs)
	}
	if err != nil {
		return err
	}
	result["dryRun"] = false
	return output(
		options, "notification-dismissal", result,
		"Dismissed %d notification(s)\n", len(selected),
	)
}

func clearNotifications(
	ctx context.Context,
	client *client,
	dryRun bool,
	options RunOptions,
) error {
	values, err := client.notificationsClient().List(ctx)
	if err != nil {
		return err
	}
	result := map[string]any{
		"operation": "clear", "count": len(values), "dryRun": dryRun,
	}
	if dryRun {
		return output(
			options, "notification-clear", result,
			"Would dismiss %d notification(s)\n", len(values),
		)
	}
	if len(values) == 0 {
		return output(
			options, "notification-clear", result,
			"There are no unread notifications\n",
		)
	}
	if err := client.notificationsClient().DismissMany(
		ctx, notificationIDs(values),
	); err != nil {
		return err
	}
	return output(
		options, "notification-clear", result,
		"Dismissed %d notification(s)\n", len(values),
	)
}

func writeNotifications(
	values []notifications.Notification, options RunOptions,
) error {
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "notification", values)
	}
	writer := tabwriter.NewWriter(options.Out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "DATE\tSUBJECT\tMESSAGE\tID"); err != nil {
		return err
	}
	for _, value := range values {
		if _, err := fmt.Fprintf(
			writer, "%s\t%s\t%s\t%s\n", value.DateTime,
			oneLine(value.Subject), oneLine(value.Message), value.ID,
		); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func filterNotifications(
	values []notifications.Notification, search string,
) []notifications.Notification {
	search = strings.ToLower(strings.TrimSpace(search))
	if search == "" {
		return values
	}
	result := make([]notifications.Notification, 0, len(values))
	for _, value := range values {
		haystack := strings.ToLower(strings.Join([]string{
			value.ID, value.App, value.User, value.ObjectID, value.ObjectType,
			value.Subject, value.Message,
		}, "\x00"))
		if strings.Contains(haystack, search) {
			result = append(result, value)
		}
	}
	return result
}

func resolveNotification(
	ctx context.Context, client *client, id string,
) (notifications.Notification, error) {
	values, err := resolveNotifications(ctx, client, []string{id})
	if err != nil {
		return notifications.Notification{}, err
	}
	return values[0], nil
}

func resolveNotifications(
	ctx context.Context,
	client *client,
	ids []string,
) ([]notifications.Notification, error) {
	requested := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			return nil, usageNotification("notification ID must not be empty")
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		requested = append(requested, id)
	}
	if len(requested) == 0 {
		return nil, usageNotification("at least one notification ID is required")
	}
	values, err := client.notificationsClient().List(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]notifications.Notification, len(values))
	for _, value := range values {
		byID[value.ID] = value
	}
	selected := make([]notifications.Notification, 0, len(requested))
	for _, id := range requested {
		value, exists := byID[id]
		if !exists {
			return nil, apperror.Wrap(
				apperror.KindNotFound, "notification",
				fmt.Errorf(
					"unknown notification %q; run ocis notification list", id,
				),
			)
		}
		selected = append(selected, value)
	}
	return selected, nil
}

func sortNotifications(values []notifications.Notification) {
	sort.SliceStable(values, func(left, right int) bool {
		leftTime, leftErr := time.Parse(time.RFC3339Nano, values[left].DateTime)
		rightTime, rightErr := time.Parse(time.RFC3339Nano, values[right].DateTime)
		if leftErr == nil && rightErr == nil && !leftTime.Equal(rightTime) {
			return leftTime.After(rightTime)
		}
		if values[left].DateTime != values[right].DateTime {
			return values[left].DateTime > values[right].DateTime
		}
		return values[left].ID < values[right].ID
	})
}

func notificationIDs(values []notifications.Notification) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.ID)
	}
	return result
}

func firstNotificationID(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func usageNotification(message string) error {
	return apperror.Wrap(
		apperror.KindUsage, "notification", errors.New(message),
	)
}
