// Package apperror defines stable error categories shared by application
// services and the executable boundary.
package apperror

import (
	"errors"
	"fmt"
)

// Kind identifies an error category that maps to a stable process exit code.
type Kind string

const (
	KindGeneral        Kind = "general"
	KindUsage          Kind = "usage"
	KindAuthentication Kind = "authentication"
	KindNotFound       Kind = "not_found"
	KindConflict       Kind = "conflict"
	KindCanceled       Kind = "canceled"
)

// Error adds stable classification and operation context to an error.
type Error struct {
	Kind Kind
	Op   string
	Err  error
}

func (err *Error) Error() string {
	if err.Op == "" {
		return err.Err.Error()
	}
	return fmt.Sprintf("%s: %v", err.Op, err.Err)
}

// Unwrap returns the underlying error.
func (err *Error) Unwrap() error {
	return err.Err
}

// Wrap classifies err. A nil error remains nil.
func Wrap(kind Kind, operation string, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Kind: kind, Op: operation, Err: err}
}

// IsKind reports whether err has the requested classification.
func IsKind(err error, kind Kind) bool {
	var classified *Error
	return errors.As(err, &classified) && classified.Kind == kind
}

// Details returns the stable classification and operation for err.
func Details(err error) (Kind, string) {
	var classified *Error
	if errors.As(err, &classified) {
		return classified.Kind, classified.Op
	}
	return KindGeneral, ""
}

// ExitCode maps an application error to the public process exit-code contract.
func ExitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case IsKind(err, KindUsage):
		return 2
	case IsKind(err, KindAuthentication):
		return 3
	case IsKind(err, KindNotFound):
		return 4
	case IsKind(err, KindConflict):
		return 5
	case IsKind(err, KindCanceled):
		return 130
	default:
		return 1
	}
}
