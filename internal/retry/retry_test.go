package retry_test

import (
	"context"
	"net/http"
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

func TestAfterCapsServerControlledDelay(t *testing.T) {
	values := []string{
		"86400",
		"999999999999999999999",
		time.Now().Add(72 * time.Hour).UTC().Format(http.TimeFormat),
	}
	for _, value := range values {
		if got := retry.After(responseWith(value)); got != retry.MaxDelay {
			t.Fatalf(
				"Retry-After %q: got %v, want the %v ceiling",
				value, got, retry.MaxDelay,
			)
		}
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
		if got := retry.Delay(base, attempt, 0); got != want {
			t.Fatalf("Delay(attempt %d): got %v, want %v", attempt, got, want)
		}
	}
}

func TestDelayPrefersServerDelay(t *testing.T) {
	if got := retry.Delay(time.Millisecond, 0, 7*time.Second); got != 7*time.Second {
		t.Fatalf("delay: got %v, want 7s", got)
	}
}

func TestDelayCapsEveryDelay(t *testing.T) {
	if got := retry.Delay(time.Hour, 3, 0); got != retry.MaxDelay {
		t.Fatalf("backoff delay: got %v, want the %v ceiling", got, retry.MaxDelay)
	}
	if got := retry.Delay(time.Millisecond, 0, 24*time.Hour); got != retry.MaxDelay {
		t.Fatalf("server delay: got %v, want the %v ceiling", got, retry.MaxDelay)
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
	if err := retry.Wait(ctx, time.Hour, 0, time.Hour); err == nil {
		t.Fatal("expected the canceled context error")
	}
}

func responseWith(retryAfter string) *http.Response {
	header := http.Header{}
	if retryAfter != "" {
		header.Set("Retry-After", retryAfter)
	}
	return &http.Response{Header: header}
}
