package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
	"github.com/mzner/ocis-cli/internal/transfer"
	"github.com/mzner/ocis-cli/internal/versions"
)

type versionContext struct {
	client     *client
	resourceID string
	remotePath string
	available  []versions.Version
}

func runVersion(
	ctx context.Context,
	request VersionRequest,
	selectedProfile string,
	options RunOptions,
) error {
	options.Logger.Debug("run version operation", "operation", request.Operation)
	if request.Operation == VersionRestore && !request.Confirmed {
		return apperror.Wrap(
			apperror.KindUsage, "version restore",
			fmt.Errorf("restoring a version requires explicit confirmation"),
		)
	}
	versionCtx, err := loadVersionContext(
		ctx, request.Path, selectedProfile, options,
	)
	if err != nil {
		return err
	}
	switch request.Operation {
	case VersionList:
		return listVersions(versionCtx, options)
	case VersionInfo:
		selected, err := resolveVersion(versionCtx.available, request.VersionID)
		if err != nil {
			return err
		}
		return writeVersionInfo(versionCtx, selected, options)
	case VersionDownload:
		selected, err := resolveVersion(versionCtx.available, request.VersionID)
		if err != nil {
			return err
		}
		return downloadVersion(ctx, versionCtx, selected, request, options)
	case VersionRestore:
		selected, err := resolveVersion(versionCtx.available, request.VersionID)
		if err != nil {
			return err
		}
		return restoreVersion(ctx, versionCtx, selected, request, options)
	default:
		return apperror.Wrap(
			apperror.KindUsage, "version",
			fmt.Errorf("unknown version command %q", request.Operation),
		)
	}
}

func (client *client) versionsClient() *versions.Client {
	if client.versions == nil {
		client.versions = versions.NewClient(client.apiConfig(), client.http)
	}
	return client.versions
}

func loadVersionContext(
	ctx context.Context,
	remotePath string,
	selectedProfile string,
	options RunOptions,
) (versionContext, error) {
	remotePath = cleanRemote(remotePath)
	client, err := newClientWithOptions(ctx, selectedProfile, options)
	if err != nil {
		return versionContext{}, err
	}
	if err := client.selectSpace(options.Space); err != nil {
		return versionContext{}, err
	}
	metadata, err := client.stat(remotePath)
	if err != nil {
		return versionContext{}, err
	}
	if metadata.Type != "file" {
		return versionContext{}, apperror.Wrap(
			apperror.KindUsage, "version",
			fmt.Errorf("%s is a directory; versions are available for files only", remotePath),
		)
	}
	if metadata.ResourceID == "" {
		return versionContext{}, fmt.Errorf(
			"server did not return a stable resource ID for %s", remotePath,
		)
	}
	available, err := client.versionsClient().List(ctx, metadata.ResourceID)
	if err != nil {
		return versionContext{}, err
	}
	return versionContext{
		client: client, resourceID: metadata.ResourceID,
		remotePath: remotePath, available: available,
	}, nil
}

func listVersions(ctx versionContext, options RunOptions) error {
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "version", ctx.available)
	}
	for _, version := range ctx.available {
		if _, err := fmt.Fprintf(
			options.Out, "%10d  %-31s  %-24s  %s\n",
			version.Size, version.Modified, version.ID, version.ETag,
		); err != nil {
			return err
		}
	}
	return nil
}

func writeVersionInfo(
	ctx versionContext, selected versions.Version, options RunOptions,
) error {
	value := map[string]any{
		"path": ctx.remotePath, "resourceId": ctx.resourceID,
		"version": selected,
	}
	return output(
		options, "version", value,
		"%s\n  Version ID: %s\n  Size: %d\n  Modified: %s\n  ETag: %s\n",
		ctx.remotePath, selected.ID, selected.Size,
		selected.Modified, selected.ETag,
	)
}

func downloadVersion(
	ctx context.Context,
	versionCtx versionContext,
	selected versions.Version,
	request VersionRequest,
	options RunOptions,
) error {
	destination, err := versionDestination(
		versionCtx.remotePath, request.Destination,
	)
	if err != nil {
		return err
	}
	if request.DryRun {
		return output(
			options, "version",
			map[string]any{
				"operation": "download", "path": versionCtx.remotePath,
				"version": selected, "destination": destination, "dryRun": true,
			},
			"Would download version %s of %s to %s\n",
			selected.ID, versionCtx.remotePath, destination,
		)
	}
	if destination != "-" && request.NoClobber {
		if _, err := os.Stat(destination); err == nil {
			return apperror.Wrap(
				apperror.KindConflict, "version download",
				fmt.Errorf("destination exists: %s", destination),
			)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	content, err := versionCtx.client.versionsClient().Open(
		ctx, versionCtx.resourceID, selected.ID,
	)
	if err != nil {
		return err
	}
	defer func() { _ = content.Body.Close() }()
	if destination == "-" {
		return copyVersionContent(options.Out, content, selected, request.Verify)
	}
	temporary, err := os.CreateTemp(
		filepath.Dir(destination), "."+filepath.Base(destination)+".version-*",
	)
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	copied, copyErr := io.Copy(temporary, content.Body)
	closeErr := temporary.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := verifyVersionContent(copied, content, selected, request.Verify); err != nil {
		return err
	}
	if err := transfer.ReplaceFile(temporaryPath, destination); err != nil {
		return err
	}
	return output(
		options, "version",
		map[string]any{
			"path": versionCtx.remotePath, "version": selected,
			"downloaded": destination, "verified": request.Verify,
		},
		"Downloaded version %s of %s to %s\n",
		selected.ID, versionCtx.remotePath, destination,
	)
}

func restoreVersion(
	ctx context.Context,
	versionCtx versionContext,
	selected versions.Version,
	request VersionRequest,
	options RunOptions,
) error {
	if request.DryRun {
		return output(
			options, "version",
			map[string]any{
				"operation": "restore", "path": versionCtx.remotePath,
				"version": selected, "dryRun": true,
			},
			"Would restore version %s of %s\n",
			selected.ID, versionCtx.remotePath,
		)
	}
	if err := versionCtx.client.versionsClient().Restore(
		ctx, versionCtx.resourceID, selected.ID,
	); err != nil {
		return err
	}
	return output(
		options, "version",
		map[string]any{
			"path": versionCtx.remotePath, "restored": selected.ID,
		},
		"Restored version %s of %s\n", selected.ID, versionCtx.remotePath,
	)
}

func resolveVersion(
	available []versions.Version, versionID string,
) (versions.Version, error) {
	versionID = strings.TrimSpace(versionID)
	if versionID == "" {
		return versions.Version{}, apperror.Wrap(
			apperror.KindUsage, "version",
			fmt.Errorf("version ID must not be empty"),
		)
	}
	for _, candidate := range available {
		if candidate.ID == versionID {
			return candidate, nil
		}
	}
	return versions.Version{}, apperror.Wrap(
		apperror.KindNotFound, "version",
		fmt.Errorf("unknown version %q; run ocis version list PATH", versionID),
	)
}

func versionDestination(remotePath string, destination string) (string, error) {
	if destination == "" {
		return "", apperror.Wrap(
			apperror.KindUsage, "version download",
			fmt.Errorf("local destination must not be empty"),
		)
	}
	if destination == "-" {
		return destination, nil
	}
	if info, err := os.Stat(destination); err == nil && info.IsDir() {
		return filepath.Join(destination, path.Base(remotePath)), nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return destination, nil
}

func copyVersionContent(
	destination io.Writer,
	content versions.Content,
	selected versions.Version,
	verify bool,
) error {
	copied, err := io.Copy(destination, content.Body)
	if err != nil {
		return err
	}
	return verifyVersionContent(copied, content, selected, verify)
}

func verifyVersionContent(
	copied int64,
	content versions.Content,
	selected versions.Version,
	verify bool,
) error {
	if !verify {
		return nil
	}
	if copied != selected.Size {
		return fmt.Errorf(
			"verify version download: size mismatch: expected %d bytes, received %d bytes",
			selected.Size, copied,
		)
	}
	if content.Size >= 0 && content.Size != copied {
		return fmt.Errorf(
			"verify version download: response size mismatch: expected %d bytes, received %d bytes",
			content.Size, copied,
		)
	}
	if content.ETag != "" && selected.ETag != "" &&
		content.ETag != selected.ETag {
		return fmt.Errorf(
			"verify version download: ETag changed from %s to %s",
			selected.ETag, content.ETag,
		)
	}
	return nil
}
