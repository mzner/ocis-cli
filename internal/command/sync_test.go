package command

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	syncmodel "github.com/mzner/ocis-cli/internal/sync"
	"github.com/mzner/ocis-cli/internal/syncjob"
	"github.com/mzner/ocis-cli/internal/syncrecovery"
	"github.com/mzner/ocis-cli/internal/syncstate"
)

func TestSyncCommandsAreDiscoverable(t *testing.T) {
	tests := []struct {
		args     []string
		expected []string
	}{
		{[]string{"sync", "--help"}, []string{
			"bidirectional, bi", "job", "push", "pull", "recovery, recover", "state",
		}},
		{
			[]string{"sync", "push", "--help"},
			[]string{
				"--delete", "--overwrite", "--dry-run", "--include",
				"--exclude", "--max-entries",
			},
		},
		{
			[]string{"sync", "pull", "--help"},
			[]string{
				"--delete", "--overwrite", "--dry-run", "--include",
				"--exclude", "--max-entries",
			},
		},
		{
			[]string{"sync", "bidirectional", "--help"},
			[]string{
				"--dry-run", "--include", "--exclude", "--max-entries",
				"--conflict-strategy", "--prefer",
			},
		},
		{
			[]string{"sync", "recovery", "--help"},
			[]string{
				"list, ls", "show, info, stat", "retry", "remove, rm, delete",
			},
		},
		{
			[]string{"sync", "recovery", "retry", "--help"},
			[]string{"--dry-run"},
		},
		{
			[]string{"sync", "recovery", "remove", "--help"},
			[]string{"--dry-run", "--yes"},
		},
		{
			[]string{"sync", "state", "--help"},
			[]string{
				"list, ls", "show, info, stat", "export",
				"remove, rm, delete",
			},
		},
		{
			[]string{"sync", "state", "remove", "--help"},
			[]string{"--dry-run", "--yes"},
		},
		{
			[]string{"sync", "job", "--help"},
			[]string{
				"add", "list, ls", "show, info", "run",
				"remove, rm, delete",
			},
		},
		{
			[]string{"sync", "job", "add", "--help"},
			[]string{
				"--direction", "--local", "--remote", "--delete",
				"--overwrite", "--include", "--exclude", "--max-entries",
			},
		},
		{
			[]string{"sync", "job", "run", "--help"},
			[]string{"--dry-run"},
		},
		{
			[]string{"sync", "job", "remove", "--help"},
			[]string{"--dry-run", "--yes"},
		},
	}
	for _, test := range tests {
		root := NewRootCommand()
		var output bytes.Buffer
		root.SetOut(&output)
		root.SetErr(&output)
		root.SetArgs(test.args)
		if err := root.Execute(); err != nil {
			t.Fatalf("%v: %v", test.args, err)
		}
		help := output.String()
		for _, expected := range test.expected {
			if !strings.Contains(help, expected) {
				t.Fatalf(
					"%v help missing %q:\n%s",
					test.args, expected, help,
				)
			}
		}
	}
}

func TestSyncRecoveryCommandsAndRemovalConfirmation(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("OCIS_SYNC_RECOVERY_DIR", directory)
	binding := syncmodel.Binding{
		Profile: "work", AccountID: "account", Direction: syncmodel.Bidirectional,
		LocalRoot: "/local", RemoteRoot: "/remote",
	}
	journal := syncrecovery.New(
		binding, 100,
		syncmodel.Plan{Direction: syncmodel.Bidirectional}, time.Now(),
	)
	journal.Status = syncrecovery.Failed
	journal.Failure = "re-scan before retrying"
	if err := syncrecovery.Save(journal); err != nil {
		t.Fatal(err)
	}

	root := NewRootCommand()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"sync", "recovery", "list"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), journal.ID[:12]) ||
		!strings.Contains(output.String(), "failed") {
		t.Fatalf("list output: %s", output.String())
	}

	root = NewRootCommand()
	root.SetIn(strings.NewReader("n\n"))
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"sync", "recovery", "remove", journal.ID})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, found, err := syncrecovery.Load(journal.ID); err != nil || !found {
		t.Fatalf("cancelled removal found=%t err=%v", found, err)
	}

	root = NewRootCommand()
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{
		"sync", "recovery", "remove", journal.ID, "--yes",
	})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, found, err := syncrecovery.Load(journal.ID); err != nil || found {
		t.Fatalf("confirmed removal found=%t err=%v", found, err)
	}
}

func TestSyncArgumentValidation(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{"sync", "push", "./only-local"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "accepts 2 arg") {
		t.Fatalf("error: %v", err)
	}
	root = NewRootCommand()
	root.SetArgs([]string{"sync", "job", "add", "website"})
	err = root.Execute()
	if err == nil || !strings.Contains(err.Error(), "required flag") {
		t.Fatalf("required-flags error: %v", err)
	}
	root = NewRootCommand()
	root.SetArgs([]string{
		"sync", "bi", "./local", "/remote", "--max-entries", "0",
	})
	err = root.Execute()
	if err == nil || !strings.Contains(err.Error(), "--max-entries") {
		t.Fatalf("bidirectional entry-limit error: %v", err)
	}
	command, _, err := NewRootCommand().Find([]string{"sync", "bi"})
	if err != nil || command.Name() != "bidirectional" {
		t.Fatalf("bidirectional alias: command=%v err=%v", command, err)
	}
}

func TestSyncStateCommandsAndRemovalConfirmation(t *testing.T) {
	t.Setenv("OCIS_STATE_DIR", t.TempDir())
	binding := syncmodel.Binding{
		Profile: "work", AccountID: "account", Direction: syncmodel.Push,
		LocalRoot: "/local", RemoteRoot: "/remote",
	}
	state := syncmodel.NewState(
		binding,
		syncmodel.Snapshot{
			"": {Path: "", Type: "directory"},
		},
		syncmodel.Snapshot{
			"": {Path: "", Type: "directory"},
		},
	)
	key := binding.Key()
	if err := syncstate.Save(key, state); err != nil {
		t.Fatal(err)
	}

	root := NewRootCommand()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"sync", "state", "list"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), key[:12]) {
		t.Fatalf("list output: %s", output.String())
	}

	root = NewRootCommand()
	root.SetIn(strings.NewReader("n\n"))
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"sync", "state", "remove", key[:12]})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, found, err := syncstate.Load(key); err != nil || !found {
		t.Fatalf("cancelled removal found=%t err=%v", found, err)
	}

	root = NewRootCommand()
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{
		"sync", "state", "remove", key[:12], "--yes",
	})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, found, err := syncstate.Load(key); err != nil || found {
		t.Fatalf("confirmed removal found=%t err=%v", found, err)
	}
}

func TestSyncStateExportRejectsMachineEnvelopeFlags(t *testing.T) {
	root := NewRootCommand()
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{
		"--json", "sync", "state", "export", strings.Repeat("a", 12),
	})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "already writes JSON") {
		t.Fatalf("error: %v", err)
	}
}

func TestSyncJobCommandsAndRemovalConfirmation(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "sync-jobs.json")
	t.Setenv("OCIS_SYNC_JOBS", storePath)
	store := syncjob.Empty()
	store.Jobs["website"] = syncjob.Job{
		Name: "website", Profile: "work", AccountID: "account",
		Direction: syncmodel.Push, LocalRoot: "/local",
		RemoteRoot: "/remote", MaxEntries: 100,
	}
	if err := syncjob.Save(store); err != nil {
		t.Fatal(err)
	}

	root := NewRootCommand()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"sync", "job", "list"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "website") {
		t.Fatalf("list output: %s", output.String())
	}

	root = NewRootCommand()
	root.SetIn(strings.NewReader("n\n"))
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"sync", "job", "remove", "website"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	loaded, err := syncjob.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, found := loaded.Jobs["website"]; !found {
		t.Fatal("cancelled removal deleted job")
	}

	root = NewRootCommand()
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{
		"sync", "job", "remove", "website", "--yes",
	})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	loaded, err = syncjob.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, found := loaded.Jobs["website"]; found {
		t.Fatal("confirmed removal kept job")
	}
}
