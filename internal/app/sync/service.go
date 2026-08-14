package sync

import (
	"context"
	"crypto/sha1" //nolint:gosec // SHA-1 is used only to compare with oCIS content checksums
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
	syncmodel "github.com/mzner/ocis-cli/internal/sync"
	"github.com/mzner/ocis-cli/internal/webdav"
)

type syncResult struct {
	Direction  syncmodel.Direction `json:"direction"`
	LocalRoot  string              `json:"localRoot"`
	RemoteRoot string              `json:"remoteRoot"`
	DryRun     bool                `json:"dryRun"`
	Applied    bool                `json:"applied"`
	StateKey   string              `json:"stateKey,omitempty"`
	Conflicts  int                 `json:"conflicts"`
	Transfers  int                 `json:"transfers"`
	Moves      int                 `json:"moves"`
	Copies     int                 `json:"copies"`
	Deletions  int                 `json:"deletions"`
	Actions    []syncmodel.Action  `json:"actions"`
}

type preparedSyncRequest struct {
	request    Request
	direction  syncmodel.Direction
	localRoot  string
	remoteRoot string
}

func Run(
	ctx context.Context,
	request Request,
	selected string,
	options Options,
) error {
	prepared, err := prepareSyncRequest(request)
	if err != nil {
		return err
	}
	client, err := options.NewClient(ctx, selected)
	if err != nil {
		return err
	}
	if err := client.SelectSpace(options.Space); err != nil {
		return err
	}
	return runPreparedSync(ctx, prepared, client, options)
}

func prepareSyncRequest(request Request) (preparedSyncRequest, error) {
	direction, err := validateSyncRequest(request)
	if err != nil {
		return preparedSyncRequest{}, apperror.Wrap(
			apperror.KindUsage, "sync", err,
		)
	}
	localRoot, err := filepath.Abs(request.LocalRoot)
	if err != nil {
		return preparedSyncRequest{}, err
	}
	includes, excludes, err := syncmodel.NormalizePatterns(
		request.Includes, request.Excludes,
	)
	if err != nil {
		return preparedSyncRequest{}, apperror.Wrap(
			apperror.KindUsage, "sync", err,
		)
	}
	request.Includes = includes
	request.Excludes = excludes
	return preparedSyncRequest{
		request: request, direction: direction,
		localRoot:  filepath.Clean(localRoot),
		remoteRoot: cleanRemote(request.RemoteRoot),
	}, nil
}

func runPreparedSync(
	ctx context.Context,
	prepared preparedSyncRequest,
	client Client,
	options Options,
) error {
	if prepared.direction == syncmodel.Bidirectional {
		return runPreparedBidirectionalSync(ctx, prepared, client, options)
	}
	request := prepared.request
	direction := prepared.direction
	localRoot := prepared.localRoot
	remoteRoot := prepared.remoteRoot
	accountID := client.AccountID()
	if accountID == "" {
		return errors.New("cannot bind sync state to an unauthenticated account")
	}
	binding := syncmodel.Binding{
		Profile: client.ProfileName(), AccountID: accountID,
		SpaceID: client.SelectedSpaceID(), Direction: direction,
		LocalRoot: localRoot, RemoteRoot: remoteRoot,
		Includes: request.Includes, Excludes: request.Excludes,
	}
	stateKey := binding.Key()
	previous, found, err := options.SyncStates.Load(stateKey)
	if err != nil {
		return fmt.Errorf("load sync state: %w", err)
	}
	if found && previous.Binding.Key() != binding.Key() {
		return errors.New("saved sync state does not match the current binding")
	}

	source, destination, err := collectSyncSnapshots(
		ctx, client, direction, localRoot, remoteRoot, request.MaxEntries,
	)
	if err != nil {
		return err
	}
	var baseline *syncmodel.State
	if found {
		baseline = &previous
	}
	plan, err := syncmodel.Build(
		direction, source, destination, baseline,
		syncmodel.Options{
			Delete: request.Delete, Overwrite: request.Overwrite,
			Includes: request.Includes, Excludes: request.Excludes,
		},
	)
	if err != nil {
		return apperror.Wrap(apperror.KindUsage, "sync plan", err)
	}
	result := syncResult{
		Direction: direction, LocalRoot: localRoot, RemoteRoot: remoteRoot,
		DryRun: request.DryRun, StateKey: stateKey,
		Conflicts: plan.Conflicts, Transfers: plan.Transfers,
		Deletions: plan.Deletions, Actions: plan.Actions,
	}
	if request.DryRun {
		return writeSyncResult(options, result)
	}
	if plan.Conflicts > 0 {
		return syncConflict(plan)
	}
	if err := applySyncPlan(
		ctx, client, direction, localRoot, remoteRoot, plan, options,
	); err != nil {
		return err
	}

	postSource, postDestination, err := collectSyncSnapshots(
		ctx, client, direction, localRoot, remoteRoot, request.MaxEntries,
	)
	if err != nil {
		return fmt.Errorf("verify synchronized trees: %w", err)
	}
	postSource, err = syncmodel.Filter(
		postSource, request.Includes, request.Excludes,
	)
	if err != nil {
		return err
	}
	postDestination, err = syncmodel.Filter(
		postDestination, request.Includes, request.Excludes,
	)
	if err != nil {
		return err
	}
	state := syncmodel.NewState(binding, postSource, postDestination)
	if err := options.SyncStates.Save(stateKey, state); err != nil {
		return fmt.Errorf("save sync state: %w", err)
	}
	result.Applied = true
	return writeSyncResult(options, result)
}

func validateSyncRequest(
	request Request,
) (syncmodel.Direction, error) {
	var direction syncmodel.Direction
	switch request.Direction {
	case Push:
		direction = syncmodel.Push
	case Pull:
		direction = syncmodel.Pull
	case Bidirectional:
		direction = syncmodel.Bidirectional
	default:
		return "", fmt.Errorf("unknown sync direction %q", request.Direction)
	}
	if strings.TrimSpace(request.LocalRoot) == "" {
		return "", errors.New("local directory is required")
	}
	if strings.TrimSpace(request.RemoteRoot) == "" {
		return "", errors.New("remote directory is required")
	}
	if request.MaxEntries < 1 {
		return "", errors.New("--max-entries must be at least 1")
	}
	strategy := request.ConflictStrategy
	if strategy == "" {
		strategy = "abort"
	}
	if direction != syncmodel.Bidirectional &&
		(request.ConflictStrategy != "" || request.Prefer != "") {
		return "", errors.New("conflict strategy is available only for bidirectional sync")
	}
	if direction == syncmodel.Bidirectional && (request.Delete || request.Overwrite) {
		return "", errors.New(
			"--delete and --overwrite are one-way policies and cannot be used with bidirectional sync",
		)
	}
	switch strategy {
	case "abort":
		if request.Prefer != "" {
			return "", errors.New("--prefer requires --conflict-strategy=keep-both")
		}
	case "keep-both":
		if request.Prefer != "local" && request.Prefer != "remote" {
			return "", errors.New(
				"--conflict-strategy=keep-both requires --prefer=local or --prefer=remote",
			)
		}
	default:
		return "", fmt.Errorf("unknown conflict strategy %q", strategy)
	}
	return direction, nil
}

func collectSyncSnapshots(
	ctx context.Context,
	client Client,
	direction syncmodel.Direction,
	localRoot, remoteRoot string,
	maxEntries int,
) (syncmodel.Snapshot, syncmodel.Snapshot, error) {
	switch direction {
	case syncmodel.Push:
		local, err := snapshotLocal(ctx, localRoot, false, maxEntries)
		if err != nil {
			return nil, nil, err
		}
		remote, err := snapshotRemote(
			ctx, client, remoteRoot, true, maxEntries,
		)
		return local, remote, err
	case syncmodel.Pull:
		remote, err := snapshotRemote(
			ctx, client, remoteRoot, false, maxEntries,
		)
		if err != nil {
			return nil, nil, err
		}
		local, err := snapshotLocal(ctx, localRoot, true, maxEntries)
		return remote, local, err
	default:
		return nil, nil, fmt.Errorf("unknown sync direction %q", direction)
	}
}

func snapshotLocal(
	ctx context.Context,
	root string,
	allowMissing bool,
	maxEntries int,
) (syncmodel.Snapshot, error) {
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) && allowMissing {
		return syncmodel.Snapshot{}, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("local sync root is not a directory: %s", root)
	}
	result := make(syncmodel.Snapshot)
	err = filepath.WalkDir(root, func(
		current string, entry os.DirEntry, walkErr error,
	) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(result) >= maxEntries {
			return fmt.Errorf(
				"sync tree exceeds --max-entries=%d", maxEntries,
			)
		}
		relative, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		if relative == "." {
			relative = ""
		} else {
			relative = filepath.ToSlash(relative)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		value := syncmodel.Entry{
			Path: relative, Modified: syncModified(info.ModTime()),
		}
		switch {
		case entry.IsDir():
			value.Type = "directory"
		case info.Mode().IsRegular():
			value.Type = "file"
			value.Size = info.Size()
			value.Checksum, err = localSHA1(current)
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf(
				"unsupported local file type in sync tree: %s", current,
			)
		}
		result[relative] = value
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func snapshotRemote(
	ctx context.Context,
	client Client,
	root string,
	allowMissing bool,
	maxEntries int,
) (syncmodel.Snapshot, error) {
	rootItem, err := client.Stat(root)
	if webdav.StatusCode(err) == 404 && allowMissing {
		return syncmodel.Snapshot{}, nil
	}
	if err != nil {
		return nil, err
	}
	if rootItem.Type != "directory" {
		return nil, fmt.Errorf("remote sync root is not a directory: %s", root)
	}
	result := syncmodel.Snapshot{
		"": remoteSyncEntry("", rootItem),
	}
	var walk func(string, string) error
	walk = func(remote, relative string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		children, err := client.List(remote)
		if err != nil {
			return err
		}
		sort.Slice(children, func(i, j int) bool {
			return children[i].Name < children[j].Name
		})
		for _, child := range children {
			if len(result) >= maxEntries {
				return fmt.Errorf(
					"sync tree exceeds --max-entries=%d", maxEntries,
				)
			}
			if err := validateSyncRemoteName(child.Name); err != nil {
				return err
			}
			childRelative := path.Join(relative, child.Name)
			result[childRelative] = remoteSyncEntry(
				childRelative, child,
			)
			if child.Type == "directory" {
				if err := walk(child.Path, childRelative); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(root, ""); err != nil {
		return nil, err
	}
	return result, nil
}

func remoteSyncEntry(relative string, value webdav.Item) syncmodel.Entry {
	checksum := ""
	for _, candidate := range value.Checksums {
		if strings.EqualFold(candidate.Algorithm, "SHA1") {
			checksum = "sha1:" + strings.ToLower(candidate.Value)
			break
		}
	}
	modified := value.LastModified
	if parsed, err := http.ParseTime(value.LastModified); err == nil {
		modified = syncModified(parsed)
	}
	return syncmodel.Entry{
		Path: relative, Type: value.Type, Size: value.Size,
		Modified: modified, ETag: value.ETag, Checksum: checksum,
	}
}

func applySyncPlan(
	ctx context.Context,
	client Client,
	direction syncmodel.Direction,
	localRoot, remoteRoot string,
	plan syncmodel.Plan,
	options Options,
) error {
	uploadCapabilities := webdav.TUSCapabilities{}
	if direction == syncmodel.Push {
		uploadCapabilities = client.DiscoverUploadCapabilities(ctx)
	}
	for _, action := range plan.Actions {
		if err := ctx.Err(); err != nil {
			return err
		}
		if action.Action == syncmodel.ActionSkip {
			continue
		}
		if action.Action == syncmodel.ActionConflict {
			return errors.New("cannot apply a synchronization plan with conflicts")
		}
		if err := applySyncAction(
			client, direction, localRoot, remoteRoot,
			action, uploadCapabilities, options,
		); err != nil {
			return fmt.Errorf(
				"sync %s %s: %w", action.Action, displaySyncPath(action.Path), err,
			)
		}
	}
	return nil
}

func applySyncAction(
	client Client,
	direction syncmodel.Direction,
	localRoot, remoteRoot string,
	action syncmodel.Action,
	uploadCapabilities webdav.TUSCapabilities,
	options Options,
) error {
	local, err := syncLocalPath(localRoot, action.Path)
	if err != nil {
		return err
	}
	remote := syncRemotePath(remoteRoot, action.Path)
	sourceRelative := action.Path
	if action.FromPath != "" {
		sourceRelative = action.FromPath
	}
	localSource, err := syncLocalPath(localRoot, sourceRelative)
	if err != nil {
		return err
	}
	remoteSource := syncRemotePath(remoteRoot, sourceRelative)
	if action.Action == syncmodel.ActionMove {
		return applySyncMove(
			client, direction, localRoot, remoteRoot, local, remote, action,
		)
	}
	if action.Action == syncmodel.ActionCopy {
		return applySyncCopy(
			client, direction, localSource, remoteSource, local, remote, action,
		)
	}
	if direction == syncmodel.Push {
		if err := verifyRemoteSyncPrecondition(
			client, remote, action.Destination,
		); err != nil {
			return err
		}
		if action.Replace {
			if err := client.DAV().RemoveWithOptions(
				client.Context(), remote, webdav.RemoveOptions{
					Recursive:    true,
					ExpectedETag: syncExpectedETag(action.Destination),
				},
			); err != nil {
				return err
			}
		}
		switch action.Action {
		case syncmodel.ActionDelete:
			return client.DAV().RemoveWithOptions(
				client.Context(), remote, webdav.RemoveOptions{
					Recursive:    true,
					ExpectedETag: syncExpectedETag(action.Destination),
				},
			)
		case syncmodel.ActionCreateDirectory:
			return client.EnsureCollection(remote)
		case syncmodel.ActionTransfer:
			return client.DAV().UploadWithOptions(
				client.Context(), localSource, remote,
				webdav.TransferOptions{
					NoClobber: action.Destination.Type == "",
					Verify:    true, TUS: uploadCapabilities,
					ExpectedETag: syncExpectedETag(action.Destination),
				},
			)
		}
	}

	if err := verifyLocalSyncPrecondition(local, action.Destination); err != nil {
		return err
	}
	if action.Replace {
		if err := os.RemoveAll(local); err != nil {
			return err
		}
	}
	switch action.Action {
	case syncmodel.ActionDelete:
		return os.RemoveAll(local)
	case syncmodel.ActionCreateDirectory:
		return os.MkdirAll(local, 0750)
	case syncmodel.ActionTransfer:
		if err := os.MkdirAll(filepath.Dir(local), 0750); err != nil {
			return err
		}
		return client.DAV().DownloadWithOptions(
			client.Context(), remoteSource, local,
			webdav.TransferOptions{
				NoClobber: action.Destination.Type == "",
				Resume:    true, Verify: true,
				ExpectedETag: syncExpectedETag(action.Source),
			},
		)
	default:
		return fmt.Errorf("unsupported sync action %q", action.Action)
	}
}

func applySyncCopy(
	client Client,
	direction syncmodel.Direction,
	localSource string,
	remoteSource string,
	localDestination string,
	remoteDestination string,
	action syncmodel.Action,
) error {
	if action.FromPath == "" {
		return errors.New("sync copy source path is empty")
	}
	if direction == syncmodel.Push {
		if err := verifyRemoteSyncSourcePrecondition(
			client, remoteSource, action.Source,
		); err != nil {
			return err
		}
		if err := verifyRemoteSyncPrecondition(
			client, remoteDestination, action.Destination,
		); err != nil {
			return err
		}
		return client.DAV().CopyWithOptions(
			client.Context(), remoteSource, remoteDestination,
			webdav.MoveOptions{
				ExpectedETag: syncExpectedETag(action.Source),
			},
		)
	}
	if err := verifyLocalSyncSourcePrecondition(
		localSource, action.Source,
	); err != nil {
		return err
	}
	if err := verifyLocalSyncPrecondition(
		localDestination, action.Destination,
	); err != nil {
		return err
	}
	return copyLocalSyncFile(localSource, localDestination)
}

func copyLocalSyncFile(source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0750); err != nil {
		return err
	}
	input, err := os.Open(source) //nolint:gosec // path belongs to the selected sync tree
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".ocis-sync-copy-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()
	if err := temporary.Chmod(0600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := io.Copy(temporary, input); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Link(temporaryName, destination); err != nil {
		return err
	}
	return os.Remove(temporaryName)
}

func applySyncMove(
	client Client,
	direction syncmodel.Direction,
	localRoot string,
	remoteRoot string,
	localDestination string,
	remoteDestination string,
	action syncmodel.Action,
) error {
	if action.FromPath == "" {
		return errors.New("sync move source path is empty")
	}
	if direction == syncmodel.Push {
		remoteSource := syncRemotePath(remoteRoot, action.FromPath)
		if err := verifyRemoteSyncSourcePrecondition(
			client, remoteSource, action.Destination,
		); err != nil {
			return err
		}
		if err := verifyRemoteSyncPrecondition(
			client, remoteDestination, syncmodel.Entry{},
		); err != nil {
			return err
		}
		return client.DAV().MoveWithOptions(
			client.Context(), remoteSource, remoteDestination,
			webdav.MoveOptions{
				ExpectedETag: syncExpectedETag(action.Destination),
			},
		)
	}
	localSource, err := syncLocalPath(localRoot, action.FromPath)
	if err != nil {
		return err
	}
	if err := verifyLocalSyncSourcePrecondition(
		localSource, action.Destination,
	); err != nil {
		return err
	}
	if err := verifyLocalSyncPrecondition(
		localDestination, syncmodel.Entry{},
	); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(localDestination), 0750); err != nil {
		return err
	}
	return os.Rename(localSource, localDestination)
}

func verifyRemoteSyncPrecondition(
	client Client,
	remote string,
	expected syncmodel.Entry,
) error {
	return verifyRemoteSyncEntry(
		client, remote, expected, "destination",
	)
}

func verifyRemoteSyncSourcePrecondition(
	client Client,
	remote string,
	expected syncmodel.Entry,
) error {
	return verifyRemoteSyncEntry(client, remote, expected, "source")
}

func verifyRemoteSyncEntry(
	client Client,
	remote string,
	expected syncmodel.Entry,
	role string,
) error {
	current, err := client.Stat(remote)
	if webdav.StatusCode(err) == 404 && expected.Type == "" {
		return nil
	}
	if err != nil {
		return err
	}
	if expected.Type == "" {
		return apperror.Wrap(
			apperror.KindConflict, "sync precondition",
			fmt.Errorf("%s appeared after planning: %s", role, remote),
		)
	}
	actual := remoteSyncEntry(expected.Path, current)
	if !actual.Equal(expected) {
		return apperror.Wrap(
			apperror.KindConflict, "sync precondition",
			fmt.Errorf("%s changed after planning: %s", role, remote),
		)
	}
	return nil
}

func verifyLocalSyncPrecondition(
	local string,
	expected syncmodel.Entry,
) error {
	return verifyLocalSyncEntry(local, expected, "destination")
}

func verifyLocalSyncSourcePrecondition(
	local string,
	expected syncmodel.Entry,
) error {
	return verifyLocalSyncEntry(local, expected, "source")
}

func verifyLocalSyncEntry(
	local string,
	expected syncmodel.Entry,
	role string,
) error {
	current, err := localSyncEntry(local, expected.Path)
	if errors.Is(err, os.ErrNotExist) && expected.Type == "" {
		return nil
	}
	if err != nil {
		return err
	}
	if expected.Type == "" {
		return apperror.Wrap(
			apperror.KindConflict, "sync precondition",
			fmt.Errorf("%s appeared after planning: %s", role, local),
		)
	}
	if !current.Equal(expected) {
		return apperror.Wrap(
			apperror.KindConflict, "sync precondition",
			fmt.Errorf("%s changed after planning: %s", role, local),
		)
	}
	return nil
}

func localSyncEntry(
	local, relative string,
) (syncmodel.Entry, error) {
	info, err := os.Lstat(local)
	if err != nil {
		return syncmodel.Entry{}, err
	}
	value := syncmodel.Entry{
		Path: relative, Modified: syncModified(info.ModTime()),
	}
	switch {
	case info.IsDir():
		value.Type = "directory"
	case info.Mode().IsRegular():
		value.Type = "file"
		value.Size = info.Size()
		value.Checksum, err = localSHA1(local)
	default:
		err = fmt.Errorf("unsupported local file type: %s", local)
	}
	return value, err
}

func localSHA1(name string) (string, error) {
	file, err := os.Open(name) //nolint:gosec // path belongs to the user-selected sync tree
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	sum := sha1.New() //nolint:gosec // compatibility fingerprint, not a security primitive
	if _, err := io.Copy(sum, file); err != nil {
		return "", err
	}
	return "sha1:" + hex.EncodeToString(sum.Sum(nil)), nil
}

func syncModified(value time.Time) string {
	return value.UTC().Truncate(time.Second).Format(time.RFC3339)
}

func validateSyncRemoteName(name string) error {
	if name == "" || name == "." || name == ".." ||
		strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("unsafe remote sync entry name %q", name)
	}
	return nil
}

func syncLocalPath(root, relative string) (string, error) {
	target := filepath.Join(root, filepath.FromSlash(relative))
	within, err := filepath.Rel(root, target)
	if err != nil {
		return "", err
	}
	if within == ".." || strings.HasPrefix(within, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("sync path escapes local root: %q", relative)
	}
	return target, nil
}

func syncRemotePath(root, relative string) string {
	if relative == "" {
		return cleanRemote(root)
	}
	return cleanRemote(path.Join(root, relative))
}

func syncExpectedETag(entry syncmodel.Entry) string {
	if entry.Type != "file" {
		return ""
	}
	return entry.ETag
}

func syncConflict(plan syncmodel.Plan) error {
	paths := make([]string, 0, plan.Conflicts)
	for _, action := range plan.Actions {
		if action.Action == syncmodel.ActionConflict {
			paths = append(paths, displaySyncPath(action.Path))
			if len(paths) == 3 {
				break
			}
		}
	}
	message := fmt.Sprintf(
		"%d synchronization conflict(s): %s; run with --dry-run to inspect the plan, "+
			"then use --overwrite only if the source should replace the destination",
		plan.Conflicts, strings.Join(paths, ", "),
	)
	return apperror.Wrap(
		apperror.KindConflict, "sync plan", errors.New(message),
	)
}

func writeSyncResult(options Options, result syncResult) error {
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "sync-plan", result)
	}
	if result.Direction == syncmodel.Bidirectional {
		if _, err := fmt.Fprintf(
			options.Out,
			"Sync bidirectional: %s <-> %s\n",
			result.LocalRoot, result.RemoteRoot,
		); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintf(
		options.Out,
		"Sync %s: %s -> %s\n",
		result.Direction, syncSource(result), syncDestination(result),
	); err != nil {
		return err
	}
	writer := tabwriter.NewWriter(options.Out, 0, 4, 2, ' ', 0)
	if result.Direction == syncmodel.Bidirectional {
		if _, err := fmt.Fprintln(
			writer, "ACTION\tTARGET\tPATH\tREASON",
		); err != nil {
			return err
		}
		for _, action := range result.Actions {
			if _, err := fmt.Fprintf(
				writer, "%s\t%s\t%s\t%s\n",
				action.Action, action.Target,
				displaySyncActionPath(action), action.Reason,
			); err != nil {
				return err
			}
		}
	} else {
		if _, err := fmt.Fprintln(writer, "ACTION\tPATH\tREASON"); err != nil {
			return err
		}
		for _, action := range result.Actions {
			if _, err := fmt.Fprintf(
				writer, "%s\t%s\t%s\n",
				action.Action, displaySyncPath(action.Path), action.Reason,
			); err != nil {
				return err
			}
		}
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	status := "planned"
	if result.Applied {
		status = "applied"
	}
	_, err := fmt.Fprintf(
		options.Out,
		"%s: %d transfer(s), %d move(s), %d conflict copy/copies, %d deletion(s), %d conflict(s)\n",
		status, result.Transfers, result.Moves, result.Copies,
		result.Deletions, result.Conflicts,
	)
	return err
}

func displaySyncActionPath(action syncmodel.Action) string {
	if action.Action == syncmodel.ActionMove {
		return displaySyncPath(action.FromPath) + " -> " + displaySyncPath(action.Path)
	}
	return displaySyncPath(action.Path)
}

func syncSource(result syncResult) string {
	if result.Direction == syncmodel.Push {
		return result.LocalRoot
	}
	return result.RemoteRoot
}

func syncDestination(result syncResult) string {
	if result.Direction == syncmodel.Push {
		return result.RemoteRoot
	}
	return result.LocalRoot
}

func displaySyncPath(relative string) string {
	if relative == "" {
		return "."
	}
	return relative
}
