package app

import (
	"errors"
	"fmt"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

func treeFilesystem(
	client *client, request FilesystemRequest, options RunOptions,
) error {
	if request.MaxDepth < 0 {
		return apperror.Wrap(
			apperror.KindUsage, "tree",
			errors.New("--max-depth cannot be negative"),
		)
	}
	if request.MaxEntries < 1 {
		return apperror.Wrap(
			apperror.KindUsage, "tree",
			errors.New("--max-entries must be at least 1"),
		)
	}
	remote := cleanRemote(request.Source)
	walk, err := walkFilesystem(
		client, remote, request.MaxDepth, request.MaxEntries, false, "tree",
	)
	if err != nil {
		return err
	}
	entries := make([]FilesystemTreeEntry, len(walk.entries))
	for index, node := range walk.entries {
		entries[index] = FilesystemTreeEntry{
			Name: node.item.Name, Path: node.item.Path, Type: node.item.Type,
			Size: node.item.Size, Depth: node.depth,
		}
	}
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "item", entries)
	}
	rootLabel := remote
	if walk.entries[0].item.Type == "directory" && remote != "/" {
		rootLabel += "/"
	}
	if _, err := fmt.Fprintln(options.Out, rootLabel); err != nil {
		return err
	}
	for _, node := range walk.entries[1:] {
		prefix := ""
		for _, parentLast := range node.parentsLast {
			if parentLast {
				prefix += "    "
			} else {
				prefix += "│   "
			}
		}
		if node.last {
			prefix += "└── "
		} else {
			prefix += "├── "
		}
		name := node.item.Name
		if node.item.Type == "directory" {
			name += "/"
		}
		if _, err := fmt.Fprintln(options.Out, prefix+name); err != nil {
			return err
		}
	}
	return nil
}
