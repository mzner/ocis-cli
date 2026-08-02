package sync

import (
	"fmt"
	"sort"
)

type moveCandidate struct {
	index int
	entry Entry
}

// Side identifies the target tree for a bidirectional action.
type Side string

const (
	// Local targets the local filesystem tree.
	Local Side = "local"
	// Remote targets the remote WebDAV tree.
	Remote Side = "remote"
)

// BuildBidirectional constructs a deterministic three-way plan without
// mutating either tree. State records are interpreted as local in Source and
// remote in Destination.
func BuildBidirectional(
	local, remote Snapshot,
	previous *State,
	options Options,
) (Plan, error) {
	var err error
	local, err = Filter(local, options.Includes, options.Excludes)
	if err != nil {
		return Plan{}, err
	}
	remote, err = Filter(remote, options.Includes, options.Excludes)
	if err != nil {
		return Plan{}, err
	}
	if err := ValidatePathIdentity(local, remote); err != nil {
		return Plan{}, fmt.Errorf("unsafe path identity: %w", err)
	}

	paths := bidirectionalPaths(local, remote, previous)
	actions := make([]Action, 0, len(paths))
	for _, relative := range paths {
		localEntry, localExists := local[relative]
		remoteEntry, remoteExists := remote[relative]
		var baseline Record
		baselineExists := false
		if previous != nil {
			baseline, baselineExists = previous.Entries[relative]
		}
		actions = append(actions, planBidirectionalPath(
			relative,
			localEntry, localExists,
			remoteEntry, remoteExists,
			baseline, baselineExists,
		))
	}
	protectChangedBidirectionalSubtrees(
		actions, local, remote, previous,
	)
	detectBidirectionalFileMoves(actions, local, remote, previous)
	sortActions(actions)

	plan := Plan{Direction: Bidirectional, Actions: actions}
	for _, action := range actions {
		switch action.Action {
		case ActionConflict:
			plan.Conflicts++
		case ActionTransfer:
			plan.Transfers++
		case ActionMove:
			plan.Moves++
		case ActionDelete:
			plan.Deletions++
		}
	}
	return plan, nil
}

func detectBidirectionalFileMoves(
	actions []Action,
	local Snapshot,
	remote Snapshot,
	previous *State,
) {
	if previous == nil {
		return
	}
	for _, target := range []Side{Local, Remote} {
		removed := make([]moveCandidate, 0)
		added := make([]moveCandidate, 0)
		for index := range actions {
			action := actions[index]
			if action.Target != target || action.Type != "file" {
				continue
			}
			switch action.Action {
			case ActionDelete:
				baseline := previous.Entries[action.Path]
				entry := baseline.Destination
				if target == Remote {
					entry = baseline.Source
				}
				if entryPresent(entry) {
					removed = append(removed, moveCandidate{index: index, entry: entry})
				}
			case ActionTransfer:
				if !entryPresent(action.Destination) {
					added = append(added, moveCandidate{index: index, entry: action.Source})
				}
			}
		}
		for _, destination := range added {
			matches := make([]moveCandidate, 0, 1)
			for _, source := range removed {
				if destination.entry.Equal(source.entry) {
					matches = append(matches, source)
				}
			}
			if len(matches) != 1 || moveFingerprintMatchesOtherAddition(
				added, matches[0].entry,
			) {
				continue
			}
			from := actions[matches[0].index]
			to := &actions[destination.index]
			if target == Local {
				if _, exists := local[to.Path]; exists {
					continue
				}
			} else if _, exists := remote[to.Path]; exists {
				continue
			}
			to.Action = ActionMove
			to.FromPath = from.Path
			to.Destination = from.Destination
			to.Reason = fmt.Sprintf("rename detected from %s", from.Path)
			actions[matches[0].index].Action = ActionSkip
			actions[matches[0].index].Target = ""
			actions[matches[0].index].Reason = "handled by rename to " + to.Path
		}
	}
}

func moveFingerprintMatchesOtherAddition(
	additions []moveCandidate,
	fingerprint Entry,
) bool {
	matches := 0
	for _, addition := range additions {
		if addition.entry.Equal(fingerprint) {
			matches++
		}
	}
	return matches != 1
}

func protectChangedBidirectionalSubtrees(
	actions []Action,
	local Snapshot,
	remote Snapshot,
	previous *State,
) {
	if previous == nil {
		return
	}
	conflictParents := make([]string, 0)
	for index := range actions {
		action := &actions[index]
		destructiveDirectory :=
			(action.Action == ActionDelete && action.Type == "directory") ||
				(action.Replace && action.Destination.Type == "directory")
		if !destructiveDirectory {
			continue
		}
		targetSnapshot := remote
		baselineSide := Remote
		if action.Target == Local {
			targetSnapshot = local
			baselineSide = Local
		}
		if !bidirectionalSubtreeChanged(
			action.Path, targetSnapshot, previous, baselineSide,
		) {
			continue
		}
		changedSide := action.Target
		action.Action = ActionConflict
		action.Target = ""
		action.Replace = false
		action.Reason = fmt.Sprintf(
			"%s subtree changed after the other side removed or replaced its directory",
			changedSide,
		)
		conflictParents = append(conflictParents, action.Path)
	}
	if len(conflictParents) == 0 {
		return
	}
	sort.Slice(conflictParents, func(i, j int) bool {
		leftDepth := pathDepth(conflictParents[i])
		rightDepth := pathDepth(conflictParents[j])
		if leftDepth != rightDepth {
			return leftDepth < rightDepth
		}
		return conflictParents[i] < conflictParents[j]
	})
	for index := range actions {
		action := &actions[index]
		if action.Action == ActionConflict {
			continue
		}
		for _, parent := range conflictParents {
			if !isDescendant(action.Path, parent) {
				continue
			}
			action.Action = ActionSkip
			action.Target = ""
			action.Replace = false
			action.Reason = "blocked by directory conflict at " + parent
			break
		}
	}
}

func bidirectionalSubtreeChanged(
	directory string,
	current Snapshot,
	previous *State,
	side Side,
) bool {
	prefix := directory + "/"
	paths := make(map[string]struct{})
	for relative := range current {
		if len(relative) > len(prefix) && relative[:len(prefix)] == prefix {
			paths[relative] = struct{}{}
		}
	}
	for relative := range previous.Entries {
		if len(relative) > len(prefix) && relative[:len(prefix)] == prefix {
			paths[relative] = struct{}{}
		}
	}
	for relative := range paths {
		entry, exists := current[relative]
		baseline := previous.Entries[relative].Destination
		if side == Local {
			baseline = previous.Entries[relative].Source
		}
		if !matchesBaseline(entry, exists, baseline) {
			return true
		}
	}
	return false
}

func pathDepth(relative string) int {
	depth := 0
	for _, character := range relative {
		if character == '/' {
			depth++
		}
	}
	return depth
}

func isDescendant(relative string, parent string) bool {
	if parent == "" {
		return relative != ""
	}
	prefix := parent + "/"
	return len(relative) > len(prefix) && relative[:len(prefix)] == prefix
}

func planBidirectionalPath(
	relative string,
	local Entry,
	localExists bool,
	remote Entry,
	remoteExists bool,
	baseline Record,
	baselineExists bool,
) Action {
	if relative == "" {
		return planBidirectionalRoot(
			local, localExists, remote, remoteExists,
		)
	}
	if !baselineExists {
		return planInitialBidirectionalPath(
			relative,
			local, localExists,
			remote, remoteExists,
		)
	}

	localChanged := !matchesBaseline(
		local, localExists, baseline.Source,
	)
	remoteChanged := !matchesBaseline(
		remote, remoteExists, baseline.Destination,
	)
	if entriesMatch(local, localExists, remote, remoteExists) {
		reason := "local and remote match"
		switch {
		case !localExists:
			reason = "deleted on both sides"
		case localChanged && remoteChanged:
			reason = "both sides changed identically"
		}
		return bidirectionalAction(
			ActionSkip, relative, "", reason,
			local, remote, false,
		)
	}

	switch {
	case localChanged && !remoteChanged:
		return propagateBidirectionalChange(
			relative, Local,
			local, localExists,
			remote, remoteExists,
		)
	case !localChanged && remoteChanged:
		return propagateBidirectionalChange(
			relative, Remote,
			remote, remoteExists,
			local, localExists,
		)
	case localChanged && remoteChanged:
		return bidirectionalAction(
			ActionConflict, relative, "",
			"local and remote changed differently",
			local, remote, false,
		)
	default:
		return bidirectionalAction(
			ActionConflict, relative, "",
			"local and remote differ from an inconsistent saved baseline",
			local, remote, false,
		)
	}
}

func planBidirectionalRoot(
	local Entry,
	localExists bool,
	remote Entry,
	remoteExists bool,
) Action {
	switch {
	case localExists && remoteExists:
		if local.Type != "directory" || remote.Type != "directory" {
			return bidirectionalAction(
				ActionConflict, "", "",
				"sync roots must both be directories",
				local, remote, false,
			)
		}
		return bidirectionalAction(
			ActionSkip, "", "", "sync roots exist",
			local, remote, false,
		)
	case localExists:
		return bidirectionalAction(
			ActionCreateDirectory, "", Remote,
			"remote sync root is missing",
			local, remote, false,
		)
	case remoteExists:
		return bidirectionalAction(
			ActionCreateDirectory, "", Local,
			"local sync root is missing",
			remote, local, false,
		)
	default:
		return bidirectionalAction(
			ActionConflict, "", "", "both sync roots are missing",
			local, remote, false,
		)
	}
}

func planInitialBidirectionalPath(
	relative string,
	local Entry,
	localExists bool,
	remote Entry,
	remoteExists bool,
) Action {
	switch {
	case localExists && !remoteExists:
		return propagateBidirectionalChange(
			relative, Local, local, true, remote, false,
		)
	case !localExists && remoteExists:
		return propagateBidirectionalChange(
			relative, Remote, remote, true, local, false,
		)
	case entriesMatch(local, localExists, remote, remoteExists):
		return bidirectionalAction(
			ActionSkip, relative, "", "local and remote match",
			local, remote, false,
		)
	default:
		return bidirectionalAction(
			ActionConflict, relative, "",
			"different pre-existing local and remote entries",
			local, remote, false,
		)
	}
}

func propagateBidirectionalChange(
	relative string,
	changedSide Side,
	source Entry,
	sourceExists bool,
	destination Entry,
	destinationExists bool,
) Action {
	target := oppositeSide(changedSide)
	if !sourceExists {
		return bidirectionalAction(
			ActionDelete, relative, target,
			fmt.Sprintf("%s deletion", changedSide),
			source, destination, false,
		)
	}
	action := ActionTransfer
	if source.Type == "directory" {
		action = ActionCreateDirectory
	}
	return bidirectionalAction(
		action, relative, target,
		fmt.Sprintf("%s-only change", changedSide),
		source, destination,
		destinationExists && source.Type != destination.Type,
	)
}

func bidirectionalAction(
	kind ActionKind,
	relative string,
	target Side,
	reason string,
	source Entry,
	destination Entry,
	replace bool,
) Action {
	entryType := source.Type
	if entryType == "" {
		entryType = destination.Type
	}
	return Action{
		Action: kind, Path: relative, Target: target,
		Type: entryType, Reason: reason, Replace: replace,
		Source: source, Destination: destination,
	}
}

func entriesMatch(
	left Entry,
	leftExists bool,
	right Entry,
	rightExists bool,
) bool {
	if leftExists != rightExists {
		return false
	}
	return !leftExists || left.Equal(right)
}

func matchesBaseline(
	current Entry,
	currentExists bool,
	baseline Entry,
) bool {
	baselineExists := entryPresent(baseline)
	if currentExists != baselineExists {
		return false
	}
	return !currentExists || current.Equal(baseline)
}

func oppositeSide(side Side) Side {
	if side == Local {
		return Remote
	}
	return Local
}

func bidirectionalPaths(
	local Snapshot,
	remote Snapshot,
	previous *State,
) []string {
	values := make(map[string]struct{}, len(local)+len(remote))
	for relative := range local {
		values[relative] = struct{}{}
	}
	for relative := range remote {
		values[relative] = struct{}{}
	}
	if previous != nil {
		for relative := range previous.Entries {
			values[relative] = struct{}{}
		}
	}
	result := make([]string, 0, len(values))
	for relative := range values {
		result = append(result, relative)
	}
	sort.Strings(result)
	return result
}
