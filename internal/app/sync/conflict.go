package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/mzner/ocis-cli/internal/apperror"
	syncmodel "github.com/mzner/ocis-cli/internal/sync"
)

func resolveBidirectionalConflicts(
	plan syncmodel.Plan,
	local syncmodel.Snapshot,
	remote syncmodel.Snapshot,
	request Request,
) (syncmodel.Plan, error) {
	strategy := request.ConflictStrategy
	if strategy == "" || strategy == "abort" || plan.Conflicts == 0 {
		return plan, nil
	}
	actions := make([]syncmodel.Action, 0, len(plan.Actions)+plan.Conflicts*2)
	for _, action := range plan.Actions {
		if action.Action != syncmodel.ActionConflict {
			actions = append(actions, action)
			continue
		}
		if action.Path == "" || action.Source.Type != "file" ||
			action.Destination.Type != "file" {
			return plan, apperror.Wrap(
				apperror.KindConflict, "bidirectional conflict resolution",
				fmt.Errorf(
					"%s is not an ordinary file/content conflict and cannot use keep-both",
					displaySyncPath(action.Path),
				),
			)
		}
		losingSide := syncmodel.Remote
		losingEntry := action.Destination
		if request.Prefer == "remote" {
			losingSide = syncmodel.Local
			losingEntry = action.Source
		}
		conflictPath := syncConflictCopyPath(
			action.Path, losingSide, losingEntry,
		)
		if err := ensureConflictCopyPathAvailable(
			conflictPath, local, remote, request,
		); err != nil {
			return plan, err
		}
		if request.Prefer == "local" {
			actions = append(actions,
				syncmodel.Action{
					Action: syncmodel.ActionCopy, FromPath: action.Path,
					Path: conflictPath, Target: syncmodel.Remote,
					Type: "file", Source: action.Destination,
					Reason: "preserve remote conflict version",
				},
				syncmodel.Action{
					Action: syncmodel.ActionTransfer, FromPath: conflictPath,
					Path: conflictPath, Target: syncmodel.Local,
					Type: "file", Source: action.Destination,
					Reason: "copy preserved remote conflict version locally",
				},
				syncmodel.Action{
					Action: syncmodel.ActionTransfer, Path: action.Path,
					Target: syncmodel.Remote, Type: "file",
					Source: action.Source, Destination: action.Destination,
					Reason: "explicitly prefer local version after preserving remote",
				},
			)
		} else {
			actions = append(actions,
				syncmodel.Action{
					Action: syncmodel.ActionCopy, FromPath: action.Path,
					Path: conflictPath, Target: syncmodel.Local,
					Type: "file", Source: action.Source,
					Reason: "preserve local conflict version",
				},
				syncmodel.Action{
					Action: syncmodel.ActionTransfer, FromPath: conflictPath,
					Path: conflictPath, Target: syncmodel.Remote,
					Type: "file", Source: action.Source,
					Reason: "copy preserved local conflict version remotely",
				},
				syncmodel.Action{
					Action: syncmodel.ActionTransfer, Path: action.Path,
					Target: syncmodel.Local, Type: "file",
					Source: action.Destination, Destination: action.Source,
					Reason: "explicitly prefer remote version after preserving local",
				},
			)
		}
	}
	plan.Actions = actions
	plan.Conflicts = 0
	plan.Transfers = 0
	plan.Deletions = 0
	plan.Moves = 0
	plan.Copies = 0
	for _, action := range actions {
		switch action.Action {
		case syncmodel.ActionConflict:
			plan.Conflicts++
		case syncmodel.ActionTransfer:
			plan.Transfers++
		case syncmodel.ActionDelete:
			plan.Deletions++
		case syncmodel.ActionMove:
			plan.Moves++
		case syncmodel.ActionCopy:
			plan.Copies++
		}
	}
	return plan, nil
}

func syncConflictCopyPath(
	relative string,
	side syncmodel.Side,
	entry syncmodel.Entry,
) string {
	fingerprint := entry.Checksum
	if fingerprint == "" {
		fingerprint = entry.ETag
	}
	if fingerprint == "" {
		fingerprint = fmt.Sprintf("%d:%s", entry.Size, entry.Modified)
	}
	sum := sha256.Sum256([]byte(string(side) + "\x00" + fingerprint))
	suffix := hex.EncodeToString(sum[:4])
	directory, name := path.Split(relative)
	extension := path.Ext(name)
	stem := strings.TrimSuffix(name, extension)
	return path.Join(
		strings.TrimSuffix(directory, "/"),
		stem+".conflict-"+string(side)+"-"+suffix+extension,
	)
}

func ensureConflictCopyPathAvailable(
	relative string,
	local syncmodel.Snapshot,
	remote syncmodel.Snapshot,
	request Request,
) error {
	if _, exists := local[relative]; exists {
		return apperror.Wrap(
			apperror.KindConflict, "bidirectional conflict resolution",
			fmt.Errorf("conflict-copy path already exists locally: %s", relative),
		)
	}
	if _, exists := remote[relative]; exists {
		return apperror.Wrap(
			apperror.KindConflict, "bidirectional conflict resolution",
			fmt.Errorf("conflict-copy path already exists remotely: %s", relative),
		)
	}
	selected, err := syncmodel.Filter(
		syncmodel.Snapshot{relative: {Path: relative, Type: "file"}},
		request.Includes, request.Excludes,
	)
	if err != nil {
		return err
	}
	if _, exists := selected[relative]; !exists {
		return errors.New(
			"generated conflict-copy path is excluded by the active sync filters",
		)
	}
	return nil
}
