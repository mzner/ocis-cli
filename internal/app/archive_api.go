package app

import "context"

// ArchiveDownloadRequest describes one server-side archive download.
type ArchiveDownloadRequest struct {
	Paths       []string
	Destination string
	Format      string
	Overwrite   bool
	DryRun      bool
}

// RunArchiveDownloadWithOptions creates and downloads a server-side archive.
func RunArchiveDownloadWithOptions(
	ctx context.Context,
	request ArchiveDownloadRequest,
	selectedProfile string,
	options RunOptions,
) error {
	return classifyProtocolError(
		"archive download",
		runArchiveDownload(
			ctx, request, selectedProfile, options.normalized(),
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
	return classifyProtocolError(
		"archive formats",
		runArchiveFormats(ctx, selectedProfile, options.normalized()),
	)
}
