package app

import (
	"bytes"
	"context"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
	syncmodel "github.com/mzner/ocis-cli/internal/sync"
	"github.com/mzner/ocis-cli/internal/syncjob"
)

func TestSyncJobLifecycleAndExecution(t *testing.T) {
	dav := newSyncDAV()
	server := httptest.NewServer(dav)
	defer server.Close()
	states := &memorySyncStates{states: map[string]syncmodel.State{}}
	jobs := &memorySyncJobs{store: syncjob.Empty()}
	dependencies := syncTestDependencies(server.URL, states)
	dependencies.SyncJobs = jobs

	local := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(local, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(local, "report.txt"), []byte("named job\n"), 0600,
	); err != nil {
		t.Fatal(err)
	}
	add := SyncJobRequest{
		Operation: SyncJobAdd, Name: "website", Profile: "work",
		Direction: SyncPush, LocalRoot: local, RemoteRoot: "/job",
		Includes:          []string{"*.txt"},
		Excludes:          []string{"*.tmp", "*.bak", "*.tmp"},
		DeleteDestination: true,
		MaxEntries:        100,
	}
	if err := RunSyncJobWithOptions(
		context.Background(), add,
		RunOptions{Out: io.Discard, Dependencies: dependencies},
	); err != nil {
		t.Fatal(err)
	}
	saved := jobs.store.Jobs["website"]
	if jobs.saves != 1 || saved.Profile != "work" ||
		saved.AccountID == "" || saved.LocalRoot != local ||
		!saved.Delete || saved.SpaceID != "" ||
		strings.Join(saved.Excludes, ",") != "*.bak,*.tmp" {
		t.Fatalf("saved job=%#v saves=%d", saved, jobs.saves)
	}

	var listed bytes.Buffer
	if err := RunSyncJobWithOptions(
		context.Background(),
		SyncJobRequest{Operation: SyncJobList, Profile: "work"},
		RunOptions{
			Out: &listed, OutputMode: appoutput.JSON,
			Dependencies: dependencies,
		},
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listed.String(), `"name": "website"`) {
		t.Fatalf("list output: %s", listed.String())
	}
	var humanList bytes.Buffer
	if err := RunSyncJobWithOptions(
		context.Background(),
		SyncJobRequest{Operation: SyncJobList},
		RunOptions{Out: &humanList, Dependencies: dependencies},
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(humanList.String(), "website") ||
		!strings.Contains(humanList.String(), local+" -> /job") {
		t.Fatalf("human list: %s", humanList.String())
	}
	var emptyList bytes.Buffer
	if err := RunSyncJobWithOptions(
		context.Background(),
		SyncJobRequest{Operation: SyncJobList, Profile: "missing"},
		RunOptions{Out: &emptyList, Dependencies: dependencies},
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(emptyList.String(), "No synchronization jobs") {
		t.Fatalf("empty list: %s", emptyList.String())
	}
	var shown bytes.Buffer
	if err := RunSyncJobWithOptions(
		context.Background(),
		SyncJobRequest{Operation: SyncJobShow, Name: "website"},
		RunOptions{Out: &shown, Dependencies: dependencies},
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(shown.String(), "Direction: push") ||
		!strings.Contains(shown.String(), "Includes: [*.txt]") ||
		!strings.Contains(shown.String(), "Excludes: [*.bak *.tmp]") {
		t.Fatalf("show output: %s", shown.String())
	}

	if err := RunSyncJobWithOptions(
		context.Background(),
		SyncJobRequest{
			Operation: SyncJobRun, Name: "website", DryRun: true,
		},
		RunOptions{Out: io.Discard, Quiet: true, Dependencies: dependencies},
	); err != nil {
		t.Fatal(err)
	}
	if dav.content("/job/report.txt") != "" || states.saves != 0 {
		t.Fatal("dry-run changed files or state")
	}
	if err := RunSyncJobWithOptions(
		context.Background(),
		SyncJobRequest{Operation: SyncJobRun, Name: "website"},
		RunOptions{Out: io.Discard, Quiet: true, Dependencies: dependencies},
	); err != nil {
		t.Fatal(err)
	}
	if got := dav.content("/job/report.txt"); got != "named job\n" {
		t.Fatalf("remote content=%q", got)
	}
	if states.saves != 1 {
		t.Fatalf("state saves=%d", states.saves)
	}
	for _, state := range states.states {
		if strings.Join(state.Binding.Includes, ",") != "*.txt" {
			t.Fatalf("state binding filters=%#v", state.Binding.Includes)
		}
		if strings.Join(state.Binding.Excludes, ",") != "*.bak,*.tmp" {
			t.Fatalf("state binding filters=%#v", state.Binding.Excludes)
		}
	}
	err := RunSyncJobWithOptions(
		context.Background(), add,
		RunOptions{Out: io.Discard, Dependencies: dependencies},
	)
	if !apperror.IsKind(err, apperror.KindConflict) {
		t.Fatalf("duplicate job error: %v", err)
	}

	err = RunSyncJobWithOptions(
		context.Background(),
		SyncJobRequest{Operation: SyncJobRemove, Name: "website"},
		RunOptions{Out: io.Discard, Dependencies: dependencies},
	)
	if !apperror.IsKind(err, apperror.KindUsage) {
		t.Fatalf("unconfirmed removal error: %v", err)
	}
	if err := RunSyncJobWithOptions(
		context.Background(),
		SyncJobRequest{
			Operation: SyncJobRemove, Name: "website", DryRun: true,
		},
		RunOptions{Out: io.Discard, Dependencies: dependencies},
	); err != nil {
		t.Fatal(err)
	}
	if jobs.saves != 1 {
		t.Fatal("dry-run saved job store")
	}
	if err := RunSyncJobWithOptions(
		context.Background(),
		SyncJobRequest{
			Operation: SyncJobRemove, Name: "website", Confirmed: true,
		},
		RunOptions{Out: io.Discard, Dependencies: dependencies},
	); err != nil {
		t.Fatal(err)
	}
	if _, found := jobs.store.Jobs["website"]; found || jobs.saves != 2 {
		t.Fatalf("jobs=%#v saves=%d", jobs.store.Jobs, jobs.saves)
	}
}

func TestSyncBidirectionalJobExecution(t *testing.T) {
	dav := newSyncDAV()
	dav.nodes["/job"] = &syncDAVNode{directory: true}
	dav.put("/job/remote.txt", "remote")
	server := httptest.NewServer(dav)
	defer server.Close()
	states := &memorySyncStates{states: map[string]syncmodel.State{}}
	jobs := &memorySyncJobs{store: syncjob.Empty()}
	dependencies := syncTestDependencies(server.URL, states)
	dependencies.SyncJobs = jobs
	local := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(local, "local.txt"), []byte("local"), 0600,
	); err != nil {
		t.Fatal(err)
	}
	if err := RunSyncJobWithOptions(
		context.Background(), SyncJobRequest{
			Operation: SyncJobAdd, Name: "two-way", Profile: "work",
			Direction: SyncBidirectional, LocalRoot: local,
			RemoteRoot: "/job", MaxEntries: 100,
		}, RunOptions{Out: io.Discard, Dependencies: dependencies},
	); err != nil {
		t.Fatal(err)
	}
	saved := jobs.store.Jobs["two-way"]
	if saved.Direction != syncmodel.Bidirectional ||
		saved.LocalRoot != local || saved.RemoteRoot != "/job" {
		t.Fatalf("saved bidirectional job=%#v", jobs.store.Jobs["two-way"])
	}
	if err := RunSyncJobWithOptions(
		context.Background(), SyncJobRequest{
			Operation: SyncJobRun, Name: "two-way",
		}, RunOptions{
			Out: io.Discard, Err: io.Discard, Quiet: true,
			Dependencies: dependencies,
		},
	); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, filepath.Join(local, "remote.txt"), "remote")
	if dav.content("/job/local.txt") != "local" || states.saves != 1 {
		t.Fatalf("remote=%q state saves=%d", dav.content("/job/local.txt"), states.saves)
	}
}

func TestSyncJobBindingAndValidation(t *testing.T) {
	dav := newSyncDAV()
	server := httptest.NewServer(dav)
	defer server.Close()
	states := &memorySyncStates{states: map[string]syncmodel.State{}}
	jobs := &memorySyncJobs{store: syncjob.Empty()}
	dependencies := syncTestDependencies(server.URL, states)
	dependencies.SyncJobs = jobs
	local := t.TempDir()

	add := SyncJobRequest{
		Operation: SyncJobAdd, Name: "bound", Profile: "work",
		Direction: SyncPush, LocalRoot: local, RemoteRoot: "/bound",
		MaxEntries: 100,
	}
	if err := RunSyncJobWithOptions(
		context.Background(), add,
		RunOptions{Out: io.Discard, Dependencies: dependencies},
	); err != nil {
		t.Fatal(err)
	}
	err := RunSyncJobWithOptions(
		context.Background(),
		SyncJobRequest{
			Operation: SyncJobRun, Name: "bound", Profile: "other",
		},
		RunOptions{Out: io.Discard, Dependencies: dependencies},
	)
	if !apperror.IsKind(err, apperror.KindUsage) {
		t.Fatalf("profile override error: %v", err)
	}
	err = RunSyncJobWithOptions(
		context.Background(),
		SyncJobRequest{
			Operation: SyncJobRun, Name: "bound", Space: "other",
		},
		RunOptions{Out: io.Discard, Dependencies: dependencies},
	)
	if !apperror.IsKind(err, apperror.KindUsage) {
		t.Fatalf("Space override error: %v", err)
	}

	configRepository := dependencies.Config.(*memoryConfig)
	profile := configRepository.store.Profiles["work"]
	profile.Username = "bob"
	configRepository.store.Profiles["work"] = profile
	err = RunSyncJobWithOptions(
		context.Background(),
		SyncJobRequest{Operation: SyncJobRun, Name: "bound"},
		RunOptions{Out: io.Discard, Dependencies: dependencies},
	)
	if !apperror.IsKind(err, apperror.KindAuthentication) {
		t.Fatalf("account binding error: %v", err)
	}
	if states.saves != 0 {
		t.Fatal("identity mismatch advanced sync state")
	}

	add.Name = "bad/name"
	err = RunSyncJobWithOptions(
		context.Background(), add,
		RunOptions{Out: io.Discard, Dependencies: dependencies},
	)
	if !apperror.IsKind(err, apperror.KindUsage) {
		t.Fatalf("invalid-name error: %v", err)
	}
	add.Name = "patterns"
	add.Excludes = []string{"["}
	err = RunSyncJobWithOptions(
		context.Background(), add,
		RunOptions{Out: io.Discard, Dependencies: dependencies},
	)
	if !apperror.IsKind(err, apperror.KindUsage) {
		t.Fatalf("invalid-pattern error: %v", err)
	}
	err = RunSyncJobWithOptions(
		context.Background(),
		SyncJobRequest{Operation: SyncJobShow, Name: "missing"},
		RunOptions{Out: io.Discard, Dependencies: dependencies},
	)
	if !apperror.IsKind(err, apperror.KindNotFound) {
		t.Fatalf("missing-job error: %v", err)
	}
}
