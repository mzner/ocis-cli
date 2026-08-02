package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
	syncmodel "github.com/mzner/ocis-cli/internal/sync"
	"github.com/mzner/ocis-cli/internal/syncrecovery"
)

type syncRecoveryRemoval struct {
	ID      string `json:"id"`
	Removed bool   `json:"removed"`
	DryRun  bool   `json:"dryRun"`
}

func runSyncRecovery(
	ctx context.Context,
	request SyncRecoveryRequest,
	options RunOptions,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch request.Operation {
	case SyncRecoveryList:
		return listSyncRecoveries(request.Profile, options)
	case SyncRecoveryShow:
		journal, err := loadSyncRecovery(request.ID, options)
		if err != nil {
			return err
		}
		return writeSyncRecovery(journal, options)
	case SyncRecoveryRetry:
		journal, err := loadSyncRecovery(request.ID, options)
		if err != nil {
			return err
		}
		return retrySyncRecovery(ctx, request, journal, options)
	case SyncRecoveryRemove:
		return removeSyncRecovery(request, options)
	default:
		return apperror.Wrap(
			apperror.KindUsage, "sync recovery",
			fmt.Errorf("unknown sync recovery command %q", request.Operation),
		)
	}
}

func listSyncRecoveries(profile string, options RunOptions) error {
	keys, err := options.Dependencies.SyncRecoveries.Keys()
	if err != nil {
		return fmt.Errorf("list sync recovery journals: %w", err)
	}
	journals := make([]syncrecovery.Journal, 0, len(keys))
	for _, key := range keys {
		journal, found, err := options.Dependencies.SyncRecoveries.Load(key)
		if err != nil {
			return fmt.Errorf("load sync recovery %s: %w", key, err)
		}
		if found && (profile == "" || journal.Binding.Profile == profile) {
			journals = append(journals, journal)
		}
	}
	sort.Slice(journals, func(i, j int) bool {
		return journals[i].UpdatedAt.Before(journals[j].UpdatedAt)
	})
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "sync-recovery", journals)
	}
	if len(journals) == 0 {
		_, err := fmt.Fprintln(options.Out, "No synchronization recovery journals found.")
		return err
	}
	writer := tabwriter.NewWriter(options.Out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(
		writer, "ID\tPROFILE\tSTATUS\tCOMPLETED\tUPDATED\tROOTS",
	); err != nil {
		return err
	}
	for _, journal := range journals {
		if _, err := fmt.Fprintf(
			writer, "%s\t%s\t%s\t%d/%d\t%s\t%s <-> %s\n",
			uniqueSyncStateID(journal.ID, keys), journal.Binding.Profile, journal.Status,
			len(journal.Completed), actionableSyncCount(journal.Plan),
			journal.UpdatedAt.Format("2006-01-02 15:04:05Z07:00"),
			journal.Binding.LocalRoot, journal.Binding.RemoteRoot,
		); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func writeSyncRecovery(
	journal syncrecovery.Journal,
	options RunOptions,
) error {
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "sync-recovery", journal)
	}
	if _, err := fmt.Fprintf(
		options.Out,
		"ID: %s\nStatus: %s\nProfile: %s\nAccount: %s\nSpace: %s\n"+
			"Local root: %s\nRemote root: %s\nCompleted actions: %d/%d\n"+
			"Started: %s\nUpdated: %s\n",
		journal.ID, journal.Status, journal.Binding.Profile,
		journal.Binding.AccountID, recoverySpace(journal),
		journal.Binding.LocalRoot, journal.Binding.RemoteRoot,
		len(journal.Completed), actionableSyncCount(journal.Plan),
		journal.StartedAt.Format("2006-01-02 15:04:05Z07:00"),
		journal.UpdatedAt.Format("2006-01-02 15:04:05Z07:00"),
	); err != nil {
		return err
	}
	if journal.Current != nil {
		if _, err := fmt.Fprintf(
			options.Out, "Uncertain action: %s %s on %s\n",
			journal.Current.Action, displaySyncActionPath(*journal.Current),
			journal.Current.Target,
		); err != nil {
			return err
		}
	}
	if journal.Failure != "" {
		_, err := fmt.Fprintf(options.Out, "Recovery: %s\n", journal.Failure)
		return err
	}
	return nil
}

func retrySyncRecovery(
	ctx context.Context,
	request SyncRecoveryRequest,
	journal syncrecovery.Journal,
	options RunOptions,
) error {
	if request.Profile != "" && request.Profile != journal.Binding.Profile {
		return apperror.Wrap(
			apperror.KindUsage, "sync recovery retry",
			fmt.Errorf(
				"recovery %q is bound to profile %q, not %q",
				journal.ID, journal.Binding.Profile, request.Profile,
			),
		)
	}
	client, err := newClientWithOptions(ctx, journal.Binding.Profile, options)
	if err != nil {
		return err
	}
	if profileIdentity(client.profile) != journal.Binding.AccountID {
		return apperror.Wrap(
			apperror.KindAuthentication, "sync recovery retry",
			errors.New("the recovery journal belongs to a different authenticated account"),
		)
	}
	if journal.Binding.SpaceID != "" {
		if err := client.selectSpace(journal.Binding.SpaceID); err != nil {
			return err
		}
	}
	if client.selectedSpaceID() != journal.Binding.SpaceID {
		return errors.New("resolved recovery Space does not match its binding")
	}
	prepared, err := prepareSyncRequest(SyncRequest{
		Direction:  SyncBidirectional,
		LocalRoot:  journal.Binding.LocalRoot,
		RemoteRoot: journal.Binding.RemoteRoot,
		Includes:   journal.Binding.Includes,
		Excludes:   journal.Binding.Excludes,
		MaxEntries: journal.MaxEntries,
		DryRun:     request.DryRun,
	})
	if err != nil {
		return err
	}
	return runPreparedSync(ctx, prepared, client, options)
}

func removeSyncRecovery(
	request SyncRecoveryRequest,
	options RunOptions,
) error {
	if !request.Confirmed && !request.DryRun {
		return apperror.Wrap(
			apperror.KindUsage, "sync recovery remove",
			errors.New("removing a recovery journal requires explicit confirmation"),
		)
	}
	journal, err := loadSyncRecovery(request.ID, options)
	if err != nil {
		return err
	}
	result := syncRecoveryRemoval{ID: journal.ID, DryRun: request.DryRun}
	if !request.DryRun {
		removed, err := options.Dependencies.SyncRecoveries.Delete(journal.ID)
		if err != nil {
			return fmt.Errorf("remove sync recovery journal: %w", err)
		}
		result.Removed = removed
	}
	verb := "Removed"
	if request.DryRun {
		verb = "Would remove"
	}
	return output(
		options, "sync-recovery-removal", result,
		verb+" sync recovery journal %s\n", journal.ID,
	)
}

func loadSyncRecovery(
	id string,
	options RunOptions,
) (syncrecovery.Journal, error) {
	key, err := resolveSyncRecoveryID(id, options)
	if err != nil {
		return syncrecovery.Journal{}, err
	}
	journal, found, err := options.Dependencies.SyncRecoveries.Load(key)
	if err != nil {
		return syncrecovery.Journal{}, err
	}
	if !found {
		return syncrecovery.Journal{}, apperror.Wrap(
			apperror.KindNotFound, "sync recovery",
			fmt.Errorf("unknown sync recovery journal %q", id),
		)
	}
	return journal, nil
}

func resolveSyncRecoveryID(
	query string,
	options RunOptions,
) (string, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	if len(query) < syncStateIDMinimumPrefix ||
		strings.Trim(query, "0123456789abcdef") != "" {
		return "", apperror.Wrap(
			apperror.KindUsage, "sync recovery",
			fmt.Errorf(
				"recovery ID must be at least %d hexadecimal characters",
				syncStateIDMinimumPrefix,
			),
		)
	}
	keys, err := options.Dependencies.SyncRecoveries.Keys()
	if err != nil {
		return "", err
	}
	matches := make([]string, 0, 1)
	for _, key := range keys {
		if strings.HasPrefix(key, query) {
			matches = append(matches, key)
		}
	}
	switch len(matches) {
	case 0:
		return "", apperror.Wrap(
			apperror.KindNotFound, "sync recovery",
			fmt.Errorf("unknown sync recovery journal %q", query),
		)
	case 1:
		return matches[0], nil
	default:
		return "", apperror.Wrap(
			apperror.KindUsage, "sync recovery",
			fmt.Errorf("recovery ID prefix %q is ambiguous", query),
		)
	}
}

func actionableSyncCount(plan syncmodel.Plan) int {
	count := 0
	for _, action := range plan.Actions {
		if action.Action != syncmodel.ActionSkip {
			count++
		}
	}
	return count
}

func recoverySpace(journal syncrecovery.Journal) string {
	if journal.Binding.SpaceID == "" {
		return "personal"
	}
	return journal.Binding.SpaceID
}
