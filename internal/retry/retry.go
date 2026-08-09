// Package retry provides the shared bounded-retry policy used by every
// protocol adapter: which responses may be retried, and how long to wait.
package retry

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// MaxDelay bounds every wait between attempts. A retry delay is derived from
// values the server controls, so an unbounded delay would let a hostile or
// misconfigured server suspend the CLI for an arbitrary time; the request
// timeout cannot recover from it because no request is in flight while waiting.
//
// Waiting longer than this is indistinguishable from a hang. Retrying sooner is
// not the alternative: a server that asks for a long delay is usually
// throttling, and a follow-up request before the delay expires can worsen the
// throttling or extend a rate-limit ban. A server-requested delay beyond this
// ceiling therefore stops the operation, while a delay the CLI chose for itself
// is simply clamped.
const MaxDelay = 30 * time.Second

// DelayTooLongError reports that the server asked the CLI to wait longer than it
// is willing to. It names both durations so the caller can decide whether to run
// the command again later.
type DelayTooLongError struct {
	Requested time.Duration
	Ceiling   time.Duration
}

func (err *DelayTooLongError) Error() string {
	return fmt.Sprintf(
		"server asked to retry after %v, which exceeds the %v limit; "+
			"run the command again later",
		err.Requested, err.Ceiling,
	)
}

// RetryableStatus reports whether a response status may be retried. Only
// throttling and server-side failures qualify; a client error is a decision
// the server will repeat.
func RetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

// After returns the delay requested by a response's Retry-After header. It
// returns zero when the header is absent, unparsable, or already elapsed,
// leaving the caller's backoff to choose the delay. A value beyond MaxDelay is
// reported as-is rather than reduced, so that Delay can refuse it and say what
// was asked for; only the saturation needed to stay inside time.Duration is
// applied.
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
		case seconds >= int64(math.MaxInt64/time.Second):
			return math.MaxInt64
		default:
			return time.Duration(seconds) * time.Second
		}
	}
	if when, err := http.ParseTime(value); err == nil {
		return max(time.Until(when), 0)
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

// Delay returns how long to wait before the next attempt. A positive
// server-requested delay wins and is honored exactly, or refused with
// *DelayTooLongError when it exceeds MaxDelay; shortening it would send a
// follow-up request the server asked us not to send. Otherwise the base wait is
// doubled per attempt up to a fixed number of doublings and clamped at MaxDelay.
func Delay(
	base time.Duration, attempt int, serverDelay time.Duration,
) (time.Duration, error) {
	if serverDelay > 0 {
		if serverDelay > MaxDelay {
			return 0, &DelayTooLongError{Requested: serverDelay, Ceiling: MaxDelay}
		}
		return serverDelay, nil
	}
	return min(base*time.Duration(1<<min(max(attempt, 0), 4)), MaxDelay), nil
}

// Wait blocks for Delay or until the context ends, whichever happens first. It
// returns without waiting when the server-requested delay is refused.
func Wait(
	ctx context.Context, base time.Duration, attempt int, serverDelay time.Duration,
) error {
	delay, err := Delay(base, attempt, serverDelay)
	if err != nil {
		return err
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
