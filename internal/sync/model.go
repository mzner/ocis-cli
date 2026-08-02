// Package sync builds deterministic one-way reconciliation plans without
// depending on local filesystems, WebDAV, Cobra, or persistence.
package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
)

// StateVersion is the current persisted synchronization-state schema.
const StateVersion = 2

// Direction identifies a synchronization policy.
type Direction string

const (
	// Push treats the local tree as source and the remote tree as destination.
	Push Direction = "push"
	// Pull treats the remote tree as source and the local tree as destination.
	Pull Direction = "pull"
	// Bidirectional reconciles local and remote changes through a saved baseline.
	Bidirectional Direction = "bidirectional"
)

// Binding prevents state from being reused for another account, Space, root,
// or direction.
type Binding struct {
	Profile    string    `json:"profile"`
	AccountID  string    `json:"accountId"`
	SpaceID    string    `json:"spaceId,omitempty"`
	Direction  Direction `json:"direction"`
	LocalRoot  string    `json:"localRoot"`
	RemoteRoot string    `json:"remoteRoot"`
	Includes   []string  `json:"includes,omitempty"`
	Excludes   []string  `json:"excludes,omitempty"`
}

// Key returns the opaque stable persistence key for this exact binding.
func (binding Binding) Key() string {
	data, _ := json.Marshal(binding)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Entry is a non-secret fingerprint for one relative path.
type Entry struct {
	Path     string `json:"path"`
	Type     string `json:"type"`
	Size     int64  `json:"size,omitempty"`
	Modified string `json:"modified,omitempty"`
	ETag     string `json:"etag,omitempty"`
	Checksum string `json:"checksum,omitempty"`
}

// Snapshot maps normalized slash-separated relative paths to fingerprints.
type Snapshot map[string]Entry

// Record stores the source and destination baseline after a successful sync.
type Record struct {
	Source      Entry `json:"source"`
	Destination Entry `json:"destination"`
}

// State is the versioned baseline persisted after a completely successful
// reconciliation.
type State struct {
	Version int               `json:"version"`
	Binding Binding           `json:"binding"`
	Entries map[string]Record `json:"entries"`
}

// ActionKind identifies one planned reconciliation step.
type ActionKind string

const (
	ActionCreateDirectory ActionKind = "create-directory"
	ActionTransfer        ActionKind = "transfer"
	ActionMove            ActionKind = "move"
	ActionCopy            ActionKind = "copy"
	ActionDelete          ActionKind = "delete"
	ActionSkip            ActionKind = "skip"
	ActionConflict        ActionKind = "conflict"
)

// Action describes one relative-path operation.
type Action struct {
	Action      ActionKind `json:"action"`
	Path        string     `json:"path"`
	FromPath    string     `json:"fromPath,omitempty"`
	Target      Side       `json:"target,omitempty"`
	Type        string     `json:"type,omitempty"`
	Reason      string     `json:"reason,omitempty"`
	Replace     bool       `json:"replace,omitempty"`
	Source      Entry      `json:"source,omitempty"`
	Destination Entry      `json:"destination,omitempty"`
}

// Plan is a stable, ordered one-way reconciliation plan.
type Plan struct {
	Direction Direction `json:"direction"`
	Actions   []Action  `json:"actions"`
	Conflicts int       `json:"conflicts"`
	Transfers int       `json:"transfers"`
	Moves     int       `json:"moves"`
	Copies    int       `json:"copies"`
	Deletions int       `json:"deletions"`
}

// Options controls destructive planning and path selection.
type Options struct {
	Delete    bool
	Overwrite bool
	Includes  []string
	Excludes  []string
}

// Build constructs a deterministic plan without mutating either side.
func Build(
	direction Direction,
	source, destination Snapshot,
	previous *State,
	options Options,
) (Plan, error) {
	if direction != Push && direction != Pull {
		return Plan{}, fmt.Errorf("unknown sync direction %q", direction)
	}
	var err error
	source, err = Filter(source, options.Includes, options.Excludes)
	if err != nil {
		return Plan{}, err
	}
	destination, err = Filter(
		destination, options.Includes, options.Excludes,
	)
	if err != nil {
		return Plan{}, err
	}

	paths := unionPaths(source, destination)
	actions := make([]Action, 0, len(paths))
	for _, relative := range paths {
		currentSource, sourceExists := source[relative]
		currentDestination, destinationExists := destination[relative]
		var baseline Record
		baselineExists := false
		if previous != nil {
			baseline, baselineExists = previous.Entries[relative]
		}
		action := planPath(
			relative,
			currentSource, sourceExists,
			currentDestination, destinationExists,
			baseline, baselineExists,
			options,
		)
		actions = append(actions, action)
	}
	sortActions(actions)

	plan := Plan{Direction: direction, Actions: actions}
	for _, action := range actions {
		switch action.Action {
		case ActionConflict:
			plan.Conflicts++
		case ActionTransfer:
			plan.Transfers++
		case ActionMove:
			plan.Moves++
		case ActionCopy:
			plan.Copies++
		case ActionDelete:
			plan.Deletions++
		}
	}
	return plan, nil
}

func planPath(
	relative string,
	source Entry,
	sourceExists bool,
	destination Entry,
	destinationExists bool,
	baseline Record,
	baselineExists bool,
	options Options,
) Action {
	action := Action{
		Path: relative, Source: source, Destination: destination,
	}
	switch {
	case sourceExists && !destinationExists:
		action.Type = source.Type
		if baselineExists && entryPresent(baseline.Destination) &&
			!options.Overwrite {
			action.Action = ActionConflict
			action.Reason = "destination was deleted after the previous sync"
			return action
		}
		if source.Type == "directory" {
			action.Action = ActionCreateDirectory
			action.Reason = "destination directory is missing"
		} else {
			action.Action = ActionTransfer
			action.Reason = "destination file is missing"
		}
		return action

	case !sourceExists && destinationExists:
		action.Type = destination.Type
		if !options.Delete {
			action.Action = ActionSkip
			action.Reason = "destination-only item kept; use --delete to remove it"
			return action
		}
		if baselineExists &&
			!destination.Equal(baseline.Destination) &&
			!options.Overwrite {
			action.Action = ActionConflict
			action.Reason = "destination changed after the source was removed"
			return action
		}
		action.Action = ActionDelete
		action.Reason = "destination-only item"
		return action

	case sourceExists && destinationExists:
		action.Type = source.Type
		if source.Equal(destination) {
			action.Action = ActionSkip
			action.Reason = "source and destination match"
			return action
		}
		if source.Type != destination.Type {
			if !options.Overwrite {
				action.Action = ActionConflict
				action.Reason = "source and destination types differ"
				return action
			}
			if source.Type == "file" &&
				destination.Type == "directory" &&
				!options.Delete {
				action.Action = ActionConflict
				action.Reason = "replacing a directory with a file also requires --delete"
				return action
			}
			action.Replace = true
			if source.Type == "directory" {
				action.Action = ActionCreateDirectory
			} else {
				action.Action = ActionTransfer
			}
			action.Reason = "replace destination with source type"
			return action
		}
		if !baselineExists {
			if !options.Overwrite {
				action.Action = ActionConflict
				action.Reason = "different pre-existing source and destination"
				return action
			}
			action.Action = ActionTransfer
			action.Reason = "overwrite different pre-existing destination"
			return action
		}

		sourceChanged := !source.Equal(baseline.Source)
		destinationChanged := !destination.Equal(baseline.Destination)
		switch {
		case sourceChanged && !destinationChanged:
			action.Action = ActionTransfer
			action.Reason = "source changed"
		case !options.Overwrite:
			action.Action = ActionConflict
			switch {
			case sourceChanged && destinationChanged:
				action.Reason = "source and destination both changed"
			case destinationChanged:
				action.Reason = "destination changed independently"
			default:
				action.Reason = "current trees differ from the saved baseline"
			}
		default:
			action.Action = ActionTransfer
			action.Reason = "overwrite destination conflict"
		}
		return action

	default:
		panic("unreachable empty sync path")
	}
}

// Equal reports whether two entries represent the same current content.
func (entry Entry) Equal(other Entry) bool {
	if !entryPresent(entry) || !entryPresent(other) ||
		entry.Type != other.Type {
		return false
	}
	if entry.Type == "directory" {
		return true
	}
	if entry.Checksum != "" && other.Checksum != "" {
		return strings.EqualFold(entry.Checksum, other.Checksum)
	}
	if entry.ETag != "" && other.ETag != "" {
		return entry.ETag == other.ETag
	}
	return entry.Size == other.Size && timestampsEquivalent(
		entry.Modified, other.Modified,
	)
}

// Converged reports whether a verified transfer can be represented as the
// same content on both filesystems. Strong comparable fingerprints take
// precedence; equal size is the final fallback after atomic transfer
// verification because local and remote modification times need not match.
func (entry Entry) Converged(other Entry) bool {
	if !entryPresent(entry) || !entryPresent(other) || entry.Type != other.Type {
		return false
	}
	if entry.Type == "directory" {
		return true
	}
	if entry.Checksum != "" && other.Checksum != "" {
		return strings.EqualFold(entry.Checksum, other.Checksum)
	}
	if entry.ETag != "" && other.ETag != "" {
		return entry.ETag == other.ETag
	}
	return entry.Size == other.Size
}

func timestampsEquivalent(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	if left == right {
		return true
	}
	leftTime, leftErr := time.Parse(time.RFC3339Nano, left)
	rightTime, rightErr := time.Parse(time.RFC3339Nano, right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	difference := leftTime.Sub(rightTime)
	if difference < 0 {
		difference = -difference
	}
	return difference <= 2*time.Second
}

// NewState creates the baseline saved after a successful reconciliation.
func NewState(
	binding Binding, source, destination Snapshot,
) State {
	entries := make(map[string]Record)
	for _, relative := range unionPaths(source, destination) {
		entries[relative] = Record{
			Source: source[relative], Destination: destination[relative],
		}
	}
	return State{
		Version: StateVersion, Binding: binding, Entries: entries,
	}
}

// Filter applies slash-based include and exclude globs. Patterns without a
// slash match a path's basename. Parent directories of included files remain
// present so a plan can create or traverse them.
func Filter(
	snapshot Snapshot, includes, excludes []string,
) (Snapshot, error) {
	if err := validatePatterns(includes); err != nil {
		return nil, fmt.Errorf("include pattern: %w", err)
	}
	if err := validatePatterns(excludes); err != nil {
		return nil, fmt.Errorf("exclude pattern: %w", err)
	}
	selected := make(map[string]bool, len(snapshot))
	if len(includes) == 0 {
		for relative := range snapshot {
			selected[relative] = true
		}
	} else {
		for relative := range snapshot {
			if matchesAny(relative, includes) {
				if _, exists := snapshot[""]; exists {
					selected[""] = true
				}
				for current := relative; ; current = path.Dir(current) {
					if _, exists := snapshot[current]; exists {
						selected[current] = true
					}
					if current == "" || current == "." {
						break
					}
				}
			}
		}
	}
	result := make(Snapshot)
	for relative, entry := range snapshot {
		if !selected[relative] || excluded(relative, excludes) {
			continue
		}
		result[relative] = entry
	}
	return result, nil
}

// NormalizePatterns validates and canonicalizes filter policy for stable
// persistence keys. Pattern order and duplicates do not change semantics.
func NormalizePatterns(
	includes, excludes []string,
) ([]string, []string, error) {
	if err := validatePatterns(includes); err != nil {
		return nil, nil, fmt.Errorf("include pattern: %w", err)
	}
	if err := validatePatterns(excludes); err != nil {
		return nil, nil, fmt.Errorf("exclude pattern: %w", err)
	}
	return sortedUnique(includes), sortedUnique(excludes), nil
}

func sortedUnique(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	write := 0
	for _, value := range result {
		if write > 0 && result[write-1] == value {
			continue
		}
		result[write] = value
		write++
	}
	return result[:write]
}

func excluded(relative string, patterns []string) bool {
	for current := relative; ; current = path.Dir(current) {
		if matchesAny(current, patterns) {
			return true
		}
		if current == "" || current == "." {
			return false
		}
	}
}

func matchesAny(relative string, patterns []string) bool {
	for _, pattern := range patterns {
		target := relative
		if !strings.Contains(pattern, "/") {
			target = path.Base(relative)
		}
		matched, _ := path.Match(pattern, target)
		if matched {
			return true
		}
	}
	return false
}

func validatePatterns(patterns []string) error {
	for _, pattern := range patterns {
		if strings.TrimSpace(pattern) == "" {
			return errors.New("pattern cannot be empty")
		}
		if _, err := path.Match(pattern, "value"); err != nil {
			return fmt.Errorf("%q: %w", pattern, err)
		}
	}
	return nil
}

func unionPaths(left, right Snapshot) []string {
	values := make(map[string]struct{}, len(left)+len(right))
	for relative := range left {
		values[relative] = struct{}{}
	}
	for relative := range right {
		values[relative] = struct{}{}
	}
	result := make([]string, 0, len(values))
	for relative := range values {
		result = append(result, relative)
	}
	sort.Strings(result)
	return result
}

func entryPresent(entry Entry) bool {
	return entry.Type != ""
}

func sortActions(actions []Action) {
	sort.SliceStable(actions, func(i, j int) bool {
		left, right := actions[i], actions[j]
		leftRank, rightRank := actionRank(left), actionRank(right)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		leftDepth := strings.Count(left.Path, "/")
		rightDepth := strings.Count(right.Path, "/")
		if left.Action == ActionDelete {
			if leftDepth != rightDepth {
				return leftDepth > rightDepth
			}
		} else if leftDepth != rightDepth {
			return leftDepth < rightDepth
		}
		return left.Path < right.Path
	})
}

func actionRank(action Action) int {
	switch action.Action {
	case ActionConflict:
		return 0
	case ActionDelete:
		return 1
	case ActionCreateDirectory:
		return 2
	case ActionTransfer:
		return 3
	case ActionMove:
		return 3
	case ActionCopy:
		return 3
	default:
		return 4
	}
}
