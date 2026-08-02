// Package transfer coordinates recursive uploads and downloads independently
// of a concrete remote protocol.
package transfer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Entry describes the remote metadata needed for recursive traversal.
type Entry struct {
	Name string
	Path string
	Type string
	Size int64
}

// Remote contains the protocol operations needed by recursive transfers.
type Remote struct {
	Stat     func(context.Context, string) (Entry, error)
	List     func(context.Context, string) ([]Entry, error)
	Upload   func(context.Context, string, string, func(int64)) error
	Download func(context.Context, string, string, func(int64)) error
	Mkdir    func(context.Context, string) error
}

// Options controls recursive transfer execution.
type Options struct {
	Concurrency int
	Progress    func(Progress)
}

// Progress is an immutable aggregate transfer progress snapshot.
type Progress struct {
	Operation      string
	Source         string
	Destination    string
	CompletedBytes int64
	TotalBytes     int64
	CompletedFiles int
	TotalFiles     int
	StartedAt      time.Time
}

type task struct {
	source      string
	destination string
	size        int64
	run         func(context.Context, func(int64)) error
}

// Upload recursively uploads local into remote.
func Upload(ctx context.Context, client Remote, local, remote string) error {
	return UploadWithOptions(ctx, client, local, remote, Options{})
}

// UploadWithOptions uploads local into remote using bounded parallelism.
func UploadWithOptions(ctx context.Context, client Remote, local, remote string, options Options) error {
	root, err := filepath.Abs(local)
	if err != nil {
		return err
	}
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return runTasks(ctx, "upload", []task{{
			source: root, destination: remote, size: info.Size(),
			run: func(ctx context.Context, progress func(int64)) error {
				return client.Upload(ctx, root, remote, progress)
			},
		}}, options)
	}
	if err := client.Mkdir(ctx, remote); err != nil {
		return err
	}
	tasks := make([]task, 0)
	err = filepath.WalkDir(root, func(localPath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if localPath == root {
			return nil
		}
		relative, err := filepath.Rel(root, localPath)
		if err != nil {
			return err
		}
		remotePath := path.Join(cleanRemote(remote), filepath.ToSlash(relative))
		if entry.IsDir() {
			return client.Mkdir(ctx, remotePath)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported local file type: %s", localPath)
		}
		tasks = append(tasks, task{
			source: localPath, destination: remotePath, size: info.Size(),
			run: func(ctx context.Context, progress func(int64)) error {
				return client.Upload(ctx, localPath, remotePath, progress)
			},
		})
		return nil
	})
	if err != nil {
		return err
	}
	return runTasks(ctx, "upload", tasks, options)
}

// Download recursively downloads remote into local.
func Download(ctx context.Context, client Remote, remote, local string) error {
	return DownloadWithOptions(ctx, client, remote, local, Options{})
}

// DownloadWithOptions downloads remote into local using bounded parallelism.
func DownloadWithOptions(ctx context.Context, client Remote, remote, local string, options Options) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	meta, err := client.Stat(ctx, remote)
	if err != nil {
		return err
	}
	if meta.Type != "directory" {
		return runTasks(ctx, "download", []task{{
			source: remote, destination: local, size: meta.Size,
			run: func(ctx context.Context, progress func(int64)) error {
				return client.Download(ctx, remote, local, progress)
			},
		}}, options)
	}
	local, err = directoryDestination(local, meta)
	if err != nil {
		return err
	}
	tasks := make([]task, 0)
	if err := collectDownloads(ctx, client, remote, local, &tasks); err != nil {
		return err
	}
	return runTasks(ctx, "download", tasks, options)
}

func directoryDestination(local string, remote Entry) (string, error) {
	info, err := os.Stat(local)
	if errors.Is(err, os.ErrNotExist) {
		return local, nil
	}
	if err != nil {
		return "", err
	}
	if !info.IsDir() || remote.Name == "" ||
		filepath.Base(filepath.Clean(local)) == remote.Name {
		return local, nil
	}
	return SafeLocalChild(local, remote.Name)
}

func collectDownloads(ctx context.Context, client Remote, remote, local string, tasks *[]task) error {
	if err := os.MkdirAll(local, 0750); err != nil {
		return err
	}
	children, err := client.List(ctx, remote)
	if err != nil {
		return err
	}
	for _, child := range children {
		localPath, err := SafeLocalChild(local, child.Name)
		if err != nil {
			return err
		}
		if child.Type == "directory" {
			if err := collectDownloads(ctx, client, child.Path, localPath, tasks); err != nil {
				return err
			}
			continue
		}
		child := child
		*tasks = append(*tasks, task{
			source: child.Path, destination: localPath, size: child.Size,
			run: func(ctx context.Context, progress func(int64)) error {
				return client.Download(ctx, child.Path, localPath, progress)
			},
		})
	}
	return nil
}

func runTasks(ctx context.Context, operation string, tasks []task, options Options) error {
	if len(tasks) == 0 {
		return nil
	}
	workers := options.Concurrency
	if workers <= 0 {
		workers = 1
	}
	if workers > len(tasks) {
		workers = len(tasks)
	}
	started := time.Now()
	var totalBytes int64
	for _, task := range tasks {
		totalBytes += task.size
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	failures := make(chan error, 1)
	var completedBytes atomic.Int64
	var completedFiles atomic.Int64
	var progressLock sync.Mutex
	reportedBytes := make([]int64, len(tasks))
	report := func(index int, current task, bytes int64, fileCompleted bool) {
		progressLock.Lock()
		defer progressLock.Unlock()
		if bytes < 0 {
			bytes = 0
		}
		delta := bytes - reportedBytes[index]
		reportedBytes[index] = bytes
		totalCompleted := completedBytes.Add(delta)
		files := int(completedFiles.Load())
		if fileCompleted {
			files = int(completedFiles.Add(1))
		}
		if options.Progress != nil {
			options.Progress(Progress{
				Operation: operation, Source: current.source, Destination: current.destination,
				CompletedBytes: totalCompleted, TotalBytes: totalBytes, CompletedFiles: files,
				TotalFiles: len(tasks), StartedAt: started,
			})
		}
	}
	var wait sync.WaitGroup
	wait.Add(workers)
	type work struct {
		index int
		task  task
	}
	workQueue := make(chan work)
	for range workers {
		go func() {
			defer wait.Done()
			for queued := range workQueue {
				current := queued.task
				if err := current.run(ctx, func(bytes int64) {
					report(queued.index, current, bytes, false)
				}); err != nil {
					select {
					case failures <- fmt.Errorf("%s %s: %w", operation, current.source, err):
						cancel()
					default:
					}
					return
				}
				report(queued.index, current, current.size, true)
			}
		}()
	}
sendLoop:
	for index, current := range tasks {
		select {
		case workQueue <- work{index: index, task: current}:
		case <-ctx.Done():
			break sendLoop
		}
	}
	close(workQueue)
	wait.Wait()
	select {
	case err := <-failures:
		return err
	default:
		return ctx.Err()
	}
}

// SafeLocalChild resolves name under root and rejects traversal and separators.
func SafeLocalChild(root, name string) (string, error) {
	if name == "" || name == "." || name == ".." || filepath.IsAbs(name) ||
		strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("unsafe remote resource name %q", name)
	}
	root = filepath.Clean(root)
	child := filepath.Join(root, name)
	relative, err := filepath.Rel(root, child)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("remote resource name %q escapes destination", name)
	}
	return child, nil
}

func cleanRemote(remote string) string {
	return "/" + strings.TrimPrefix(path.Clean("/"+remote), "/")
}
