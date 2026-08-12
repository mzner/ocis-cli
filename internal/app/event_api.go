package app

import (
	"context"
	"time"
)

// EventWatchRequest selects real-time events to print.
type EventWatchRequest struct {
	Types   []string
	Once    bool
	MaxWait time.Duration
}

// RunEventWatchWithOptions watches authenticated real-time server events.
func RunEventWatchWithOptions(
	ctx context.Context,
	request EventWatchRequest,
	selectedProfile string,
	options RunOptions,
) error {
	return classifyProtocolError(
		"event watch",
		runEventWatch(ctx, request, selectedProfile, options.normalized()),
	)
}

// RunEventTypesWithOptions prints the event names known by this CLI. The
// server may emit additional names.
func RunEventTypesWithOptions(options RunOptions) error {
	return runEventTypes(options.normalized())
}
