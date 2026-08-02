package app

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
	syncmodel "github.com/mzner/ocis-cli/internal/sync"
	"github.com/mzner/ocis-cli/internal/syncrecovery"
)

func TestSyncRecoveryInspectionAndRemoval(t *testing.T) {
	repository := &memorySyncRecoveries{
		journals: map[string]syncrecovery.Journal{},
	}
	options := RunOptions{
		Out:          io.Discard,
		Dependencies: Dependencies{SyncRecoveries: repository},
	}
	var empty bytes.Buffer
	options.Out = &empty
	if err := RunSyncRecoveryWithOptions(
		context.Background(), SyncRecoveryRequest{
			Operation: SyncRecoveryList,
		}, options,
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(empty.String(), "No synchronization recovery") {
		t.Fatalf("empty output=%q", empty.String())
	}

	binding := syncmodel.Binding{
		Profile: "work", AccountID: "account", Direction: syncmodel.Bidirectional,
		LocalRoot: "/local", RemoteRoot: "/remote",
	}
	journal := syncrecovery.New(
		binding, 100,
		syncmodel.Plan{
			Direction: syncmodel.Bidirectional,
			Actions: []syncmodel.Action{{
				Action: syncmodel.ActionTransfer, Path: "report.txt",
				Target: syncmodel.Remote,
			}},
		},
		time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC),
	)
	journal.Status = syncrecovery.Failed
	journal.Failure = "re-scan before retrying"
	journal.Current = &journal.Plan.Actions[0]
	repository.journals[journal.ID] = journal

	var listed bytes.Buffer
	options.Out = &listed
	if err := RunSyncRecoveryWithOptions(
		context.Background(), SyncRecoveryRequest{
			Operation: SyncRecoveryList, Profile: "work",
		}, options,
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listed.String(), journal.ID[:12]) ||
		!strings.Contains(listed.String(), "failed") {
		t.Fatalf("list output=%q", listed.String())
	}

	var shown bytes.Buffer
	options.Out = &shown
	if err := RunSyncRecoveryWithOptions(
		context.Background(), SyncRecoveryRequest{
			Operation: SyncRecoveryShow, ID: journal.ID[:12],
		}, options,
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(shown.String(), "Uncertain action") ||
		!strings.Contains(shown.String(), journal.Failure) {
		t.Fatalf("show output=%q", shown.String())
	}

	options.Out = io.Discard
	err := RunSyncRecoveryWithOptions(
		context.Background(), SyncRecoveryRequest{
			Operation: SyncRecoveryRemove, ID: journal.ID[:12],
		}, options,
	)
	if !apperror.IsKind(err, apperror.KindUsage) {
		t.Fatalf("unconfirmed removal error=%v", err)
	}
	if err := RunSyncRecoveryWithOptions(
		context.Background(), SyncRecoveryRequest{
			Operation: SyncRecoveryRemove, ID: journal.ID[:12], DryRun: true,
		}, options,
	); err != nil {
		t.Fatal(err)
	}
	if repository.deletes != 0 {
		t.Fatal("dry-run removed recovery journal")
	}
	if err := RunSyncRecoveryWithOptions(
		context.Background(), SyncRecoveryRequest{
			Operation: SyncRecoveryRemove, ID: journal.ID[:12], Confirmed: true,
		}, options,
	); err != nil {
		t.Fatal(err)
	}
	if repository.deletes != 1 || len(repository.journals) != 0 {
		t.Fatalf("repository=%#v", repository)
	}
}

func TestSyncRecoveryMachineOutputAndValidation(t *testing.T) {
	binding := syncmodel.Binding{
		Profile: "work", AccountID: "account", Direction: syncmodel.Bidirectional,
		LocalRoot: "/local", RemoteRoot: "/remote",
	}
	journal := syncrecovery.New(
		binding, 10, syncmodel.Plan{Direction: syncmodel.Bidirectional}, time.Now(),
	)
	repository := &memorySyncRecoveries{
		journals: map[string]syncrecovery.Journal{journal.ID: journal},
	}
	var output bytes.Buffer
	options := RunOptions{
		Out: &output, OutputMode: appoutput.JSON,
		Dependencies: Dependencies{SyncRecoveries: repository},
	}
	if err := RunSyncRecoveryWithOptions(
		context.Background(), SyncRecoveryRequest{
			Operation: SyncRecoveryShow, ID: journal.ID,
		}, options,
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"type": "sync-recovery"`) {
		t.Fatalf("machine output=%s", output.String())
	}
	for _, request := range []SyncRecoveryRequest{
		{Operation: SyncRecoveryShow, ID: "short"},
		{Operation: SyncRecoveryShow, ID: strings.Repeat("f", 8)},
		{Operation: SyncRecoveryOperation("future")},
	} {
		err := RunSyncRecoveryWithOptions(
			context.Background(), request,
			RunOptions{Out: io.Discard, Dependencies: options.Dependencies},
		)
		if err == nil {
			t.Fatalf("request %#v unexpectedly succeeded", request)
		}
	}
}
