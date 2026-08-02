package app

import (
	"fmt"
	"net/http"
	"os"

	"github.com/mzner/ocis-cli/internal/apperror"
	"github.com/mzner/ocis-cli/internal/webdav"
)

func touchFilesystem(
	client *client, request FilesystemRequest, options RunOptions,
) error {
	target := cleanRemote(request.Source)
	meta, err := client.stat(target)
	switch {
	case err == nil:
		return existingTouchResult(options, target, meta)
	case webdav.StatusCode(err) != http.StatusNotFound:
		return err
	}

	temporary, err := createEmptyTemporaryFile()
	if err != nil {
		return fmt.Errorf("create temporary file for touch: %w", err)
	}
	defer func() { _ = os.Remove(temporary) }()

	err = client.davClient().UploadWithOptions(
		client.context(), temporary, target,
		webdav.TransferOptions{NoClobber: true},
	)
	if err != nil {
		status := webdav.StatusCode(err)
		if status != http.StatusConflict && status != http.StatusPreconditionFailed {
			return err
		}
		meta, statErr := client.stat(target)
		if statErr != nil {
			return err
		}
		return existingTouchResult(options, target, meta)
	}

	meta, err = client.stat(target)
	if err != nil {
		return fmt.Errorf("verify touched file %s: %w", target, err)
	}
	if meta.Type != "file" || meta.Size != 0 {
		return apperror.Wrap(
			apperror.KindConflict, "touch",
			fmt.Errorf("%s changed while it was being created", target),
		)
	}
	return touchResult(options, target, true)
}

func createEmptyTemporaryFile() (string, error) {
	file, err := os.CreateTemp("", "ocis-cli-touch-*")
	if err != nil {
		return "", err
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

func existingTouchResult(options RunOptions, target string, meta item) error {
	if meta.Type != "file" {
		return apperror.Wrap(
			apperror.KindConflict, "touch",
			fmt.Errorf("%s is a directory", target),
		)
	}
	return touchResult(options, target, false)
}

func touchResult(options RunOptions, target string, created bool) error {
	value := map[string]any{
		"path": target, "created": created, "unchanged": !created,
	}
	if created {
		return output(options, "resource", value, "Created %s\n", target)
	}
	return output(options, "resource", value, "Unchanged %s\n", target)
}
