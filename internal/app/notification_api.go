package app

import "context"

// NotificationOperation identifies an unread-notification use case.
type NotificationOperation string

const (
	NotificationList    NotificationOperation = "list"
	NotificationInfo    NotificationOperation = "info"
	NotificationDismiss NotificationOperation = "dismiss"
	NotificationClear   NotificationOperation = "clear"
)

// NotificationRequest describes one notification operation.
type NotificationRequest struct {
	Operation NotificationOperation
	IDs       []string
	Search    string
	Confirmed bool
	DryRun    bool
}

// RunNotificationWithOptions manages unread in-app notifications.
func RunNotificationWithOptions(
	ctx context.Context,
	request NotificationRequest,
	selectedProfile string,
	options RunOptions,
) error {
	return classifyProtocolError(
		"notification "+string(request.Operation),
		runNotification(
			ctx, request, selectedProfile, options.normalized(),
		),
	)
}
