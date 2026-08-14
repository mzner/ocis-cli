package filesystem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
	"github.com/mzner/ocis-cli/internal/transfer"
	"github.com/mzner/ocis-cli/internal/webdav"
	"golang.org/x/term"
)

func Run(
	ctx context.Context, request Request, selected string, options Options,
) error {
	options.Logger.Debug("run filesystem operation", "operation", request.Operation)
	client, err := options.NewClient(ctx, selected)
	if err != nil {
		return err
	}
	if err := client.SelectSpace(options.Space); err != nil {
		return err
	}
	return runFilesystemWithClient(ctx, client, request, options)
}

func runFilesystemWithClient(
	ctx context.Context, client Client,
	request Request, options Options,
) error {
	switch request.Operation {
	case List:
		remote := request.Source
		if remote == "" {
			remote = "/"
		}
		items, err := client.List(remote)
		if err != nil {
			return err
		}
		if options.OutputMode != appoutput.Human {
			return writeOutput(options, "item", items)
		}
		for _, item := range items {
			size := "-"
			if item.Type == "file" {
				size = strconv.FormatInt(item.Size, 10)
			}
			_, _ = fmt.Fprintf(
				options.Out, "%-10s %10s  %s\n", item.Type, size, item.Name,
			)
		}
		return nil
	case Stat:
		meta, err := client.Stat(request.Source)
		if err != nil {
			return addSpaceStatHint(ctx, client, request.Source, err)
		}
		if options.OutputMode != appoutput.Human {
			return writeOutput(options, "item", meta)
		}
		return writeHumanStat(options.Out, meta)
	case Cat:
		meta, err := client.Stat(request.Source)
		if err != nil {
			return err
		}
		if meta.Type == "directory" {
			return apperror.Wrap(
				apperror.KindUsage, "cat",
				fmt.Errorf("%s is a directory", cleanRemote(request.Source)),
			)
		}
		return client.Stream(request.Source, options.Out)
	case Tree:
		return treeFilesystem(client, request, options)
	case DU:
		return duFilesystem(client, request, options)
	case Upload:
		return uploadFilesystem(ctx, client, request, options)
	case Download:
		return downloadFilesystem(ctx, client, request, options)
	case Mkdir:
		return mkdirFilesystem(client, request, options)
	case Touch:
		return touchFilesystem(client, request, options)
	case Move, Copy:
		return copyOrMoveFilesystem(client, request, options)
	case Remove:
		if request.DryRun {
			return output(
				options, "resource",
				map[string]any{
					"operation": "remove", "path": cleanRemote(request.Source),
					"recursive": request.Recursive, "dryRun": true,
				},
				"Would delete %s\n", cleanRemote(request.Source),
			)
		}
		if err := client.Remove(request.Source, request.Recursive); err != nil {
			return err
		}
		return output(
			options, "resource",
			map[string]any{
				"deleted": cleanRemote(request.Source), "recursive": request.Recursive,
			},
			"Deleted %s\n", cleanRemote(request.Source),
		)
	default:
		return apperror.Wrap(
			apperror.KindUsage, "filesystem",
			fmt.Errorf("unknown filesystem command %q", request.Operation),
		)
	}
}

func writeHumanStat(writer io.Writer, meta webdav.Item) error {
	fields := [][2]string{
		{"Name", meta.Name},
		{"Path", meta.Path},
		{"Type", meta.Type},
		{"Size", strconv.FormatInt(meta.Size, 10) + " bytes"},
		{"Modified", meta.LastModified},
		{"ETag", meta.ETag},
		{"Resource ID", meta.ResourceID},
	}
	if meta.Favorite != nil {
		fields = append(fields, [2]string{
			"Favorite", strconv.FormatBool(*meta.Favorite),
		})
	}
	if len(meta.Tags) > 0 {
		fields = append(fields, [2]string{
			"Tags", strings.Join(meta.Tags, ", "),
		})
	}
	for _, field := range fields {
		if field[1] == "" {
			continue
		}
		if _, err := fmt.Fprintf(
			writer, "%-12s %s\n", field[0]+":", field[1],
		); err != nil {
			return err
		}
	}
	if len(meta.Checksums) > 0 {
		if _, err := fmt.Fprintln(writer, "Checksums:"); err != nil {
			return err
		}
		for _, checksum := range meta.Checksums {
			if _, err := fmt.Fprintf(
				writer, "  %-10s %s\n",
				checksum.Algorithm+":", checksum.Value,
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func addSpaceStatHint(
	ctx context.Context, client Client, remote string, statErr error,
) error {
	if webdav.StatusCode(statErr) != 404 {
		return statErr
	}
	spaces, err := client.ListMyDrives(ctx)
	if err != nil {
		return statErr
	}
	for _, candidate := range spaces {
		if candidate.ID != remote &&
			!strings.EqualFold(candidate.Name, remote) &&
			!strings.EqualFold(candidate.DriveAlias, remote) {
			continue
		}
		return fmt.Errorf(
			"resource %q was not found in the current file root; %q is also a Space: "+
				"use %q for Space metadata or %q for its root: %w",
			cleanRemote(remote), remote,
			"ocis space info "+shellQuote(remote),
			"ocis --space "+shellQuote(remote)+" stat /",
			statErr,
		)
	}
	return statErr
}

func shellQuote(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\n'\"\\") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func uploadFilesystem(
	ctx context.Context, client Client, request Request, options Options,
) error {
	if request.DryRun {
		return output(
			options, "transfer",
			map[string]any{
				"operation": "upload", "source": request.Source,
				"destination": cleanRemote(request.Destination), "dryRun": true,
			},
			"Would upload %s to %s\n",
			request.Source, cleanRemote(request.Destination),
		)
	}
	uploadSource := request.Source
	if request.Source == "-" {
		if request.Recursive {
			return apperror.Wrap(
				apperror.KindUsage, "upload",
				errors.New("--recursive cannot be used when uploading from stdin"),
			)
		}
		var cleanup func()
		var err error
		uploadSource, cleanup, err = spoolInput(options.In)
		if err != nil {
			return err
		}
		defer cleanup()
	}
	info, err := os.Stat(uploadSource)
	if err != nil {
		return err
	}
	if info.IsDir() && !request.Recursive {
		return apperror.Wrap(
			apperror.KindUsage, "upload",
			errors.New("local path is a directory; use --recursive"),
		)
	}
	uploadCapabilities := webdav.TUSCapabilities{}
	if !request.NoClobber {
		uploadCapabilities = discoverUploadCapabilities(ctx, client, options)
	}
	if err := transfer.UploadWithOptions(
		ctx, transferRemote(client, request, uploadCapabilities),
		uploadSource, request.Destination,
		transfer.Options{
			Concurrency: options.Concurrency, Progress: progressReporter(options),
		},
	); err != nil {
		return err
	}
	return output(
		options, "transfer",
		map[string]any{
			"uploaded": request.Source, "destination": cleanRemote(request.Destination),
			"recursive": request.Recursive, "verified": request.Verify,
		},
		"Uploaded %s to %s\n", request.Source, cleanRemote(request.Destination),
	)
}

func downloadFilesystem(
	ctx context.Context, client Client, request Request, options Options,
) error {
	if request.DryRun {
		return output(
			options, "transfer",
			map[string]any{
				"operation": "download", "source": cleanRemote(request.Source),
				"destination": request.Destination, "dryRun": true,
			},
			"Would download %s to %s\n",
			cleanRemote(request.Source), request.Destination,
		)
	}
	meta, err := client.Stat(request.Source)
	if err != nil {
		return err
	}
	if meta.Type == "directory" && !request.Recursive {
		return apperror.Wrap(
			apperror.KindUsage, "download",
			errors.New("remote path is a directory; use --recursive"),
		)
	}
	downloadDestination := request.Destination
	if request.Destination == "-" {
		if meta.Type == "directory" {
			return apperror.Wrap(
				apperror.KindUsage, "download",
				errors.New("directories cannot be downloaded to stdout"),
			)
		}
		var cleanup func()
		downloadDestination, cleanup, err = temporaryOutput()
		if err != nil {
			return err
		}
		defer cleanup()
	}
	if err := transfer.DownloadWithOptions(
		ctx, transferRemote(client, request, webdav.TUSCapabilities{}),
		request.Source, downloadDestination,
		transfer.Options{
			Concurrency: options.Concurrency, Progress: progressReporter(options),
		},
	); err != nil {
		return err
	}
	if request.Destination == "-" {
		return writeFileTo(options.Out, downloadDestination)
	}
	return output(
		options, "transfer",
		map[string]any{
			"downloaded":  cleanRemote(request.Source),
			"destination": request.Destination, "recursive": request.Recursive,
			"verified": request.Verify,
		},
		"Downloaded %s to %s\n",
		cleanRemote(request.Source), request.Destination,
	)
}

func copyOrMoveFilesystem(
	client Client, request Request, options Options,
) error {
	resolvedDestination, err := resolveCopyMoveDestination(
		client, request.Source, request.Destination,
	)
	if err != nil {
		return err
	}
	request.Destination = resolvedDestination
	return copyOrMoveFilesystemResolved(client, request, options)
}

func copyOrMoveFilesystemResolved(
	client Client, request Request, options Options,
) error {
	action, verb := "Moved", "move"
	if request.Operation == Copy {
		action, verb = "Copied", "copy"
	}
	if request.DryRun {
		return output(
			options, "transfer",
			map[string]any{
				"operation": request.Operation, "source": cleanRemote(request.Source),
				"destination": cleanRemote(request.Destination),
				"overwrite":   request.Overwrite, "dryRun": true,
			},
			"Would %s %s to %s\n",
			verb, cleanRemote(request.Source), cleanRemote(request.Destination),
		)
	}
	var err error
	if request.Operation == Copy {
		err = client.Copy(request.Source, request.Destination, request.Overwrite)
	} else {
		err = client.Move(request.Source, request.Destination, request.Overwrite)
	}
	if err != nil {
		return err
	}
	return output(
		options, "transfer",
		map[string]any{
			"source":      cleanRemote(request.Source),
			"destination": cleanRemote(request.Destination),
			"operation":   strings.ToLower(action), "overwrite": request.Overwrite,
		},
		"%s %s to %s\n",
		action, cleanRemote(request.Source), cleanRemote(request.Destination),
	)
}

func resolveCopyMoveDestination(
	client Client, source, destination string,
) (string, error) {
	cleanedDestination := cleanRemote(destination)
	requiresDirectory := strings.HasSuffix(destination, "/")
	meta, err := client.Stat(cleanedDestination)
	switch {
	case err == nil && meta.Type == "directory":
		name := path.Base(cleanRemote(source))
		if name == "/" || name == "." {
			return "", apperror.Wrap(
				apperror.KindUsage, "destination",
				errors.New("the remote root cannot be moved or copied into a directory"),
			)
		}
		return path.Join(cleanedDestination, name), nil
	case err == nil && requiresDirectory:
		return "", apperror.Wrap(
			apperror.KindUsage, "destination",
			fmt.Errorf("%s is not a directory", cleanedDestination),
		)
	case err == nil:
		return cleanedDestination, nil
	case webdav.StatusCode(err) == 404 && requiresDirectory:
		return "", fmt.Errorf(
			"destination directory %s does not exist: %w",
			cleanedDestination, err,
		)
	case webdav.StatusCode(err) == 404:
		return cleanedDestination, nil
	default:
		return "", err
	}
}

func transferRemote(
	client Client,
	request Request,
	uploadCapabilities webdav.TUSCapabilities,
) transfer.Remote {
	return transfer.Remote{
		Stat: func(_ context.Context, remote string) (transfer.Entry, error) {
			value, err := client.Stat(remote)
			return transfer.Entry{
				Name: value.Name, Path: value.Path, Type: value.Type, Size: value.Size,
			}, err
		},
		List: func(_ context.Context, remote string) ([]transfer.Entry, error) {
			values, err := client.List(remote)
			entries := make([]transfer.Entry, len(values))
			for index, value := range values {
				entries[index] = transfer.Entry{
					Name: value.Name, Path: value.Path,
					Type: value.Type, Size: value.Size,
				}
			}
			return entries, err
		},
		Upload: func(
			_ context.Context, local, remote string, progress func(int64),
		) error {
			return client.Upload(
				client.Context(), local, remote,
				webdav.TransferOptions{
					NoClobber: request.NoClobber,
					Verify:    request.Verify, Progress: progress,
					TUS: uploadCapabilities,
				},
			)
		},
		Download: func(
			_ context.Context, remote, local string, progress func(int64),
		) error {
			return client.Download(
				client.Context(), remote, local,
				webdav.TransferOptions{
					NoClobber: request.NoClobber, Verify: request.Verify,
					Resume: true, Progress: progress,
				},
			)
		},
		Mkdir: func(_ context.Context, remote string) error {
			return client.EnsureCollection(remote)
		},
	}
}

func discoverUploadCapabilities(
	ctx context.Context, client Client, options Options,
) webdav.TUSCapabilities {
	capabilities, err := client.SharingCapabilities(ctx)
	if err != nil {
		options.Logger.Debug(
			"TUS capability discovery failed; using WebDAV PUT",
			"reason", err.Error(),
		)
		return webdav.TUSCapabilities{}
	}
	return webdav.TUSCapabilities{
		Version:            capabilities.Files.TUS.Version,
		Resumable:          capabilities.Files.TUS.Resumable,
		Extensions:         capabilities.Files.TUS.Extensions,
		MaxChunkSize:       capabilities.Files.TUS.MaxChunkSize,
		HTTPMethodOverride: capabilities.Files.TUS.HTTPMethodOverride,
	}
}

func cleanRemote(remote string) string {
	return "/" + strings.TrimPrefix(path.Clean("/"+remote), "/")
}

func progressReporter(options Options) func(transfer.Progress) {
	if options.Quiet || options.OutputMode != appoutput.Human {
		return nil
	}
	terminalOutput := false
	if descriptor, ok := options.Err.(interface{ Fd() uintptr }); ok {
		terminalOutput = term.IsTerminal(int(descriptor.Fd()))
	}
	var lastUpdate time.Time
	lastFiles := -1
	return func(progress transfer.Progress) {
		now := time.Now()
		finished := progress.CompletedFiles == progress.TotalFiles
		if !finished && progress.CompletedFiles == lastFiles &&
			now.Sub(lastUpdate) < 200*time.Millisecond {
			return
		}
		lastUpdate, lastFiles = now, progress.CompletedFiles
		elapsed := time.Since(progress.StartedAt)
		var bytesPerSecond float64
		if elapsed > 0 {
			bytesPerSecond = float64(progress.CompletedBytes) / elapsed.Seconds()
		}
		percent := 100.0
		if progress.TotalBytes > 0 {
			percent = float64(progress.CompletedBytes) /
				float64(progress.TotalBytes) * 100
		}
		eta := time.Duration(0)
		if bytesPerSecond > 0 && progress.TotalBytes > progress.CompletedBytes {
			eta = time.Duration(
				float64(progress.TotalBytes-progress.CompletedBytes)/bytesPerSecond,
			) * time.Second
		}
		prefix, suffix := "", "\n"
		if terminalOutput {
			prefix, suffix = "\r", ""
			if finished {
				suffix = "\n"
			}
		}
		_, _ = fmt.Fprintf(
			options.Err,
			"%s%s [%s] %d/%d files, %d/%d bytes (%.0f%%, %.0f B/s, ETA %s): %s%s",
			prefix, progress.Operation, progressBar(percent),
			progress.CompletedFiles, progress.TotalFiles,
			progress.CompletedBytes, progress.TotalBytes, percent, bytesPerSecond,
			eta.Round(time.Second), progress.Destination, suffix,
		)
	}
}

func progressBar(percent float64) string {
	const width = 20
	filled := int(percent / 100 * width)
	filled = max(0, min(width, filled))
	return strings.Repeat("=", filled) + strings.Repeat(" ", width-filled)
}

func spoolInput(input io.Reader) (string, func(), error) {
	file, err := os.CreateTemp("", "ocis-upload-*")
	if err != nil {
		return "", func() {}, err
	}
	name := file.Name()
	cleanup := func() { _ = os.Remove(name) }
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		cleanup()
		return "", func() {}, err
	}
	if _, err := io.Copy(file, input); err != nil {
		_ = file.Close()
		cleanup()
		return "", func() {}, err
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return name, cleanup, nil
}

func temporaryOutput() (string, func(), error) {
	file, err := os.CreateTemp("", "ocis-download-*")
	if err != nil {
		return "", func() {}, err
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(name)
		return "", func() {}, err
	}
	return name, func() { _ = os.Remove(name) }, nil
}

func writeFileTo(destination io.Writer, name string) error {
	file, err := os.Open(name) //nolint:gosec // private temporary output file
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	_, err = io.Copy(destination, file)
	return err
}
