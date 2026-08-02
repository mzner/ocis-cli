package apperror

import (
	"context"
	"errors"
	"testing"
)

func TestExitCode(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{nil, 0},
		{errors.New("failure"), 1},
		{Wrap(KindUsage, "parse", errors.New("bad input")), 2},
		{Wrap(KindAuthentication, "login", errors.New("denied")), 3},
		{Wrap(KindNotFound, "stat", errors.New("missing")), 4},
		{Wrap(KindConflict, "upload", errors.New("exists")), 5},
		{Wrap(KindCanceled, "download", context.Canceled), 130},
	}
	for _, test := range tests {
		if got := ExitCode(test.err); got != test.want {
			t.Errorf("ExitCode(%v): got %d, want %d", test.err, got, test.want)
		}
	}
}

func TestWrapPreservesCause(t *testing.T) {
	cause := errors.New("cause")
	err := Wrap(KindNotFound, "lookup", cause)
	if !errors.Is(err, cause) || !IsKind(err, KindNotFound) {
		t.Fatalf("classification or cause was lost: %v", err)
	}
}

func TestDetails(t *testing.T) {
	kind, operation := Details(Wrap(KindConflict, "upload", errors.New("exists")))
	if kind != KindConflict || operation != "upload" {
		t.Fatalf("got kind %q and operation %q", kind, operation)
	}

	kind, operation = Details(errors.New("plain error"))
	if kind != KindGeneral || operation != "" {
		t.Fatalf("plain error: got kind %q and operation %q", kind, operation)
	}
}
