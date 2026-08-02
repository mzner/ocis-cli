package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
	syncmodel "github.com/mzner/ocis-cli/internal/sync"
)

func TestSyncStateListShowAndExport(t *testing.T) {
	state := syncStateFixture("work")
	key := state.Binding.Key()
	invalidKey := strings.Repeat("f", 64)
	repository := &memorySyncStates{
		states: map[string]syncmodel.State{key: state},
		loadErrors: map[string]error{
			invalidKey: errors.New("decode sync state: invalid JSON"),
		},
	}

	var human bytes.Buffer
	if err := RunSyncStateWithOptions(
		context.Background(),
		SyncStateRequest{Operation: SyncStateList},
		RunOptions{
			Out:          &human,
			Dependencies: Dependencies{SyncStates: repository},
		},
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(human.String(), key[:12]) ||
		!strings.Contains(human.String(), "invalid JSON") {
		t.Fatalf("list output:\n%s", human.String())
	}

	var filtered bytes.Buffer
	if err := RunSyncStateWithOptions(
		context.Background(),
		SyncStateRequest{
			Operation: SyncStateList, Profile: "another-profile",
		},
		RunOptions{
			Out: &filtered, OutputMode: appoutput.JSON,
			Dependencies: Dependencies{SyncStates: repository},
		},
	); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(filtered.String(), `"profile": "work"`) ||
		!strings.Contains(filtered.String(), `"status": "invalid"`) {
		t.Fatalf("filtered list: %s", filtered.String())
	}

	var shown bytes.Buffer
	if err := RunSyncStateWithOptions(
		context.Background(),
		SyncStateRequest{Operation: SyncStateShow, ID: key[:12]},
		RunOptions{
			Out: &shown, OutputMode: appoutput.JSON,
			Dependencies: Dependencies{SyncStates: repository},
		},
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(shown.String(), `"id": "`+key+`"`) ||
		!strings.Contains(shown.String(), `"entries": 1`) {
		t.Fatalf("show output: %s", shown.String())
	}
	var humanShow bytes.Buffer
	if err := RunSyncStateWithOptions(
		context.Background(),
		SyncStateRequest{Operation: SyncStateShow, ID: key[:12]},
		RunOptions{
			Out:          &humanShow,
			Dependencies: Dependencies{SyncStates: repository},
		},
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(humanShow.String(), "Includes: [*.txt]") ||
		!strings.Contains(humanShow.String(), "Excludes: [*.tmp]") {
		t.Fatalf("human show: %s", humanShow.String())
	}

	var exported bytes.Buffer
	if err := RunSyncStateWithOptions(
		context.Background(),
		SyncStateRequest{Operation: SyncStateExport, ID: key},
		RunOptions{
			Out:          &exported,
			Dependencies: Dependencies{SyncStates: repository},
		},
	); err != nil {
		t.Fatal(err)
	}
	var document syncStateExportDocument
	if err := json.Unmarshal(exported.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if document.SchemaVersion != "1" || document.ID != key ||
		document.State.Binding.Profile != "work" {
		t.Fatalf("export: %#v", document)
	}
}

func TestSyncStateRemovalAndResolution(t *testing.T) {
	state := syncStateFixture("work")
	key := state.Binding.Key()
	repository := &memorySyncStates{
		states: map[string]syncmodel.State{key: state},
	}
	options := RunOptions{
		Out:          io.Discard,
		Dependencies: Dependencies{SyncStates: repository},
	}

	err := RunSyncStateWithOptions(
		context.Background(),
		SyncStateRequest{Operation: SyncStateRemove, ID: key},
		options,
	)
	if !apperror.IsKind(err, apperror.KindUsage) {
		t.Fatalf("unconfirmed error: %v", err)
	}
	if err := RunSyncStateWithOptions(
		context.Background(),
		SyncStateRequest{
			Operation: SyncStateRemove, ID: key[:12], DryRun: true,
		},
		options,
	); err != nil {
		t.Fatal(err)
	}
	if repository.deletes != 0 {
		t.Fatal("dry-run deleted state")
	}
	if err := RunSyncStateWithOptions(
		context.Background(),
		SyncStateRequest{
			Operation: SyncStateRemove, ID: key[:12], Confirmed: true,
		},
		options,
	); err != nil {
		t.Fatal(err)
	}
	if repository.deletes != 1 {
		t.Fatalf("deletes=%d", repository.deletes)
	}
	err = RunSyncStateWithOptions(
		context.Background(),
		SyncStateRequest{
			Operation: SyncStateShow, ID: key[:12],
		},
		options,
	)
	if !apperror.IsKind(err, apperror.KindNotFound) {
		t.Fatalf("not-found error: %v", err)
	}
}

func TestSyncStateInvalidAmbiguousAndCanceled(t *testing.T) {
	invalidKey := strings.Repeat("e", 64)
	repository := &memorySyncStates{
		states: map[string]syncmodel.State{
			"abcdef12" + strings.Repeat("0", 56): {},
			"abcdef12" + strings.Repeat("1", 56): {},
		},
		loadErrors: map[string]error{
			invalidKey: errors.New("broken state"),
		},
	}
	options := RunOptions{
		Out:          io.Discard,
		Dependencies: Dependencies{SyncStates: repository},
	}
	err := RunSyncStateWithOptions(
		context.Background(),
		SyncStateRequest{Operation: SyncStateShow, ID: "abcdef12"},
		options,
	)
	if !apperror.IsKind(err, apperror.KindConflict) {
		t.Fatalf("ambiguous error: %v", err)
	}
	err = RunSyncStateWithOptions(
		context.Background(),
		SyncStateRequest{Operation: SyncStateShow, ID: "short"},
		options,
	)
	if !apperror.IsKind(err, apperror.KindUsage) {
		t.Fatalf("short-ID error: %v", err)
	}
	err = RunSyncStateWithOptions(
		context.Background(),
		SyncStateRequest{Operation: SyncStateShow, ID: invalidKey},
		options,
	)
	if err == nil || !strings.Contains(err.Error(), "unreadable") {
		t.Fatalf("invalid-state error: %v", err)
	}
	if err := RunSyncStateWithOptions(
		context.Background(),
		SyncStateRequest{
			Operation: SyncStateRemove, ID: invalidKey, Confirmed: true,
		},
		options,
	); err != nil {
		t.Fatal(err)
	}
	if repository.deletes != 1 {
		t.Fatalf("invalid state was not removed: %d", repository.deletes)
	}
	err = RunSyncStateWithOptions(
		context.Background(),
		SyncStateRequest{Operation: SyncStateExport, ID: "abcdef120"},
		RunOptions{
			Out: io.Discard, OutputMode: appoutput.JSON,
			Dependencies: Dependencies{SyncStates: repository},
		},
	)
	if !apperror.IsKind(err, apperror.KindUsage) {
		t.Fatalf("machine export error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = RunSyncStateWithOptions(
		ctx, SyncStateRequest{Operation: SyncStateList}, options,
	)
	if !apperror.IsKind(err, apperror.KindCanceled) {
		t.Fatalf("cancellation error: %v", err)
	}
}

func TestUniqueSyncStateID(t *testing.T) {
	first := "123456789012a" + strings.Repeat("0", 51)
	second := "123456789012b" + strings.Repeat("0", 51)
	if got := uniqueSyncStateID(first, []string{first, second}); got != first[:13] {
		t.Fatalf("unique ID=%q", got)
	}
	if got := uniqueSyncStateID(first, []string{first}); got != first[:12] {
		t.Fatalf("single ID=%q", got)
	}
}

func syncStateFixture(profile string) syncmodel.State {
	binding := syncmodel.Binding{
		Profile: profile, AccountID: "v1:account", SpaceID: "space",
		Direction: syncmodel.Push,
		LocalRoot: "/local", RemoteRoot: "/remote",
		Includes: []string{"*.txt"}, Excludes: []string{"*.tmp"},
	}
	return syncmodel.NewState(
		binding,
		syncmodel.Snapshot{
			"": {Path: "", Type: "directory"},
		},
		syncmodel.Snapshot{
			"": {Path: "", Type: "directory"},
		},
	)
}
