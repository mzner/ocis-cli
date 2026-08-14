package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
	syncmodel "github.com/mzner/ocis-cli/internal/sync"
)

const (
	syncStateIDDisplayLength = 12
	syncStateIDMinimumPrefix = 8
)

type syncStateSummary struct {
	ID         string              `json:"id"`
	Status     string              `json:"status"`
	Error      string              `json:"error,omitempty"`
	Version    int                 `json:"version,omitempty"`
	Profile    string              `json:"profile,omitempty"`
	SpaceID    string              `json:"spaceId,omitempty"`
	Direction  syncmodel.Direction `json:"direction,omitempty"`
	LocalRoot  string              `json:"localRoot,omitempty"`
	RemoteRoot string              `json:"remoteRoot,omitempty"`
	Includes   []string            `json:"includes,omitempty"`
	Excludes   []string            `json:"excludes,omitempty"`
	Entries    int                 `json:"entries,omitempty"`
}

type syncStateExportDocument struct {
	SchemaVersion string          `json:"schemaVersion"`
	ID            string          `json:"id"`
	State         syncmodel.State `json:"state"`
}

type syncStateRemoval struct {
	ID      string `json:"id"`
	Removed bool   `json:"removed"`
	DryRun  bool   `json:"dryRun"`
}

func RunState(
	ctx context.Context,
	request StateRequest,
	options Options,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch request.Operation {
	case StateList:
		return listSyncStates(request.Profile, options)
	case StateShow:
		key, state, err := resolveAndLoadSyncState(request.ID, options)
		if err != nil {
			return err
		}
		return writeSyncStateSummary(
			options, syncStateSummaryFor(key, state, nil),
		)
	case StateExport:
		if options.OutputMode != appoutput.Human {
			return apperror.Wrap(
				apperror.KindUsage, "sync state export",
				errors.New(
					"--json and --jsonl are unnecessary because export already writes JSON",
				),
			)
		}
		key, state, err := resolveAndLoadSyncState(request.ID, options)
		if err != nil {
			return err
		}
		return exportSyncState(options.Out, key, state)
	case StateRemove:
		return removeSyncState(request, options)
	default:
		return apperror.Wrap(
			apperror.KindUsage, "sync state",
			fmt.Errorf("unknown sync state command %q", request.Operation),
		)
	}
}

func listSyncStates(profile string, options Options) error {
	keys, err := options.SyncStates.Keys()
	if err != nil {
		return fmt.Errorf("list sync state: %w", err)
	}
	summaries := make([]syncStateSummary, 0, len(keys))
	for _, key := range keys {
		state, found, loadErr := options.SyncStates.Load(key)
		if !found && loadErr == nil {
			continue
		}
		summary := syncStateSummaryFor(key, state, loadErr)
		if profile != "" && summary.Status == "valid" &&
			summary.Profile != profile {
			continue
		}
		summaries = append(summaries, summary)
	}
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "sync-state", summaries)
	}
	if len(summaries) == 0 {
		_, err := fmt.Fprintln(options.Out, "No synchronization state found.")
		return err
	}
	writer := tabwriter.NewWriter(options.Out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(
		writer, "ID\tPROFILE\tDIRECTION\tSPACE\tENTRIES\tSTATUS\tROOTS",
	); err != nil {
		return err
	}
	for _, summary := range summaries {
		space := summary.SpaceID
		if space == "" {
			space = "personal"
		}
		roots := summary.LocalRoot + " <-> " + summary.RemoteRoot
		if summary.Status != "valid" {
			space, roots = "-", summary.Error
		}
		if _, err := fmt.Fprintf(
			writer, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
			uniqueSyncStateID(summary.ID, keys),
			summary.Profile, summary.Direction,
			space, summary.Entries, summary.Status, roots,
		); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func writeSyncStateSummary(
	options Options,
	summary syncStateSummary,
) error {
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "sync-state", summary)
	}
	space := summary.SpaceID
	if space == "" {
		space = "personal"
	}
	_, err := fmt.Fprintf(
		options.Out,
		"ID: %s\nStatus: %s\nVersion: %d\nProfile: %s\n"+
			"Space: %s\nDirection: %s\nLocal root: %s\n"+
			"Remote root: %s\nEntries: %d\n",
		summary.ID, summary.Status, summary.Version, summary.Profile,
		space, summary.Direction, summary.LocalRoot, summary.RemoteRoot,
		summary.Entries,
	)
	if err != nil {
		return err
	}
	if len(summary.Includes) > 0 {
		if _, err := fmt.Fprintf(
			options.Out, "Includes: %v\n", summary.Includes,
		); err != nil {
			return err
		}
	}
	if len(summary.Excludes) > 0 {
		_, err := fmt.Fprintf(
			options.Out, "Excludes: %v\n", summary.Excludes,
		)
		return err
	}
	return nil
}

func exportSyncState(
	writer io.Writer,
	key string,
	state syncmodel.State,
) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(syncStateExportDocument{
		SchemaVersion: "1", ID: key, State: state,
	})
}

func removeSyncState(
	request StateRequest,
	options Options,
) error {
	if !request.Confirmed && !request.DryRun {
		return apperror.Wrap(
			apperror.KindUsage, "sync state remove",
			errors.New("removing synchronization state requires explicit confirmation"),
		)
	}
	key, err := resolveSyncStateID(request.ID, options)
	if err != nil {
		return err
	}
	result := syncStateRemoval{ID: key, DryRun: request.DryRun}
	if !request.DryRun {
		removed, err := options.SyncStates.Delete(key)
		if err != nil {
			return fmt.Errorf("remove sync state: %w", err)
		}
		if !removed {
			return syncStateNotFound(request.ID)
		}
		result.Removed = true
	}
	return output(
		options, "sync-state-removal", result,
		map[bool]string{
			true:  "Would remove synchronization state %s\n",
			false: "Removed synchronization state %s\n",
		}[request.DryRun],
		key,
	)
}

func resolveAndLoadSyncState(
	query string,
	options Options,
) (string, syncmodel.State, error) {
	key, err := resolveSyncStateID(query, options)
	if err != nil {
		return "", syncmodel.State{}, err
	}
	state, found, err := options.SyncStates.Load(key)
	if err != nil {
		return "", syncmodel.State{}, fmt.Errorf(
			"sync state %s is unreadable: %w; remove it with "+
				"`ocis sync state remove %s`",
			shortSyncStateID(key), err, key,
		)
	}
	if !found {
		return "", syncmodel.State{}, syncStateNotFound(query)
	}
	if state.Binding.Key() != key {
		return "", syncmodel.State{}, fmt.Errorf(
			"sync state %s is invalid: its binding does not match its ID; "+
				"remove it with `ocis sync state remove %s`",
			shortSyncStateID(key), key,
		)
	}
	return key, state, nil
}

func resolveSyncStateID(
	query string,
	options Options,
) (string, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	if len(query) < syncStateIDMinimumPrefix ||
		strings.Trim(query, "0123456789abcdef") != "" {
		return "", apperror.Wrap(
			apperror.KindUsage, "sync state",
			fmt.Errorf(
				"state ID must be at least %d hexadecimal characters",
				syncStateIDMinimumPrefix,
			),
		)
	}
	keys, err := options.SyncStates.Keys()
	if err != nil {
		return "", fmt.Errorf("list sync state: %w", err)
	}
	matches := make([]string, 0, 1)
	for _, key := range keys {
		if strings.HasPrefix(key, query) {
			matches = append(matches, key)
		}
	}
	switch len(matches) {
	case 0:
		return "", syncStateNotFound(query)
	case 1:
		return matches[0], nil
	default:
		return "", apperror.Wrap(
			apperror.KindConflict, "sync state",
			fmt.Errorf(
				"state ID prefix %q is ambiguous; provide more characters",
				query,
			),
		)
	}
}

func syncStateSummaryFor(
	key string,
	state syncmodel.State,
	loadErr error,
) syncStateSummary {
	summary := syncStateSummary{ID: key, Status: "valid"}
	switch {
	case loadErr != nil:
		summary.Status = "invalid"
		summary.Error = loadErr.Error()
	case state.Binding.Key() != key:
		summary.Status = "invalid"
		summary.Error = "binding does not match state ID"
	default:
		summary.Version = state.Version
		summary.Profile = state.Binding.Profile
		summary.SpaceID = state.Binding.SpaceID
		summary.Direction = state.Binding.Direction
		summary.LocalRoot = state.Binding.LocalRoot
		summary.RemoteRoot = state.Binding.RemoteRoot
		summary.Includes = state.Binding.Includes
		summary.Excludes = state.Binding.Excludes
		summary.Entries = len(state.Entries)
	}
	return summary
}

func shortSyncStateID(key string) string {
	if len(key) <= syncStateIDDisplayLength {
		return key
	}
	return key[:syncStateIDDisplayLength]
}

func uniqueSyncStateID(key string, keys []string) string {
	for length := min(syncStateIDDisplayLength, len(key)); length < len(key); length++ {
		prefix := key[:length]
		matches := 0
		for _, candidate := range keys {
			if strings.HasPrefix(candidate, prefix) {
				matches++
			}
		}
		if matches == 1 {
			return prefix
		}
	}
	return key
}

func syncStateNotFound(query string) error {
	return apperror.Wrap(
		apperror.KindNotFound, "sync state",
		fmt.Errorf("no synchronization state matches %q", query),
	)
}
