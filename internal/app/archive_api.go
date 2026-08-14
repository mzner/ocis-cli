package app

import (
	"context"

	archiveapp "github.com/mzner/ocis-cli/internal/app/archive"
	archiveclient "github.com/mzner/ocis-cli/internal/archiver"
	"github.com/mzner/ocis-cli/internal/sharing"
	"github.com/mzner/ocis-cli/internal/webdav"
)

// ArchiveDownloadRequest describes one server-side archive download.
type ArchiveDownloadRequest = archiveapp.Request

// ArchiveFormat reports one usable server archive format.
type ArchiveFormat = archiveapp.ArchiveFormat

// ArchiveResource is one selected archive root.
type ArchiveResource = archiveapp.ArchiveResource

// ArchiveResult describes archive preflight and completed download state.
type ArchiveResult = archiveapp.ArchiveResult

// RunArchiveDownloadWithOptions creates and downloads a server-side archive.
func RunArchiveDownloadWithOptions(
	ctx context.Context,
	request ArchiveDownloadRequest,
	selectedProfile string,
	options RunOptions,
) error {
	options = options.normalized()
	return classifyProtocolError(
		"archive download",
		archiveapp.RunDownload(
			ctx, request, selectedProfile, archiveOptions(options),
		),
	)
}

// RunArchiveFormatsWithOptions lists formats advertised by the selected
// server's preferred enabled archive service.
func RunArchiveFormatsWithOptions(
	ctx context.Context,
	selectedProfile string,
	options RunOptions,
) error {
	options = options.normalized()
	return classifyProtocolError(
		"archive formats",
		archiveapp.RunFormats(ctx, selectedProfile, archiveOptions(options)),
	)
}

type archiveClientAdapter struct{ client *client }

func (adapter archiveClientAdapter) SelectSpace(identifier string) error {
	return adapter.client.selectSpace(identifier)
}

func (adapter archiveClientAdapter) Capabilities(
	ctx context.Context,
) (sharing.Capabilities, error) {
	return adapter.client.sharingClient().Capabilities(ctx)
}

func (adapter archiveClientAdapter) Stat(path string) (webdav.Item, error) {
	return adapter.client.stat(path)
}

func (adapter archiveClientAdapter) List(path string) ([]webdav.Item, error) {
	return adapter.client.list(path)
}

func (adapter archiveClientAdapter) Archiver(
	endpoint string,
) (*archiveclient.Client, error) {
	return adapter.client.archiverClient(endpoint)
}

func archiveOptions(options RunOptions) archiveapp.Options {
	return archiveapp.Options{
		OutputMode: options.OutputMode, Out: options.Out, Err: options.Err,
		Quiet: options.Quiet, Space: options.Space,
		NewClient: func(
			ctx context.Context, selectedProfile string,
		) (archiveapp.Client, error) {
			selected, err := newClientWithOptions(ctx, selectedProfile, options)
			if err != nil {
				return nil, err
			}
			return archiveClientAdapter{client: selected}, nil
		},
	}
}
