// Package retry provides the shared bounded-retry policy used by every
// protocol adapter: which responses may be retried, and how long to wait.
package retry

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// MaxDelay bounds every wait between attempts. A retry delay is derived from
// values the server controls, so an unbounded delay would let a hostile or
// misconfigured server suspend the CLI for an arbitrary time; the request
// timeout cannot recover from it because no request is in flight while waiting.
// Waiting longer than this is indistinguishable from a hang, so an excessive
// hint is clamped rather than honored or discarded: the retry still happens,
// just no later than this.
const MaxDelay = 30 * time.Second

// RetryableStatus reports whether a response status may be retried. Only
// throttling and server-side failures qualify; a client error is a decision
// the server will repeat.
func RetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

// After returns the delay requested by a response's Retry-After header, capped
// at MaxDelay. It returns zero when the header is absent, unparsable, or
// already elapsed, leaving the caller's backoff to choose the delay.
func After(response *http.Response) time.Duration {
	if response == nil {
		return 0
	}
	// RFC 9110 defines exactly two forms: delta-seconds and an HTTP-date.
	value := strings.TrimSpace(response.Header.Get("Retry-After"))
	if seconds, ok := deltaSeconds(value); ok {
		switch {
		case seconds <= 0:
			return 0
		// Compared in seconds because converting first would overflow
		// time.Duration and wrap to a negative delay.
		case seconds >= int64(MaxDelay/time.Second):
			return MaxDelay
		default:
			return time.Duration(seconds) * time.Second
		}
	}
	if when, err := http.ParseTime(value); err == nil {
		return min(max(time.Until(when), 0), MaxDelay)
	}
	return 0
}

// deltaSeconds decodes the delta-seconds form. A value too large for int64
// saturates instead of being rejected, so that an absurd hint is capped by the
// caller rather than silently treated as "no delay requested".
func deltaSeconds(value string) (int64, bool) {
	seconds, err := strconv.ParseInt(value, 10, 64)
	switch {
	case err == nil:
		return seconds, true
	case errors.Is(err, strconv.ErrRange):
		return seconds, true
	default:
		return 0, false
	}
}

// Delay returns how long to wait before the next attempt, capped at MaxDelay.
// A positive server-requested delay wins; otherwise the base wait is doubled
// per attempt up to a fixed number of doublings.
func Delay(base time.Duration, attempt int, serverDelay time.Duration) time.Duration {
	if serverDelay <= 0 {
		serverDelay = base * time.Duration(1<<min(max(attempt, 0), 4))
	}
	return min(serverDelay, MaxDelay)
}

// Wait blocks for Delay or until the context ends, whichever happens first.
func Wait(
	ctx context.Context, base time.Duration, attempt int, serverDelay time.Duration,
) error {
	timer := time.NewTimer(Delay(base, attempt, serverDelay))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
