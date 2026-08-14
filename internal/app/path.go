package app

import (
	"path"
	"strings"
)

// cleanRemote normalizes a user-facing remote path at the application facade.
func cleanRemote(remote string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" || remote == "/" {
		return "/"
	}
	return path.Clean("/" + strings.Trim(remote, "/"))
}

func fallback(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
