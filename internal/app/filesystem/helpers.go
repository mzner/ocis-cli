package filesystem

import (
	"context"
	"errors"
	"net/http"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

func output(options Options, kind string, value any, format string, args ...any) error {
	return (appoutput.Renderer{Writer: options.Out, Mode: options.OutputMode, Type: kind}).Write(value, format, args...)
}
func writeOutput(options Options, kind string, value any) error {
	return output(options, kind, value, "")
}
func protocolStatus(err error) int {
	var statusErr interface{ HTTPStatusCode() int }
	if errors.As(err, &statusErr) {
		return statusErr.HTTPStatusCode()
	}
	return http.StatusOK
}

func classifyProtocolError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return apperror.Wrap(apperror.KindCanceled, operation, err)
	}
	switch protocolStatus(err) {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return apperror.Wrap(apperror.KindUsage, operation, err)
	case http.StatusUnauthorized, http.StatusForbidden:
		return apperror.Wrap(apperror.KindAuthentication, operation, err)
	case http.StatusNotFound:
		return apperror.Wrap(apperror.KindNotFound, operation, err)
	case http.StatusConflict, http.StatusPreconditionFailed:
		return apperror.Wrap(apperror.KindConflict, operation, err)
	default:
		return err
	}
}
