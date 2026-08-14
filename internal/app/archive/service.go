// Package archive contains the archive-download application domain. It
// depends on a narrow authenticated client port instead of the parent app
// package, so archive policy cannot reach unrelated application helpers.
package archive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mzner/ocis-cli/internal/apperror"
	archiveclient "github.com/mzner/ocis-cli/internal/archiver"
	appoutput "github.com/mzner/ocis-cli/internal/output"
	"github.com/mzner/ocis-cli/internal/sharing"
	"github.com/mzner/ocis-cli/internal/transfer"
	"github.com/mzner/ocis-cli/internal/webdav"
	"golang.org/x/term"
)

// Request describes one server-side archive download.
type Request struct {
	Paths       []string
	Destination string
	Format      string
	Overwrite   bool
	DryRun      bool
}

// Client is the authenticated server functionality used by this domain.
type Client interface {
	SelectSpace(string) error
	Capabilities(context.Context) (sharing.Capabilities, error)
	Stat(string) (webdav.Item, error)
	List(string) ([]webdav.Item, error)
	Archiver(string) (*archiveclient.Client, error)
}

// ClientFactory creates an account-bound client without exposing application
// runtime internals to this domain.
type ClientFactory func(context.Context, string) (Client, error)

// Options contains the process-boundary values used by archive operations.
type Options struct {
	OutputMode appoutput.Mode
	Out        io.Writer
	Err        io.Writer
	Quiet      bool
	Space      string
	NewClient  ClientFactory
}

// ArchiveFormat reports one usable format and the limits shared by the
// selected archive service.
type ArchiveFormat struct {
	Format      string `json:"format"`
	Version     string `json:"version"`
	MaxNumFiles int64  `json:"maxNumFiles,omitempty"`
	MaxSize     int64  `json:"maxSize,omitempty"`
}

// ArchiveResource is one selected archive root.
type ArchiveResource struct {
	Path       string `json:"path"`
	ResourceID string `json:"resourceId"`
	Type       string `json:"type"`
}

// ArchiveResult describes archive preflight and completed download state.
type ArchiveResult struct {
	Resources    []ArchiveResource `json:"resources"`
	Destination  string            `json:"destination"`
	Format       string            `json:"format"`
	Entries      int64             `json:"entries"`
	Files        int64             `json:"files"`
	Directories  int64             `json:"directories"`
	LogicalBytes int64             `json:"logicalBytes"`
	ArchiveBytes int64             `json:"archiveBytes,omitempty"`
	DryRun       bool              `json:"dryRun,omitempty"`
}

// RunFormats lists the preferred enabled archive service's formats.
func RunFormats(
	ctx context.Context, selectedProfile string, options Options,
) error {
	client, err := options.NewClient(ctx, selectedProfile)
	if err != nil {
		return err
	}
	capability, err := discoverArchiver(ctx, client)
	if err != nil {
		return err
	}
	formats := archiveFormats(capability)
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "archive-format", formats)
	}
	writer := tabwriter.NewWriter(options.Out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(
		writer, "FORMAT\tVERSION\tMAX ENTRIES\tMAX SOURCE BYTES",
	); err != nil {
		return err
	}
	for _, value := range formats {
		if _, err := fmt.Fprintf(
			writer, "%s\t%s\t%s\t%s\n", value.Format, value.Version,
			limitText(value.MaxNumFiles), limitText(value.MaxSize),
		); err != nil {
			return err
		}
	}
	return writer.Flush()
}

// RunDownload preflights and downloads one server-created archive.
func RunDownload(
	ctx context.Context,
	request Request,
	selectedProfile string,
	options Options,
) error {
	if len(request.Paths) == 0 {
		return archiveUsage("select at least one remote path")
	}
	if strings.TrimSpace(request.Destination) == "" {
		return archiveUsage("--output is required")
	}
	if request.Destination == "-" {
		return archiveUsage("archive output must be a local file, not stdout")
	}
	paths, err := normalizeArchivePaths(request.Paths)
	if err != nil {
		return archiveUsage(err.Error())
	}
	format, err := resolveArchiveFormat(request.Format, request.Destination)
	if err != nil {
		return archiveUsage(err.Error())
	}
	if err := validateArchiveDestination(
		request.Destination, request.Overwrite,
	); err != nil {
		return err
	}

	client, err := options.NewClient(ctx, selectedProfile)
	if err != nil {
		return err
	}
	if err := client.SelectSpace(options.Space); err != nil {
		return err
	}
	capability, err := discoverArchiver(ctx, client)
	if err != nil {
		return err
	}
	if !containsArchiveFormat(capability.Formats, format) {
		return archiveUsage(fmt.Sprintf(
			"server archive service does not support %s; available formats: %s",
			format, strings.Join(normalizeFormats(capability.Formats), ", "),
		))
	}
	result := ArchiveResult{
		Destination: request.Destination, Format: format,
		Resources: make([]ArchiveResource, 0, len(paths)),
	}
	for _, remote := range paths {
		if err := addArchiveResource(client, remote, capability, &result); err != nil {
			return err
		}
	}
	if request.DryRun {
		result.DryRun = true
		return writeArchiveResult(result, options)
	}

	protocol, err := client.Archiver(capability.URL)
	if err != nil {
		return fmt.Errorf("configure archive download: %w", err)
	}
	file, err := os.CreateTemp(
		filepath.Dir(request.Destination), ".ocis-archive-*.part",
	)
	if err != nil {
		return fmt.Errorf("create archive temporary file: %w", err)
	}
	temporary := file.Name()
	committed := false
	defer func() {
		_ = file.Close()
		if !committed {
			_ = os.Remove(temporary)
		}
	}()
	progress, finishProgress := archiveProgressReporter(
		options, request.Destination,
	)
	downloaded, err := protocol.Download(
		ctx, archiveclient.DownloadRequest{
			ResourceIDs: archiveResourceIDs(result.Resources), Format: format,
		}, file, progress,
	)
	finishProgress(downloaded.Bytes)
	if err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync archive temporary file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close archive temporary file: %w", err)
	}
	if err := archiveclient.ValidateFile(
		temporary, format, archiveclient.ValidationLimits{
			MaxEntries: capability.MaxNumFiles,
			MaxBytes:   capability.MaxSize,
		},
	); err != nil {
		return err
	}
	if err := transfer.CommitFile(
		temporary, request.Destination, request.Overwrite,
	); err != nil {
		if errors.Is(err, transfer.ErrDestinationExists) {
			return apperror.Wrap(apperror.KindConflict, "archive download", err)
		}
		return err
	}
	committed = true
	result.ArchiveBytes = downloaded.Bytes
	return writeArchiveResult(result, options)
}

func discoverArchiver(
	ctx context.Context, client Client,
) (sharing.ArchiverCapabilities, error) {
	capabilities, err := client.Capabilities(ctx)
	if err != nil {
		return sharing.ArchiverCapabilities{}, fmt.Errorf(
			"discover archive service: %w", err,
		)
	}
	return SelectCapabilities(capabilities.Files.Archivers)
}

// SelectCapabilities chooses the highest enabled usable archive capability.
func SelectCapabilities(
	capabilities []sharing.ArchiverCapabilities,
) (sharing.ArchiverCapabilities, error) {
	var selected *sharing.ArchiverCapabilities
	for index := range capabilities {
		value := &capabilities[index]
		if !value.Enabled || strings.TrimSpace(value.Version) == "" ||
			strings.TrimSpace(value.URL) == "" || len(normalizeFormats(value.Formats)) == 0 {
			continue
		}
		if selected == nil || compareArchiveVersions(value.Version, selected.Version) > 0 {
			selected = value
		}
	}
	if selected == nil {
		return sharing.ArchiverCapabilities{}, errors.New(
			"server does not advertise an enabled archive service",
		)
	}
	result := *selected
	result.Formats = normalizeFormats(result.Formats)
	return result, nil
}

func addArchiveResource(
	client Client,
	remote string,
	capability sharing.ArchiverCapabilities,
	result *ArchiveResult,
) error {
	root, err := client.Stat(remote)
	if err != nil {
		return err
	}
	if root.ResourceID == "" {
		return fmt.Errorf(
			"server did not return a stable resource ID for %s", remote,
		)
	}
	for _, existing := range result.Resources {
		if existing.ResourceID == root.ResourceID {
			return archiveUsage(fmt.Sprintf(
				"%s resolves to the same resource as %s",
				remote, existing.Path,
			))
		}
	}
	result.Resources = append(result.Resources, ArchiveResource{
		Path: remote, ResourceID: root.ResourceID, Type: root.Type,
	})
	includeRoot := remote != "/"
	return scanArchiveItem(client, root, includeRoot, capability, result)
}

func scanArchiveItem(
	client Client,
	value webdav.Item,
	include bool,
	capability sharing.ArchiverCapabilities,
	result *ArchiveResult,
) error {
	if include {
		result.Entries++
		if value.Type == "directory" {
			result.Directories++
		} else {
			result.Files++
			if value.Size > 0 && result.LogicalBytes > math.MaxInt64-value.Size {
				return archiveUsage("selected source size exceeds the supported integer range")
			}
			result.LogicalBytes += value.Size
		}
		if capability.MaxNumFiles > 0 && result.Entries > capability.MaxNumFiles {
			return archiveUsage(fmt.Sprintf(
				"selection contains more than the server limit of %d archive entries",
				capability.MaxNumFiles,
			))
		}
		if capability.MaxSize > 0 && result.LogicalBytes > capability.MaxSize {
			return archiveUsage(fmt.Sprintf(
				"selection exceeds the server limit of %d source bytes",
				capability.MaxSize,
			))
		}
	}
	if value.Type != "directory" {
		return nil
	}
	children, err := client.List(value.Path)
	if err != nil {
		return err
	}
	for _, child := range children {
		if err := scanArchiveItem(
			client, child, true, capability, result,
		); err != nil {
			return err
		}
	}
	return nil
}

func normalizeArchivePaths(values []string) ([]string, error) {
	result := make([]string, len(values))
	for index, value := range values {
		if strings.TrimSpace(value) == "" {
			return nil, errors.New("remote archive path cannot be empty")
		}
		result[index] = cleanRemote(value)
	}
	for left := range result {
		for right := left + 1; right < len(result); right++ {
			if result[left] == result[right] {
				return nil, fmt.Errorf("remote path %s is selected more than once", result[left])
			}
			if archivePathContains(result[left], result[right]) ||
				archivePathContains(result[right], result[left]) {
				return nil, fmt.Errorf(
					"nested selections %s and %s would duplicate archive entries",
					result[left], result[right],
				)
			}
		}
	}
	return result, nil
}

func archivePathContains(parent, child string) bool {
	if parent == "/" {
		return child != "/"
	}
	return strings.HasPrefix(child, parent+"/")
}

func resolveArchiveFormat(requested, destination string) (string, error) {
	format := strings.ToLower(strings.TrimSpace(requested))
	extension := strings.ToLower(filepath.Ext(destination))
	if format == "" {
		if extension == ".tar" {
			return "tar", nil
		}
		return "zip", nil
	}
	if format != "zip" && format != "tar" {
		return "", fmt.Errorf("--format must be zip or tar, got %q", requested)
	}
	if (extension == ".zip" || extension == ".tar") &&
		extension != "."+format {
		return "", fmt.Errorf(
			"--format %s conflicts with destination extension %s",
			format, extension,
		)
	}
	return format, nil
}

func validateArchiveDestination(destination string, overwrite bool) error {
	info, err := os.Lstat(destination)
	switch {
	case err == nil && info.IsDir():
		return apperror.Wrap(
			apperror.KindConflict, "archive download",
			fmt.Errorf("destination is a directory: %s", destination),
		)
	case err == nil && !overwrite:
		return apperror.Wrap(
			apperror.KindConflict, "archive download",
			fmt.Errorf("destination already exists: %s; pass --overwrite to replace it", destination),
		)
	case err == nil:
		return nil
	case errors.Is(err, os.ErrNotExist):
		return nil
	default:
		return fmt.Errorf("inspect archive destination: %w", err)
	}
}

func archiveFormats(capability sharing.ArchiverCapabilities) []ArchiveFormat {
	formats := make([]ArchiveFormat, 0, len(capability.Formats))
	for _, format := range normalizeFormats(capability.Formats) {
		formats = append(formats, ArchiveFormat{
			Format: format, Version: capability.Version,
			MaxNumFiles: capability.MaxNumFiles, MaxSize: capability.MaxSize,
		})
	}
	return formats
}

func normalizeFormats(values []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if (value == "zip" || value == "tar") && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func containsArchiveFormat(values []string, selected string) bool {
	for _, value := range normalizeFormats(values) {
		if value == selected {
			return true
		}
	}
	return false
}

func compareArchiveVersions(left, right string) int {
	leftParts := strings.Split(strings.TrimPrefix(strings.ToLower(left), "v"), ".")
	rightParts := strings.Split(strings.TrimPrefix(strings.ToLower(right), "v"), ".")
	for index := 0; index < max(len(leftParts), len(rightParts)); index++ {
		leftNumber, rightNumber := 0, 0
		if index < len(leftParts) {
			leftNumber, _ = strconv.Atoi(strings.SplitN(leftParts[index], "-", 2)[0])
		}
		if index < len(rightParts) {
			rightNumber, _ = strconv.Atoi(strings.SplitN(rightParts[index], "-", 2)[0])
		}
		if leftNumber < rightNumber {
			return -1
		}
		if leftNumber > rightNumber {
			return 1
		}
	}
	return strings.Compare(left, right)
}

func archiveResourceIDs(values []ArchiveResource) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.ResourceID
	}
	return result
}

func archiveProgressReporter(
	options Options, destination string,
) (func(int64), func(int64)) {
	if options.Quiet || options.OutputMode != appoutput.Human {
		return nil, func(int64) {}
	}
	terminalOutput := false
	if descriptor, ok := options.Err.(interface{ Fd() uintptr }); ok {
		terminalOutput = term.IsTerminal(int(descriptor.Fd()))
	}
	lastUpdate := time.Time{}
	lastBytes := int64(-1)
	update := func(written int64) {
		now := time.Now()
		if written == lastBytes || now.Sub(lastUpdate) < 200*time.Millisecond {
			return
		}
		lastUpdate, lastBytes = now, written
		if terminalOutput {
			_, _ = fmt.Fprintf(
				options.Err, "\rArchive download: %d bytes written to %s",
				written, destination,
			)
		}
	}
	finish := func(written int64) {
		if terminalOutput {
			_, _ = fmt.Fprintf(
				options.Err, "\rArchive download: %d bytes written to %s\n",
				written, destination,
			)
		}
	}
	return update, finish
}

// CapabilityDetail describes the preferred capability for diagnostics.
func CapabilityDetail(capabilities sharing.Capabilities) string {
	selected, err := SelectCapabilities(capabilities.Files.Archivers)
	if err != nil {
		return "not advertised"
	}
	details := []string{
		"version " + selected.Version,
		"formats " + strings.Join(selected.Formats, ", "),
	}
	if selected.MaxNumFiles > 0 {
		details = append(details, fmt.Sprintf(
			"maximum %d entries", selected.MaxNumFiles,
		))
	}
	if selected.MaxSize > 0 {
		details = append(details, fmt.Sprintf(
			"maximum %d source bytes", selected.MaxSize,
		))
	}
	return strings.Join(details, "; ")
}

func writeArchiveResult(result ArchiveResult, options Options) error {
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "archive", result)
	}
	if result.DryRun {
		_, err := fmt.Fprintf(
			options.Out,
			"Would archive %d entries (%d source bytes) from %d selection(s) to %s as %s\n",
			result.Entries, result.LogicalBytes, len(result.Resources),
			result.Destination, result.Format,
		)
		return err
	}
	_, err := fmt.Fprintf(
		options.Out,
		"Downloaded %d entries (%d source bytes) to %s as %s (%d archive bytes)\n",
		result.Entries, result.LogicalBytes, result.Destination,
		result.Format, result.ArchiveBytes,
	)
	return err
}

func writeOutput(options Options, kind string, value any) error {
	return (appoutput.Renderer{
		Writer: options.Out, Mode: options.OutputMode, Type: kind,
	}).Write(value, "")
}

func cleanRemote(remote string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" || remote == "/" {
		return "/"
	}
	return "/" + strings.Trim(remote, "/")
}

func archiveUsage(message string) error {
	return apperror.Wrap(
		apperror.KindUsage, "archive download", errors.New(message),
	)
}

func limitText(value int64) string {
	if value <= 0 {
		return "not advertised"
	}
	return strconv.FormatInt(value, 10)
}
