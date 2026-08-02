package transfer

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSafeLocalChild(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"", ".", "..", "../escape", `..\\escape`, "/absolute", `C:\\absolute`} {
		if _, err := SafeLocalChild(root, name); err == nil {
			t.Errorf("SafeLocalChild(%q) succeeded", name)
		}
	}
	got, err := SafeLocalChild(root, "report.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(root, "report.pdf") {
		t.Fatalf("got %q", got)
	}
}

func TestUploadDirectoryCreatesCollectionsAndTransfersFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "one.txt"), []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "two.txt"), []byte("two"), 0600); err != nil {
		t.Fatal(err)
	}
	var lock sync.Mutex
	var directories, files []string
	var progress Progress
	client := Remote{
		Mkdir: func(_ context.Context, remote string) error {
			lock.Lock()
			directories = append(directories, remote)
			lock.Unlock()
			return nil
		},
		Upload: func(_ context.Context, _ string, remote string, progress func(int64)) error {
			lock.Lock()
			files = append(files, remote)
			lock.Unlock()
			progress(3)
			return nil
		},
	}
	err := UploadWithOptions(context.Background(), client, root, "/backup", Options{
		Concurrency: 2,
		Progress:    func(value Progress) { progress = value },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(directories) != 2 {
		t.Fatalf("directories: %#v", directories)
	}
	if len(files) != 2 {
		t.Fatalf("files: %#v", files)
	}
	if progress.CompletedFiles != 2 || progress.TotalBytes != 6 {
		t.Fatalf("progress: %#v", progress)
	}
}

func TestDownloadDirectoryTransfersInParallel(t *testing.T) {
	var active atomic.Int64
	var maximum atomic.Int64
	client := Remote{
		Stat: func(context.Context, string) (Entry, error) {
			return Entry{Type: "directory"}, nil
		},
		List: func(_ context.Context, remote string) ([]Entry, error) {
			if remote != "/" {
				return nil, nil
			}
			return []Entry{
				{Name: "one.txt", Path: "/one.txt", Type: "file", Size: 3},
				{Name: "two.txt", Path: "/two.txt", Type: "file", Size: 3},
			}, nil
		},
		Download: func(_ context.Context, _ string, local string, progress func(int64)) error {
			current := active.Add(1)
			for {
				seen := maximum.Load()
				if current <= seen || maximum.CompareAndSwap(seen, current) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			progress(3)
			active.Add(-1)
			return os.WriteFile(local, []byte("123"), 0600)
		},
	}
	root := filepath.Join(t.TempDir(), "download")
	if err := DownloadWithOptions(
		context.Background(), client, "/", root, Options{Concurrency: 2},
	); err != nil {
		t.Fatal(err)
	}
	if maximum.Load() != 2 {
		t.Fatalf("maximum concurrency: got %d, want 2", maximum.Load())
	}
	for _, name := range []string{"one.txt", "two.txt"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDownloadDirectoryIntoExistingParentUsesRemoteName(t *testing.T) {
	parent := t.TempDir()
	client := Remote{
		Stat: func(context.Context, string) (Entry, error) {
			return Entry{Name: "demo", Path: "/demo", Type: "directory"}, nil
		},
		List: func(context.Context, string) ([]Entry, error) {
			return []Entry{{Name: "report.txt", Path: "/demo/report.txt", Type: "file", Size: 5}}, nil
		},
		Download: func(_ context.Context, _ string, local string, progress func(int64)) error {
			progress(5)
			return os.WriteFile(local, []byte("hello"), 0600)
		},
	}
	if err := DownloadWithOptions(
		context.Background(), client, "/demo", parent, Options{},
	); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(parent, "demo", "report.txt"))
	if err != nil || string(data) != "hello" {
		t.Fatalf("download: %q, %v", data, err)
	}
}

func TestDownloadDirectoryIntoMatchingDirectoryDoesNotDoubleNest(t *testing.T) {
	parent := t.TempDir()
	destination := filepath.Join(parent, "demo")
	if err := os.Mkdir(destination, 0750); err != nil {
		t.Fatal(err)
	}
	client := Remote{
		Stat: func(context.Context, string) (Entry, error) {
			return Entry{Name: "demo", Path: "/demo", Type: "directory"}, nil
		},
		List: func(context.Context, string) ([]Entry, error) { return nil, nil },
	}
	if err := Download(context.Background(), client, "/demo", destination); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, "demo")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected nested directory: %v", err)
	}
}

func TestUploadStopsAfterFailure(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"one.txt", "two.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	want := errors.New("upload failed")
	client := Remote{
		Mkdir: func(context.Context, string) error { return nil },
		Upload: func(_ context.Context, _ string, remote string, _ func(int64)) error {
			if path.Base(remote) == "one.txt" {
				return want
			}
			return nil
		},
	}
	err := UploadWithOptions(context.Background(), client, root, "/", Options{Concurrency: 1})
	if !errors.Is(err, want) {
		t.Fatalf("got %v, want wrapped upload failure", err)
	}
}

func TestDownloadHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := DownloadWithOptions(ctx, Remote{}, "/", t.TempDir(), Options{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context cancellation", err)
	}
}

func TestDownloadRejectsHostileRemoteName(t *testing.T) {
	client := Remote{
		Stat: func(context.Context, string) (Entry, error) {
			return Entry{Type: "directory"}, nil
		},
		List: func(context.Context, string) ([]Entry, error) {
			return []Entry{{Name: "../escape", Path: "/escape", Type: "file"}}, nil
		},
	}
	if err := Download(context.Background(), client, "/", t.TempDir()); err == nil {
		t.Fatal("Download succeeded with hostile child name")
	}
}
