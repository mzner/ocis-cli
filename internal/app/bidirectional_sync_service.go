package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mzner/ocis-cli/internal/apperror"
	syncmodel "github.com/mzner/ocis-cli/internal/sync"
	"github.com/mzner/ocis-cli/internal/syncrecovery"
	"github.com/mzner/ocis-cli/internal/webdav"
)

func runPreparedBidirectionalSync(
	ctx context.Context,
	prepared preparedSyncRequest,
	client *client,
	options RunOptions,
) error {
	request := prepared.request
	accountID := profileIdentity(client.profile)
	if accountID == "" {
		return errors.New("cannot bind sync state to an unauthenticated account")
	}
	binding := syncmodel.Binding{
		Profile: client.name, AccountID: accountID,
		SpaceID:   client.selectedSpaceID(),
		Direction: syncmodel.Bidirectional,
		LocalRoot: prepared.localRoot, RemoteRoot: prepared.remoteRoot,
		Includes: request.Includes, Excludes: request.Excludes,
	}
	stateKey := binding.Key()
	previous, found, err := options.Dependencies.SyncStates.Load(stateKey)
	if err != nil {
		return fmt.Errorf("load sync state: %w", err)
	}
	if found && previous.Binding.Key() != binding.Key() {
		return errors.New("saved sync state does not match the current binding")
	}

	local, remote, err := collectBidirectionalSnapshots(
		ctx, client, prepared.localRoot, prepared.remoteRoot,
		request.MaxEntries,
	)
	if err != nil {
		return err
	}
	var baseline *syncmodel.State
	if found {
		baseline = &previous
	}
	plan, err := syncmodel.BuildBidirectional(
		local, remote, baseline,
		syncmodel.Options{
			Includes: request.Includes, Excludes: request.Excludes,
		},
	)
	if err != nil {
		return fmt.Errorf("build bidirectional sync plan: %w", err)
	}
	originalPlan := plan
	plan, err = resolveBidirectionalConflicts(
		originalPlan, local, remote, request,
	)
	if err != nil {
		if !request.DryRun {
			journal := syncrecovery.New(
				binding, request.MaxEntries, originalPlan, time.Now(),
			)
			journal.Status = syncrecovery.Conflict
			journal.Failure = "automatic keep-both resolution was not safe; both trees were left unchanged"
			if saveErr := options.Dependencies.SyncRecoveries.Save(journal); saveErr != nil {
				return errors.Join(err, fmt.Errorf("save sync conflict report: %w", saveErr))
			}
		}
		return err
	}
	result := syncResult{
		Direction: syncmodel.Bidirectional,
		LocalRoot: prepared.localRoot, RemoteRoot: prepared.remoteRoot,
		DryRun: request.DryRun, Applied: false, StateKey: stateKey,
		Conflicts: plan.Conflicts, Transfers: plan.Transfers,
		Moves: plan.Moves, Copies: plan.Copies,
		Deletions: plan.Deletions, Actions: plan.Actions,
	}
	if request.DryRun {
		return writeSyncResult(options, result)
	}
	journal := syncrecovery.New(binding, request.MaxEntries, plan, time.Now())
	if plan.Conflicts > 0 {
		journal.Status = syncrecovery.Conflict
		journal.Failure = "the plan contains unresolved conflicts"
		journal.UpdatedAt = time.Now().UTC()
		if err := options.Dependencies.SyncRecoveries.Save(journal); err != nil {
			return fmt.Errorf("save sync conflict report: %w", err)
		}
		return bidirectionalSyncConflict(plan)
	}
	if err := options.Dependencies.SyncRecoveries.Save(journal); err != nil {
		return fmt.Errorf("create sync recovery journal: %w", err)
	}
	if err := applyBidirectionalSyncPlan(
		ctx, client, prepared.localRoot, prepared.remoteRoot,
		plan, &journal, options,
	); err != nil {
		status := syncrecovery.Failed
		failure := "synchronization stopped before convergence; re-scan before retrying"
		if errors.Is(err, context.Canceled) {
			status = syncrecovery.Canceled
			failure = "synchronization was canceled; re-scan before retrying"
		}
		if saveErr := updateSyncRecovery(
			&journal, status, failure, options,
		); saveErr != nil {
			return errors.Join(err, fmt.Errorf("update sync recovery journal: %w", saveErr))
		}
		return err
	}

	postLocal, postRemote, err := collectBidirectionalSnapshots(
		ctx, client, prepared.localRoot, prepared.remoteRoot,
		request.MaxEntries,
	)
	if err != nil {
		_ = updateSyncRecovery(
			&journal, syncrecovery.Failed,
			"post-run verification failed; re-scan before retrying", options,
		)
		return fmt.Errorf("verify synchronized trees: %w", err)
	}
	postLocal, err = syncmodel.Filter(
		postLocal, request.Includes, request.Excludes,
	)
	if err != nil {
		return err
	}
	postRemote, err = syncmodel.Filter(
		postRemote, request.Includes, request.Excludes,
	)
	if err != nil {
		return err
	}
	if difference := firstSyncSnapshotDifference(
		postLocal, postRemote,
	); difference != "" {
		_ = updateSyncRecovery(
			&journal, syncrecovery.Failed,
			"local and remote trees did not converge; re-scan before retrying",
			options,
		)
		return apperror.Wrap(
			apperror.KindConflict, "bidirectional sync verification",
			fmt.Errorf(
				"local and remote trees did not converge at %s; saved state was not advanced",
				displaySyncPath(difference),
			),
		)
	}
	state := syncmodel.NewState(binding, postLocal, postRemote)
	if err := options.Dependencies.SyncStates.Save(
		stateKey, state,
	); err != nil {
		_ = updateSyncRecovery(
			&journal, syncrecovery.Failed,
			"trees converged but saving the baseline failed; re-scan before retrying",
			options,
		)
		return fmt.Errorf("save sync state: %w", err)
	}
	if _, err := options.Dependencies.SyncRecoveries.Delete(journal.ID); err != nil {
		return fmt.Errorf("remove completed sync recovery journal: %w", err)
	}
	result.Applied = true
	return writeSyncResult(options, result)
}

func applyBidirectionalSyncPlan(
	ctx context.Context,
	client *client,
	localRoot string,
	remoteRoot string,
	plan syncmodel.Plan,
	journal *syncrecovery.Journal,
	options RunOptions,
) error {
	uploadCapabilities := webdav.TUSCapabilities{}
	for _, action := range plan.Actions {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch action.Action {
		case syncmodel.ActionSkip:
			continue
		case syncmodel.ActionConflict:
			return errors.New(
				"cannot apply a bidirectional synchronization plan with conflicts",
			)
		}
		if action.Action != syncmodel.ActionCopy {
			if err := verifyBidirectionalSourcePrecondition(
				client, localRoot, remoteRoot, action,
			); err != nil {
				return err
			}
		}
		current := action
		journal.Current = &current
		journal.UpdatedAt = time.Now().UTC()
		if err := options.Dependencies.SyncRecoveries.Save(*journal); err != nil {
			return fmt.Errorf("record pending sync action: %w", err)
		}
		var direction syncmodel.Direction
		switch action.Target {
		case syncmodel.Remote:
			direction = syncmodel.Push
		case syncmodel.Local:
			direction = syncmodel.Pull
		default:
			return fmt.Errorf(
				"bidirectional action %q for %s has no valid target",
				action.Action, displaySyncPath(action.Path),
			)
		}
		if err := applySyncAction(
			client, direction, localRoot, remoteRoot,
			action, uploadCapabilities, options,
		); err != nil {
			return fmt.Errorf(
				"sync %s %s on %s: %w",
				action.Action, displaySyncPath(action.Path),
				action.Target, err,
			)
		}
		journal.Completed = append(journal.Completed, action)
		journal.Current = nil
		journal.UpdatedAt = time.Now().UTC()
		if err := options.Dependencies.SyncRecoveries.Save(*journal); err != nil {
			return fmt.Errorf("record completed sync action: %w", err)
		}
	}
	return nil
}

func updateSyncRecovery(
	journal *syncrecovery.Journal,
	status syncrecovery.Status,
	failure string,
	options RunOptions,
) error {
	journal.Status = status
	journal.Failure = failure
	journal.UpdatedAt = time.Now().UTC()
	return options.Dependencies.SyncRecoveries.Save(*journal)
}

func verifyBidirectionalSourcePrecondition(
	client *client,
	localRoot string,
	remoteRoot string,
	action syncmodel.Action,
) error {
	sourcePath := action.Path
	if action.FromPath != "" && action.Action != syncmodel.ActionMove {
		sourcePath = action.FromPath
	}
	switch action.Target {
	case syncmodel.Remote:
		local, err := syncLocalPath(localRoot, sourcePath)
		if err != nil {
			return err
		}
		return verifyLocalSyncSourcePrecondition(local, action.Source)
	case syncmodel.Local:
		return verifyRemoteSyncSourcePrecondition(
			client, syncRemotePath(remoteRoot, sourcePath), action.Source,
		)
	default:
		return fmt.Errorf("invalid bidirectional target %q", action.Target)
	}
}

func firstSyncSnapshotDifference(
	local syncmodel.Snapshot,
	remote syncmodel.Snapshot,
) string {
	paths := make(map[string]struct{}, len(local)+len(remote))
	for relative := range local {
		paths[relative] = struct{}{}
	}
	for relative := range remote {
		paths[relative] = struct{}{}
	}
	ordered := make([]string, 0, len(paths))
	for relative := range paths {
		ordered = append(ordered, relative)
	}
	sort.Strings(ordered)
	for _, relative := range ordered {
		localEntry, localExists := local[relative]
		remoteEntry, remoteExists := remote[relative]
		if localExists != remoteExists ||
			(localExists && !localEntry.Converged(remoteEntry)) {
			return relative
		}
	}
	return ""
}

func bidirectionalSyncConflict(plan syncmodel.Plan) error {
	paths := make([]string, 0, plan.Conflicts)
	for _, action := range plan.Actions {
		if action.Action != syncmodel.ActionConflict {
			continue
		}
		paths = append(paths, displaySyncPath(action.Path))
		if len(paths) == 3 {
			break
		}
	}
	return apperror.Wrap(
		apperror.KindConflict, "bidirectional sync plan",
		fmt.Errorf(
			"%d bidirectional conflict(s): %s; neither tree was changed; run with --dry-run to inspect the plan",
			plan.Conflicts, strings.Join(paths, ", "),
		),
	)
}

func collectBidirectionalSnapshots(
	ctx context.Context,
	client *client,
	localRoot string,
	remoteRoot string,
	maxEntries int,
) (syncmodel.Snapshot, syncmodel.Snapshot, error) {
	local, err := snapshotLocal(ctx, localRoot, true, maxEntries)
	if err != nil {
		return nil, nil, err
	}
	remote, err := snapshotRemote(
		ctx, client, remoteRoot, true, maxEntries,
	)
	if err != nil {
		return nil, nil, err
	}
	if _, localExists := local[""]; !localExists {
		if _, remoteExists := remote[""]; !remoteExists {
			return nil, nil, errors.New(
				"local and remote sync roots are both missing",
			)
		}
	}
	return local, remote, nil
}
