package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mzner/ocis-cli/internal/apperror"
)

type filesystemWalkEntry struct {
	item        item
	depth       int
	last        bool
	parentsLast []bool
}

type filesystemWalk struct {
	entries      []filesystemWalkEntry
	depthLimited bool
}

func walkFilesystem(
	client *client, remote string, maxDepth, maxEntries int,
	detectDepthLimit bool, operation string,
) (filesystemWalk, error) {
	root, err := client.stat(remote)
	if err != nil {
		return filesystemWalk{}, err
	}
	result := filesystemWalk{entries: []filesystemWalkEntry{{
		item: root, depth: 0, last: true,
	}}}
	if root.Type != "directory" {
		return result, nil
	}
	if maxDepth == 0 {
		if detectDepthLimit {
			children, listErr := client.list(remote)
			if listErr != nil {
				return filesystemWalk{}, listErr
			}
			result.depthLimited = len(children) > 0
		}
		return result, nil
	}
	if err := appendFilesystemWalk(
		client, remote, 0, nil, maxDepth, maxEntries,
		detectDepthLimit, operation, &result,
	); err != nil {
		return filesystemWalk{}, err
	}
	return result, nil
}

func appendFilesystemWalk(
	client *client, remote string, depth int, parentsLast []bool,
	maxDepth, maxEntries int, detectDepthLimit bool, operation string,
	result *filesystemWalk,
) error {
	children, err := client.list(remote)
	if err != nil {
		return err
	}
	sort.SliceStable(children, func(left, right int) bool {
		leftName := strings.ToLower(children[left].Name)
		rightName := strings.ToLower(children[right].Name)
		if leftName == rightName {
			return children[left].Name < children[right].Name
		}
		return leftName < rightName
	})
	for index, child := range children {
		if len(result.entries) >= maxEntries {
			return apperror.Wrap(
				apperror.KindUsage, operation,
				fmt.Errorf(
					"remote traversal exceeds --max-entries %d; increase the limit to continue",
					maxEntries,
				),
			)
		}
		last := index == len(children)-1
		childDepth := depth + 1
		result.entries = append(result.entries, filesystemWalkEntry{
			item: child, depth: childDepth, last: last,
			parentsLast: append([]bool(nil), parentsLast...),
		})
		if child.Type != "directory" {
			continue
		}
		if childDepth < maxDepth {
			if err := appendFilesystemWalk(
				client, child.Path, childDepth,
				append(parentsLast, last), maxDepth, maxEntries,
				detectDepthLimit, operation, result,
			); err != nil {
				return err
			}
			continue
		}
		if detectDepthLimit && childDepth == maxDepth {
			grandchildren, listErr := client.list(child.Path)
			if listErr != nil {
				return listErr
			}
			if len(grandchildren) > 0 {
				result.depthLimited = true
			}
		}
	}
	return nil
}
