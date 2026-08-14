package sync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	syncmodel "github.com/mzner/ocis-cli/internal/sync"
	"github.com/mzner/ocis-cli/internal/syncjob"
)

func TestDeletionFiltersAndValidation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "report.md"), []byte("report"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "report.md"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := snapshotLocal(context.Background(), root, false, 100); err == nil || !strings.Contains(err.Error(), "unsupported local file type") {
		t.Fatalf("symlink error: %v", err)
	}
	if _, err := snapshotLocal(context.Background(), root, false, 1); err == nil || !strings.Contains(err.Error(), "--max-entries") {
		t.Fatalf("entry-limit error: %v", err)
	}
	for _, request := range []Request{
		{Direction: Direction("sideways"), LocalRoot: root, RemoteRoot: "/", MaxEntries: 1},
		{Direction: Push, RemoteRoot: "/", MaxEntries: 1},
		{Direction: Pull, LocalRoot: root, MaxEntries: 1},
		{Direction: Pull, LocalRoot: root, RemoteRoot: "/", MaxEntries: 0},
	} {
		if _, err := validateSyncRequest(request); err == nil {
			t.Fatalf("request accepted: %#v", request)
		}
	}
	if direction, err := validateSyncRequest(Request{Direction: Bidirectional, LocalRoot: root, RemoteRoot: "/", MaxEntries: 1}); err != nil || direction != syncmodel.Bidirectional {
		t.Fatalf("bidirectional request: direction=%q err=%v", direction, err)
	}
	if _, err := syncLocalPath(root, "../../escape"); err == nil {
		t.Fatal("escaping local sync path accepted")
	}
	if err := validateSyncRemoteName("../escape"); err == nil {
		t.Fatal("unsafe remote name accepted")
	}
}

func TestJobRootDisplay(t *testing.T) {
	job := syncjob.Job{Direction: syncmodel.Pull, LocalRoot: "/local", RemoteRoot: "/remote"}
	if got := syncJobRoots(job); got != "/remote -> /local" {
		t.Fatalf("roots=%q", got)
	}
}

func TestUniqueStateID(t *testing.T) {
	first := "123456789012a" + strings.Repeat("0", 51)
	second := "123456789012b" + strings.Repeat("0", 51)
	if got := uniqueSyncStateID(first, []string{first, second}); got != first[:13] {
		t.Fatalf("unique ID=%q", got)
	}
	if got := uniqueSyncStateID(first, []string{first}); got != first[:12] {
		t.Fatalf("single ID=%q", got)
	}
}
