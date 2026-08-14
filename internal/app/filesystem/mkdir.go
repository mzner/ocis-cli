package filesystem

import (
	"fmt"
	"path"
	"strings"

	"github.com/mzner/ocis-cli/internal/apperror"
	"github.com/mzner/ocis-cli/internal/webdav"
)

func mkdirFilesystem(
	client Client, request Request, options Options,
) error {
	target := cleanRemote(request.Source)
	if !request.Parents {
		if err := client.EnsureCollection(target); err != nil {
			return err
		}
		return output(
			options, "resource", map[string]any{"created": target},
			"Created %s\n", target,
		)
	}
	created, err := ensureDirectoryPath(client, target)
	if err != nil {
		return err
	}
	value := map[string]any{
		"created": target, "parents": true,
		"createdDirectories": created,
	}
	if len(created) == 0 {
		return output(
			options, "resource", value, "Directory exists %s\n", target,
		)
	}
	return output(
		options, "resource", value, "Created %s\n", target,
	)
}

func ensureDirectoryPath(client Client, target string) ([]string, error) {
	if target == "/" {
		return nil, nil
	}
	parts := strings.Split(strings.TrimPrefix(target, "/"), "/")
	created := make([]string, 0, len(parts))
	current := "/"
	for _, part := range parts {
		current = path.Join(current, part)
		meta, err := client.Stat(current)
		switch {
		case err == nil && meta.Type == "directory":
			continue
		case err == nil:
			return nil, apperror.Wrap(
				apperror.KindConflict, "mkdir",
				fmt.Errorf("path component %s is a file", current),
			)
		case webdav.StatusCode(err) != 404:
			return nil, err
		}
		if err := client.EnsureCollection(current); err != nil {
			return nil, err
		}
		meta, err = client.Stat(current)
		if err != nil {
			return nil, fmt.Errorf("verify created directory %s: %w", current, err)
		}
		if meta.Type != "directory" {
			return nil, apperror.Wrap(
				apperror.KindConflict, "mkdir",
				fmt.Errorf("path component %s is a file", current),
			)
		}
		created = append(created, current)
	}
	return created, nil
}
