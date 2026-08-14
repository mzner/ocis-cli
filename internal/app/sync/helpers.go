package sync

import (
	"path"
	"strings"

	appoutput "github.com/mzner/ocis-cli/internal/output"
)

func output(options Options, kind string, value any, format string, args ...any) error {
	return (appoutput.Renderer{Writer: options.Out, Mode: options.OutputMode, Type: kind}).Write(value, format, args...)
}
func writeOutput(options Options, kind string, value any) error {
	return output(options, kind, value, "")
}
func cleanRemote(remote string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" || remote == "/" {
		return "/"
	}
	return path.Clean("/" + strings.Trim(remote, "/"))
}
