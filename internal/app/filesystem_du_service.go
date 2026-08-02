package app

import (
	"errors"
	"fmt"

	"github.com/mzner/ocis-cli/internal/apperror"
)

func duFilesystem(
	client *client, request FilesystemRequest, options RunOptions,
) error {
	if request.MaxDepth < 0 {
		return apperror.Wrap(
			apperror.KindUsage, "du",
			errors.New("--max-depth cannot be negative"),
		)
	}
	if request.MaxEntries < 1 {
		return apperror.Wrap(
			apperror.KindUsage, "du",
			errors.New("--max-entries must be at least 1"),
		)
	}
	remote := cleanRemote(request.Source)
	walk, err := walkFilesystem(
		client, remote, request.MaxDepth, request.MaxEntries, true, "du",
	)
	if err != nil {
		return err
	}
	usage := FilesystemUsage{
		Path: remote, Entries: len(walk.entries), MaxDepth: request.MaxDepth,
		MaxEntries: request.MaxEntries, Complete: !walk.depthLimited,
	}
	for _, entry := range walk.entries {
		if entry.item.Type == "directory" {
			usage.Directories++
			continue
		}
		usage.Files++
		usage.LogicalBytes += entry.item.Size
	}
	if options.OutputMode != "human" {
		return writeOutput(options, "usage", usage)
	}
	completeness := "complete"
	if !usage.Complete {
		completeness = "depth-limited"
	}
	_, err = fmt.Fprintf(
		options.Out,
		"%d bytes  %d files  %d directories  %s  (%s)\n",
		usage.LogicalBytes, usage.Files, usage.Directories,
		usage.Path, completeness,
	)
	return err
}
