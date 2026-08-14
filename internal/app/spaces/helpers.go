package spaces

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/mzner/ocis-cli/internal/apperror"
	"github.com/mzner/ocis-cli/internal/graph"
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
func Resolve(spaces []graph.Drive, identifier string) (graph.Drive, error) {
	var matches []graph.Drive
	for _, v := range spaces {
		if v.ID == identifier || strings.EqualFold(v.Name, identifier) || strings.EqualFold(v.DriveAlias, identifier) {
			matches = append(matches, v)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return graph.Drive{}, apperror.Wrap(apperror.KindUsage, "space", fmt.Errorf("unknown space %q; run ocis space list", identifier))
	}
	return graph.Drive{}, apperror.Wrap(apperror.KindUsage, "space", fmt.Errorf("space name %q is ambiguous; use its ID", identifier))
}
