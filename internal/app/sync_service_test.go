package app

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mzner/ocis-cli/internal/apperror"
	appconfig "github.com/mzner/ocis-cli/internal/config"
	"github.com/mzner/ocis-cli/internal/credentials"
	appoutput "github.com/mzner/ocis-cli/internal/output"
	syncmodel "github.com/mzner/ocis-cli/internal/sync"
	"github.com/mzner/ocis-cli/internal/syncrecovery"
)

func TestSyncPushPullStateAndConflict(t *testing.T) {
	dav := newSyncDAV()
	server := httptest.NewServer(dav)
	defer server.Close()
	states := &memorySyncStates{
		states: map[string]syncmodel.State{},
	}
	dependencies := syncTestDependencies(server.URL, states)

	local := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(filepath.Join(local, "docs"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(local, "docs", "report.txt"),
		[]byte("first"), 0600,
	); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	request := SyncRequest{
		Direction: SyncPush, LocalRoot: local, RemoteRoot: "/sync",
		MaxEntries: 100,
	}
	if err := RunSyncWithOptions(
		context.Background(), request, "work",
		RunOptions{
			Out: &output, Err: io.Discard, Quiet: true,
			Dependencies: dependencies,
		},
	); err != nil {
		t.Fatal(err)
	}
	if got := dav.content("/sync/docs/report.txt"); got != "first" {
		t.Fatalf("remote content: %q", got)
	}
	if states.saves != 1 || !strings.Contains(output.String(), "applied:") {
		t.Fatalf("states=%d output=%q", states.saves, output.String())
	}

	pullDestination := filepath.Join(t.TempDir(), "pulled")
	if err := RunSyncWithOptions(
		context.Background(),
		SyncRequest{
			Direction: SyncPull, RemoteRoot: "/sync",
			LocalRoot: pullDestination, MaxEntries: 100,
		},
		"work",
		RunOptions{
			Out: io.Discard, Err: io.Discard, Quiet: true,
			Dependencies: dependencies,
		},
	); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(
		filepath.Join(pullDestination, "docs", "report.txt"),
	)
	if err != nil || string(data) != "first" {
		t.Fatalf("pulled=%q err=%v", data, err)
	}

	if err := os.WriteFile(
		filepath.Join(local, "docs", "report.txt"),
		[]byte("local-change"), 0600,
	); err != nil {
		t.Fatal(err)
	}
	dav.put("/sync/docs/report.txt", "remote-change")
	err = RunSyncWithOptions(
		context.Background(), request, "work",
		RunOptions{
			Out: io.Discard, Err: io.Discard, Quiet: true,
			Dependencies: dependencies,
		},
	)
	if !apperror.IsKind(err, apperror.KindConflict) {
		t.Fatalf("conflict error: %v", err)
	}
	if got := dav.content("/sync/docs/report.txt"); got != "remote-change" {
		t.Fatalf("conflict mutated remote content: %q", got)
	}

	var plan bytes.Buffer
	request.DryRun = true
	if err := RunSyncWithOptions(
		context.Background(), request, "work",
		RunOptions{
			Out: &plan, Err: io.Discard, Quiet: true,
			OutputMode: appoutput.JSON, Dependencies: dependencies,
		},
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.String(), `"action": "conflict"`) ||
		!strings.Contains(plan.String(), `"dryRun": true`) {
		t.Fatalf("dry-run plan: %s", plan.String())
	}

	request.DryRun = false
	request.Overwrite = true
	if err := RunSyncWithOptions(
		context.Background(), request, "work",
		RunOptions{
			Out: io.Discard, Err: io.Discard, Quiet: true,
			Dependencies: dependencies,
		},
	); err != nil {
		t.Fatal(err)
	}
	if got := dav.content("/sync/docs/report.txt"); got != "local-change" {
		t.Fatalf("overwrite content: %q", got)
	}
}

func TestBidirectionalSyncDryRunPlansBothDirectionsWithoutMutation(
	t *testing.T,
) {
	dav := newSyncDAV()
	dav.nodes["/sync"] = &syncDAVNode{directory: true}
	dav.put("/sync/remote.txt", "remote")
	dav.put("/sync/conflict.txt", "remote-conflict")
	server := httptest.NewServer(dav)
	defer server.Close()
	states := &memorySyncStates{states: map[string]syncmodel.State{}}

	local := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(local, "local.txt"), []byte("local"), 0600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(local, "conflict.txt"),
		[]byte("local-conflict"), 0600,
	); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err := RunSyncWithOptions(
		context.Background(),
		SyncRequest{
			Direction: SyncBidirectional,
			LocalRoot: local, RemoteRoot: "/sync",
			DryRun: true, MaxEntries: 100,
		},
		"work",
		RunOptions{
			Out: &output, Err: io.Discard, Quiet: true,
			OutputMode:   appoutput.JSON,
			Dependencies: syncTestDependencies(server.URL, states),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	rendered := output.String()
	for _, expected := range []string{
		`"direction": "bidirectional"`,
		`"path": "local.txt"`, `"target": "remote"`,
		`"path": "remote.txt"`, `"target": "local"`,
		`"path": "conflict.txt"`, `"action": "conflict"`,
		`"dryRun": true`, `"applied": false`,
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("plan does not contain %q:\n%s", expected, rendered)
		}
	}
	if states.saves != 0 {
		t.Fatalf("dry-run saved state %d time(s)", states.saves)
	}
	if got := dav.content("/sync/local.txt"); got != "" {
		t.Fatalf("dry-run uploaded local file: %q", got)
	}
	if _, err := os.Stat(filepath.Join(local, "remote.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run downloaded remote file: %v", err)
	}
	if got := dav.content("/sync/conflict.txt"); got != "remote-conflict" {
		t.Fatalf("dry-run changed conflict: %q", got)
	}
}

func TestBidirectionalSyncExecutesBothDirectionsAndPersistsBaseline(
	t *testing.T,
) {
	dav := newSyncDAV()
	dav.nodes["/sync"] = &syncDAVNode{directory: true}
	dav.put("/sync/remote.txt", "remote-one")
	server := httptest.NewServer(dav)
	defer server.Close()
	states := &memorySyncStates{states: map[string]syncmodel.State{}}
	dependencies := syncTestDependencies(server.URL, states)

	local := t.TempDir()
	localFile := filepath.Join(local, "local.txt")
	remoteFile := filepath.Join(local, "remote.txt")
	if err := os.WriteFile(localFile, []byte("local-one"), 0600); err != nil {
		t.Fatal(err)
	}
	request := SyncRequest{
		Direction: SyncBidirectional,
		LocalRoot: local, RemoteRoot: "/sync", MaxEntries: 100,
	}
	var output bytes.Buffer
	if err := RunSyncWithOptions(
		context.Background(), request, "work",
		RunOptions{
			Out: &output, Err: io.Discard, Quiet: true,
			Dependencies: dependencies,
		},
	); err != nil {
		t.Fatal(err)
	}
	if got := dav.content("/sync/local.txt"); got != "local-one" {
		t.Fatalf("uploaded content=%q", got)
	}
	assertFileContent(t, remoteFile, "remote-one")
	if states.saves != 1 || len(states.states) != 1 ||
		!strings.Contains(output.String(), "applied:") {
		t.Fatalf(
			"state saves=%d states=%#v output=%q",
			states.saves, states.states, output.String(),
		)
	}
	for _, state := range states.states {
		if state.Binding.Direction != syncmodel.Bidirectional {
			t.Fatalf("saved direction=%q", state.Binding.Direction)
		}
	}

	if err := os.WriteFile(localFile, []byte("local-two"), 0600); err != nil {
		t.Fatal(err)
	}
	dav.put("/sync/remote.txt", "remote-two")
	if err := RunSyncWithOptions(
		context.Background(), request, "work",
		RunOptions{
			Out: io.Discard, Err: io.Discard, Quiet: true,
			Dependencies: dependencies,
		},
	); err != nil {
		t.Fatal(err)
	}
	if got := dav.content("/sync/local.txt"); got != "local-two" {
		t.Fatalf("updated upload=%q", got)
	}
	assertFileContent(t, remoteFile, "remote-two")
	if states.saves != 2 {
		t.Fatalf("state saves=%d, want 2", states.saves)
	}

	if err := os.Remove(localFile); err != nil {
		t.Fatal(err)
	}
	dav.lock.Lock()
	delete(dav.nodes, "/sync/remote.txt")
	dav.lock.Unlock()
	if err := RunSyncWithOptions(
		context.Background(), request, "work",
		RunOptions{
			Out: io.Discard, Err: io.Discard, Quiet: true,
			Dependencies: dependencies,
		},
	); err != nil {
		t.Fatal(err)
	}
	if got := dav.content("/sync/local.txt"); got != "" {
		t.Fatalf("local tombstone did not remove remote file: %q", got)
	}
	if _, err := os.Stat(remoteFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("remote tombstone did not remove local file: %v", err)
	}
	if states.saves != 3 {
		t.Fatalf("state saves=%d, want 3", states.saves)
	}
}

func TestBidirectionalSyncConflictFailsBeforeMutation(t *testing.T) {
	dav := newSyncDAV()
	dav.nodes["/sync"] = &syncDAVNode{directory: true}
	dav.put("/sync/report.txt", "remote")
	server := httptest.NewServer(dav)
	defer server.Close()
	states := &memorySyncStates{states: map[string]syncmodel.State{}}
	local := t.TempDir()
	localFile := filepath.Join(local, "report.txt")
	if err := os.WriteFile(localFile, []byte("local"), 0600); err != nil {
		t.Fatal(err)
	}
	dependencies := syncTestDependencies(server.URL, states)
	recoveries := dependencies.SyncRecoveries.(*memorySyncRecoveries)
	err := RunSyncWithOptions(
		context.Background(), SyncRequest{
			Direction: SyncBidirectional,
			LocalRoot: local, RemoteRoot: "/sync", MaxEntries: 100,
		},
		"work", RunOptions{
			Out: io.Discard, Err: io.Discard, Quiet: true,
			Dependencies: dependencies,
		},
	)
	if !apperror.IsKind(err, apperror.KindConflict) ||
		!strings.Contains(err.Error(), "neither tree was changed") {
		t.Fatalf("conflict error=%v", err)
	}
	if got := dav.content("/sync/report.txt"); got != "remote" {
		t.Fatalf("remote conflict changed=%q", got)
	}
	assertFileContent(t, localFile, "local")
	if states.saves != 0 {
		t.Fatalf("conflict saved state %d time(s)", states.saves)
	}
	if len(recoveries.journals) != 1 {
		t.Fatalf("conflict recovery reports=%#v", recoveries.journals)
	}
	for _, journal := range recoveries.journals {
		if journal.Status != syncrecovery.Conflict || journal.Plan.Conflicts != 1 {
			t.Fatalf("conflict journal=%#v", journal)
		}
	}
}

func TestBidirectionalSyncPartialFailureDoesNotAdvanceState(t *testing.T) {
	dav := newSyncDAV()
	dav.nodes["/sync"] = &syncDAVNode{directory: true}
	dav.failMoveTargets["/sync/b.txt"] = true
	server := httptest.NewServer(dav)
	defer server.Close()
	states := &memorySyncStates{states: map[string]syncmodel.State{}}
	local := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(
			filepath.Join(local, name), []byte(name), 0600,
		); err != nil {
			t.Fatal(err)
		}
	}
	dependencies := syncTestDependencies(server.URL, states)
	recoveries := dependencies.SyncRecoveries.(*memorySyncRecoveries)
	err := RunSyncWithOptions(
		context.Background(), SyncRequest{
			Direction: SyncBidirectional,
			LocalRoot: local, RemoteRoot: "/sync", MaxEntries: 100,
		},
		"work", RunOptions{
			Out: io.Discard, Err: io.Discard, Quiet: true,
			Dependencies: dependencies,
		},
	)
	if err == nil {
		t.Fatal("partial transfer failure was ignored")
	}
	if got := dav.content("/sync/a.txt"); got != "a.txt" {
		t.Fatalf("first ordered action was not applied: %q", got)
	}
	if got := dav.content("/sync/b.txt"); got != "" {
		t.Fatalf("failed action unexpectedly completed: %q", got)
	}
	if states.saves != 0 || len(states.states) != 0 {
		t.Fatalf("partial failure advanced state: %#v", states)
	}
	keys, err := recoveries.Keys()
	if err != nil || len(keys) != 1 {
		t.Fatalf("recovery keys=%v err=%v", keys, err)
	}
	journal := recoveries.journals[keys[0]]
	if journal.Status != syncrecovery.Failed || len(journal.Completed) != 1 ||
		journal.Current == nil {
		t.Fatalf("partial failure journal=%#v", journal)
	}
	dav.failMoveTargets["/sync/b.txt"] = false
	if err := RunSyncRecoveryWithOptions(
		context.Background(), SyncRecoveryRequest{
			Operation: SyncRecoveryRetry, ID: journal.ID,
		}, RunOptions{
			Out: io.Discard, Err: io.Discard, Quiet: true,
			Dependencies: dependencies,
		},
	); err != nil {
		t.Fatal(err)
	}
	if dav.content("/sync/b.txt") != "b.txt" || len(recoveries.journals) != 0 ||
		states.saves != 1 {
		t.Fatalf(
			"retry content=%q recoveries=%#v state saves=%d",
			dav.content("/sync/b.txt"), recoveries.journals, states.saves,
		)
	}
}

func TestBidirectionalSyncExecutesUniqueRenamesBothDirections(t *testing.T) {
	dav := newSyncDAV()
	dav.nodes["/sync"] = &syncDAVNode{directory: true}
	server := httptest.NewServer(dav)
	defer server.Close()
	states := &memorySyncStates{states: map[string]syncmodel.State{}}
	dependencies := syncTestDependencies(server.URL, states)
	local := t.TempDir()
	oldLocal := filepath.Join(local, "old.txt")
	if err := os.WriteFile(oldLocal, []byte("rename-me"), 0600); err != nil {
		t.Fatal(err)
	}
	request := SyncRequest{
		Direction: SyncBidirectional, LocalRoot: local,
		RemoteRoot: "/sync", MaxEntries: 100,
	}
	if err := RunSyncWithOptions(
		context.Background(), request, "work",
		RunOptions{Out: io.Discard, Err: io.Discard, Quiet: true, Dependencies: dependencies},
	); err != nil {
		t.Fatal(err)
	}
	newLocal := filepath.Join(local, "new.txt")
	if err := os.Rename(oldLocal, newLocal); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RunSyncWithOptions(
		context.Background(), request, "work",
		RunOptions{
			Out: &output, Err: io.Discard, Quiet: true,
			OutputMode: appoutput.JSON, Dependencies: dependencies,
		},
	); err != nil {
		t.Fatal(err)
	}
	if dav.content("/sync/old.txt") != "" ||
		dav.content("/sync/new.txt") != "rename-me" ||
		!strings.Contains(output.String(), `"action": "move"`) {
		t.Fatalf("remote rename failed; output=%s nodes=%#v", output.String(), dav.nodes)
	}
	dav.lock.Lock()
	dav.nodes["/sync/final.txt"] = dav.nodes["/sync/new.txt"]
	delete(dav.nodes, "/sync/new.txt")
	dav.lock.Unlock()
	if err := RunSyncWithOptions(
		context.Background(), request, "work",
		RunOptions{Out: io.Discard, Err: io.Discard, Quiet: true, Dependencies: dependencies},
	); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, filepath.Join(local, "final.txt"), "rename-me")
	if _, err := os.Stat(newLocal); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old local rename path still exists: %v", err)
	}
}

func TestBidirectionalSyncKeepBothPreservesLosingVersion(t *testing.T) {
	for _, preferred := range []string{"local", "remote"} {
		t.Run(preferred, func(t *testing.T) {
			dav := newSyncDAV()
			dav.nodes["/sync"] = &syncDAVNode{directory: true}
			dav.put("/sync/report.txt", "base")
			server := httptest.NewServer(dav)
			defer server.Close()
			states := &memorySyncStates{states: map[string]syncmodel.State{}}
			dependencies := syncTestDependencies(server.URL, states)
			recoveries := dependencies.SyncRecoveries.(*memorySyncRecoveries)
			local := t.TempDir()
			file := filepath.Join(local, "report.txt")
			if err := os.WriteFile(file, []byte("base"), 0600); err != nil {
				t.Fatal(err)
			}
			request := SyncRequest{
				Direction: SyncBidirectional, LocalRoot: local,
				RemoteRoot: "/sync", MaxEntries: 100,
			}
			if err := RunSyncWithOptions(
				context.Background(), request, "work",
				RunOptions{Out: io.Discard, Err: io.Discard, Quiet: true, Dependencies: dependencies},
			); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte("local-version"), 0600); err != nil {
				t.Fatal(err)
			}
			dav.put("/sync/report.txt", "remote-version")
			request.ConflictStrategy = "keep-both"
			request.Prefer = preferred
			if err := RunSyncWithOptions(
				context.Background(), request, "work",
				RunOptions{Out: io.Discard, Err: io.Discard, Quiet: true, Dependencies: dependencies},
			); err != nil {
				t.Fatal(err)
			}
			primary := preferred + "-version"
			assertFileContent(t, file, primary)
			if dav.content("/sync/report.txt") != primary {
				t.Fatalf("remote primary=%q want=%q", dav.content("/sync/report.txt"), primary)
			}
			entries, err := os.ReadDir(local)
			if err != nil {
				t.Fatal(err)
			}
			var conflictName string
			for _, entry := range entries {
				if strings.Contains(entry.Name(), ".conflict-") {
					conflictName = entry.Name()
				}
			}
			if conflictName == "" {
				t.Fatal("local conflict copy missing")
			}
			losing := "remote-version"
			if preferred == "remote" {
				losing = "local-version"
			}
			assertFileContent(t, filepath.Join(local, conflictName), losing)
			if dav.content("/sync/"+conflictName) != losing {
				t.Fatalf("remote conflict copy=%q want=%q", dav.content("/sync/"+conflictName), losing)
			}
			if len(recoveries.journals) != 0 || states.saves != 2 {
				t.Fatalf("recoveries=%#v state saves=%d", recoveries.journals, states.saves)
			}
		})
	}
}

func assertFileContent(t *testing.T, name string, expected string) {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != expected {
		t.Fatalf("%s=%q want=%q", name, data, expected)
	}
}

func TestSyncDeletionFiltersAndValidation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(root, "report.md"), []byte("report"), 0600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(
		filepath.Join(root, "report.md"), filepath.Join(root, "link"),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := snapshotLocal(
		context.Background(), root, false, 100,
	); err == nil || !strings.Contains(err.Error(), "unsupported local file type") {
		t.Fatalf("symlink error: %v", err)
	}
	if _, err := snapshotLocal(
		context.Background(), root, false, 1,
	); err == nil || !strings.Contains(err.Error(), "--max-entries") {
		t.Fatalf("entry-limit error: %v", err)
	}
	for _, request := range []SyncRequest{
		{Direction: SyncDirection("sideways"), LocalRoot: root, RemoteRoot: "/", MaxEntries: 1},
		{Direction: SyncPush, RemoteRoot: "/", MaxEntries: 1},
		{Direction: SyncPull, LocalRoot: root, MaxEntries: 1},
		{Direction: SyncPull, LocalRoot: root, RemoteRoot: "/", MaxEntries: 0},
	} {
		if _, err := validateSyncRequest(request); err == nil {
			t.Fatalf("request accepted: %#v", request)
		}
	}
	if direction, err := validateSyncRequest(SyncRequest{
		Direction: SyncBidirectional, LocalRoot: root,
		RemoteRoot: "/", MaxEntries: 1,
	}); err != nil || direction != syncmodel.Bidirectional {
		t.Fatalf("bidirectional request: direction=%q err=%v", direction, err)
	}
	if _, err := syncLocalPath(root, "../../escape"); err == nil {
		t.Fatal("escaping local sync path accepted")
	}
	if err := validateSyncRemoteName("../escape"); err == nil {
		t.Fatal("unsafe remote name accepted")
	}
}

func syncTestDependencies(
	server string,
	states *memorySyncStates,
) Dependencies {
	return Dependencies{
		Config: &memoryConfig{store: &appconfig.Store{
			Version: appconfig.CurrentVersion, Current: "work",
			Profiles: map[string]appconfig.Profile{"work": {
				Server: server, Username: "alice", AuthType: "basic",
			}},
		}},
		Credentials: &memoryCredentials{
			secrets: map[string]credentials.Secret{
				"work": {Password: "secret"},
			},
		},
		SyncStates: states,
		SyncRecoveries: &memorySyncRecoveries{
			journals: map[string]syncrecovery.Journal{},
		},
	}
}

type syncDAV struct {
	lock            sync.Mutex
	nodes           map[string]*syncDAVNode
	failMoveTargets map[string]bool
}

type syncDAVNode struct {
	directory bool
	content   string
}

func newSyncDAV() *syncDAV {
	return &syncDAV{
		nodes:           map[string]*syncDAVNode{},
		failMoveTargets: map[string]bool{},
	}
}

func (dav *syncDAV) ServeHTTP(
	writer http.ResponseWriter,
	request *http.Request,
) {
	remote := syncDAVRemotePath(request.URL.Path)
	switch request.Method {
	case "PROPFIND":
		dav.propfind(writer, request, remote)
	case "MKCOL":
		dav.lock.Lock()
		dav.nodes[remote] = &syncDAVNode{directory: true}
		dav.lock.Unlock()
		writer.WriteHeader(http.StatusCreated)
	case http.MethodPut:
		data, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		dav.put(remote, string(data))
		writer.Header().Set("ETag", syncDAVETag(string(data)))
		writer.WriteHeader(http.StatusCreated)
	case http.MethodGet:
		dav.lock.Lock()
		node := dav.nodes[remote]
		dav.lock.Unlock()
		if node == nil || node.directory {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Length", fmt.Sprint(len(node.content)))
		writer.Header().Set("ETag", syncDAVETag(node.content))
		_, _ = io.WriteString(writer, node.content)
	case http.MethodDelete:
		dav.lock.Lock()
		for name := range dav.nodes {
			if name == remote || strings.HasPrefix(name, remote+"/") {
				delete(dav.nodes, name)
			}
		}
		dav.lock.Unlock()
		writer.WriteHeader(http.StatusNoContent)
	case "MOVE":
		destination, err := url.Parse(request.Header.Get("Destination"))
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		target := syncDAVRemotePath(destination.Path)
		dav.lock.Lock()
		if dav.failMoveTargets[target] {
			dav.lock.Unlock()
			http.Error(
				writer, "injected move failure",
				http.StatusInternalServerError,
			)
			return
		}
		if request.Header.Get("Overwrite") == "F" &&
			dav.nodes[target] != nil {
			dav.lock.Unlock()
			http.Error(writer, "destination exists", http.StatusPreconditionFailed)
			return
		}
		dav.nodes[target] = dav.nodes[remote]
		delete(dav.nodes, remote)
		dav.lock.Unlock()
		writer.WriteHeader(http.StatusCreated)
	case "COPY":
		destination, err := url.Parse(request.Header.Get("Destination"))
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		target := syncDAVRemotePath(destination.Path)
		dav.lock.Lock()
		source := dav.nodes[remote]
		if source == nil {
			dav.lock.Unlock()
			http.NotFound(writer, request)
			return
		}
		if request.Header.Get("Overwrite") == "F" && dav.nodes[target] != nil {
			dav.lock.Unlock()
			http.Error(writer, "destination exists", http.StatusPreconditionFailed)
			return
		}
		dav.nodes[target] = &syncDAVNode{
			directory: source.directory, content: source.content,
		}
		dav.lock.Unlock()
		writer.WriteHeader(http.StatusCreated)
	default:
		http.Error(writer, "unsupported", http.StatusNotFound)
	}
}

func (dav *syncDAV) propfind(
	writer http.ResponseWriter,
	request *http.Request,
	remote string,
) {
	dav.lock.Lock()
	defer dav.lock.Unlock()
	node := dav.nodes[remote]
	if node == nil {
		http.NotFound(writer, request)
		return
	}
	names := []string{remote}
	if request.Header.Get("Depth") == "1" && node.directory {
		for name := range dav.nodes {
			if path.Dir(name) == remote {
				names = append(names, name)
			}
		}
	}
	sortStrings(names)
	writer.WriteHeader(http.StatusMultiStatus)
	_, _ = io.WriteString(writer, `<?xml version="1.0"?>`+
		`<d:multistatus xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns">`)
	for _, name := range names {
		current := dav.nodes[name]
		kind := `<d:resourcetype/>`
		size, checksums := 0, ""
		if current.directory {
			kind = `<d:resourcetype><d:collection/></d:resourcetype>`
		} else {
			size = len(current.content)
			checksums = `<oc:checksums><oc:checksum>SHA1:` +
				syncDAVSHA1(current.content) +
				`</oc:checksum></oc:checksums>`
		}
		href := "/remote.php/dav/files/alice" + name
		_, _ = fmt.Fprintf(
			writer,
			`<d:response><d:href>%s</d:href><d:propstat>`+
				`<d:status>HTTP/1.1 200 OK</d:status><d:prop>`+
				`<d:displayname>%s</d:displayname>%s`+
				`<d:getcontentlength>%d</d:getcontentlength>`+
				`<d:getetag>%s</d:getetag>`+
				`<d:getlastmodified>Mon, 01 Jan 2024 00:00:00 GMT</d:getlastmodified>`+
				`%s</d:prop></d:propstat></d:response>`,
			html.EscapeString(href), html.EscapeString(path.Base(name)),
			kind, size, syncDAVETag(current.content), checksums,
		)
	}
	_, _ = io.WriteString(writer, `</d:multistatus>`)
}

func (dav *syncDAV) put(remote, content string) {
	dav.lock.Lock()
	dav.nodes[remote] = &syncDAVNode{content: content}
	dav.lock.Unlock()
}

func (dav *syncDAV) content(remote string) string {
	dav.lock.Lock()
	defer dav.lock.Unlock()
	if dav.nodes[remote] == nil {
		return ""
	}
	return dav.nodes[remote].content
}

func syncDAVRemotePath(value string) string {
	const prefix = "/remote.php/dav/files/alice"
	value = strings.TrimPrefix(value, prefix)
	if value == "" {
		return "/"
	}
	return "/" + strings.TrimPrefix(path.Clean("/"+value), "/")
}

func syncDAVSHA1(value string) string {
	sum := sha1.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

func syncDAVETag(value string) string {
	return `"` + syncDAVSHA1(value) + `"`
}

func sortStrings(values []string) {
	for index := 1; index < len(values); index++ {
		for current := index; current > 0 &&
			values[current] < values[current-1]; current-- {
			values[current], values[current-1] =
				values[current-1], values[current]
		}
	}
}
