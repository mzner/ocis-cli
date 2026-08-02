package logging

import (
	"bytes"
	"testing"
)

func TestTextLoggerWritesStructuredFields(t *testing.T) {
	var output bytes.Buffer
	NewText(&output).Debug("retrying request", "attempt", 2, "status", 503)
	if got, want := output.String(), "debug: retrying request attempt=2 status=503\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNopLogger(t *testing.T) {
	Nop().Debug("ignored", "secret", "must not be rendered")
}
