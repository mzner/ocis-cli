package retry_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mzner/ocis-cli/internal/retry"
)

func TestRetryableStatus(t *testing.T) {
	cases := map[int]bool{
		http.StatusOK:                  false,
		http.StatusNotFound:            false,
		http.StatusConflict:            false,
		http.StatusTooManyRequests:     true,
		http.StatusInternalServerError: true,
		http.StatusServiceUnavailable:  true,
	}
	for status, want := range cases {
		if got := retry.RetryableStatus(status); got != want {
			t.Fatalf("RetryableStatus(%d) = %v, want %v", status, got, want)
		}
	}
}

func TestAfterParsesDeltaSeconds(t *testing.T) {
	if got := retry.After(responseWith("5")); got != 5*time.Second {
		t.Fatalf("delay: got %v, want 5s", got)
	}
}

func TestAfterParsesHTTPDate(t *testing.T) {
	when := time.Now().Add(4 * time.Second).UTC().Format(http.TimeFormat)
	got := retry.After(responseWith(when))
	if got <= 0 || got > 5*time.Second {
		t.Fatalf("delay: got %v, want a positive value near 4s", got)
	}
}

// TestAfterReportsAnExcessiveDelayWithoutSaturating checks that a delay beyond
// anything worth waiting for is still reported as the large value it is, so the
// policy can refuse it and name it, rather than being silently rounded down to
// the ceiling or wrapping to a negative duration.
func TestAfterReportsAnExcessiveDelayWithoutSaturating(t *testing.T) {
	values := []string{
		"86400",
		"999999999999999999999",
		time.Now().Add(72 * time.Hour).UTC().Format(http.TimeFormat),
	}
	for _, value := range values {
		if got := retry.After(responseWith(value)); got <= retry.MaxDelay {
			t.Fatalf(
				"Retry-After %q: got %v, want a value above the %v ceiling",
				value, got, retry.MaxDelay,
			)
		}
	}
}

// TestDelayRefusesToRetryBeforeTheServerAllows covers the case where honoring
// Retry-After would exceed the local ceiling. Waiting that long is
// indistinguishable from a hang, and retrying sooner contradicts legitimate
// throttling guidance and can extend a rate-limit ban, so the operation stops
// with both durations named.
func TestDelayRefusesToRetryBeforeTheServerAllows(t *testing.T) {
	_, err := retry.Delay(time.Millisecond, 0, 24*time.Hour)
	if err == nil {
		t.Fatal("expected an excessive Retry-After to stop the operation")
	}
	var excessive *retry.DelayTooLongError
	if !errors.As(err, &excessive) {
		t.Fatalf("error type: got %T, want *retry.DelayTooLongError", err)
	}
	if excessive.Requested != 24*time.Hour || excessive.Ceiling != retry.MaxDelay {
		t.Fatalf("durations: got %v and %v", excessive.Requested, excessive.Ceiling)
	}
	if !strings.Contains(err.Error(), "24h0m0s") ||
		!strings.Contains(err.Error(), retry.MaxDelay.String()) {
		t.Fatalf("message must name both durations: %v", err)
	}
}

func TestDelayHonorsAServerDelayWithinTheCeiling(t *testing.T) {
	got, err := retry.Delay(time.Millisecond, 0, retry.MaxDelay)
	if err != nil || got != retry.MaxDelay {
		t.Fatalf("delay: got %v, %v; want the ceiling honored", got, err)
	}
}

func TestWaitRefusesAnExcessiveServerDelayPromptly(t *testing.T) {
	started := time.Now()
	err := retry.Wait(context.Background(), time.Millisecond, 0, time.Hour)
	if err == nil {
		t.Fatal("expected an excessive Retry-After to stop the operation")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("elapsed: got %v, want a prompt refusal", elapsed)
	}
}

func TestAfterIgnoresUnusableValues(t *testing.T) {
	for _, value := range []string{"", "soon", "-5", "0", "5m3", "1h", "1.5"} {
		if got := retry.After(responseWith(value)); got != 0 {
			t.Fatalf("Retry-After %q: got %v, want 0", value, got)
		}
	}
}

func TestAfterToleratesSurroundingWhitespace(t *testing.T) {
	if got := retry.After(responseWith("  2  ")); got != 2*time.Second {
		t.Fatalf("delay: got %v, want 2s", got)
	}
}

func TestDelayGrowsExponentiallyFromTheBase(t *testing.T) {
	base := 200 * time.Millisecond
	for attempt, want := range []time.Duration{
		200 * time.Millisecond,
		400 * time.Millisecond,
		800 * time.Millisecond,
		1600 * time.Millisecond,
		3200 * time.Millisecond,
		3200 * time.Millisecond,
	} {
		got, err := retry.Delay(base, attempt, 0)
		if err != nil || got != want {
			t.Fatalf("Delay(attempt %d): got %v, %v; want %v", attempt, got, err, want)
		}
	}
}

func TestDelayPrefersServerDelay(t *testing.T) {
	got, err := retry.Delay(time.Millisecond, 0, 7*time.Second)
	if err != nil || got != 7*time.Second {
		t.Fatalf("delay: got %v, %v; want 7s", got, err)
	}
}

// TestDelayCapsLocalBackoff covers the ceiling for a delay the CLI chose
// itself. Unlike a server-requested delay, shortening it contradicts nothing,
// so it is clamped rather than refused.
func TestDelayCapsLocalBackoff(t *testing.T) {
	got, err := retry.Delay(time.Hour, 3, 0)
	if err != nil || got != retry.MaxDelay {
		t.Fatalf("backoff delay: got %v, %v; want the %v ceiling", got, err, retry.MaxDelay)
	}
}

func TestWaitSleepsTheComputedDelay(t *testing.T) {
	started := time.Now()
	if err := retry.Wait(context.Background(), 10*time.Millisecond, 2, 0); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 40*time.Millisecond {
		t.Fatalf("elapsed: got %v, want at least 40ms", elapsed)
	}
}

func TestWaitReturnsContextError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := retry.Wait(
		ctx, time.Hour, 0, retry.MaxDelay,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want the canceled context error", err)
	}
}

func responseWith(retryAfter string) *http.Response {
	header := http.Header{}
	if retryAfter != "" {
		header.Set("Retry-After", retryAfter)
	}
	return &http.Response{Header: header}
}
