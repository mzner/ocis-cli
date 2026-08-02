package sync

import "testing"

func TestBuildBidirectionalInitialPlan(t *testing.T) {
	local := Snapshot{
		"":             directory(""),
		"local.txt":    file("local.txt", "sha1:local"),
		"same.txt":     file("same.txt", "sha1:same"),
		"conflict.txt": file("conflict.txt", "sha1:local-conflict"),
	}
	remote := Snapshot{
		"":             directory(""),
		"remote.txt":   file("remote.txt", "sha1:remote"),
		"same.txt":     file("same.txt", "sha1:same"),
		"conflict.txt": file("conflict.txt", "sha1:remote-conflict"),
	}
	plan, err := BuildBidirectional(local, remote, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Direction != Bidirectional || plan.Transfers != 2 ||
		plan.Deletions != 0 || plan.Conflicts != 1 {
		t.Fatalf("plan: %#v", plan)
	}
	assertBidirectionalAction(
		t, plan, "local.txt", ActionTransfer, Remote,
	)
	assertBidirectionalAction(
		t, plan, "remote.txt", ActionTransfer, Local,
	)
	assertBidirectionalAction(
		t, plan, "same.txt", ActionSkip, "",
	)
	assertBidirectionalAction(
		t, plan, "conflict.txt", ActionConflict, "",
	)
}

func TestBuildBidirectionalThreeWayChangesAndTombstones(t *testing.T) {
	baseline := Snapshot{
		"":                  directory(""),
		"local-change.txt":  file("local-change.txt", "sha1:old-local"),
		"remote-change.txt": file("remote-change.txt", "sha1:old-remote"),
		"local-delete.txt":  file("local-delete.txt", "sha1:old-delete-local"),
		"remote-delete.txt": file("remote-delete.txt", "sha1:old-delete-remote"),
		"same-change.txt":   file("same-change.txt", "sha1:old-same"),
		"conflict.txt":      file("conflict.txt", "sha1:old-conflict"),
		"both-delete.txt":   file("both-delete.txt", "sha1:old-both-delete"),
	}
	previous := NewState(
		Binding{Direction: Bidirectional}, baseline, baseline,
	)
	local := cloneSnapshot(baseline)
	remote := cloneSnapshot(baseline)
	local["local-change.txt"] = file("local-change.txt", "sha1:new-local")
	remote["remote-change.txt"] = file("remote-change.txt", "sha1:new-remote")
	delete(local, "local-delete.txt")
	delete(remote, "remote-delete.txt")
	local["same-change.txt"] = file("same-change.txt", "sha1:new-same")
	remote["same-change.txt"] = file("same-change.txt", "sha1:new-same")
	local["conflict.txt"] = file("conflict.txt", "sha1:new-local-conflict")
	remote["conflict.txt"] = file("conflict.txt", "sha1:new-remote-conflict")
	delete(local, "both-delete.txt")
	delete(remote, "both-delete.txt")

	plan, err := BuildBidirectional(local, remote, &previous, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Transfers != 2 || plan.Deletions != 2 || plan.Conflicts != 1 {
		t.Fatalf("plan: %#v", plan)
	}
	assertBidirectionalAction(
		t, plan, "local-change.txt", ActionTransfer, Remote,
	)
	assertBidirectionalAction(
		t, plan, "remote-change.txt", ActionTransfer, Local,
	)
	assertBidirectionalAction(
		t, plan, "local-delete.txt", ActionDelete, Remote,
	)
	assertBidirectionalAction(
		t, plan, "remote-delete.txt", ActionDelete, Local,
	)
	assertBidirectionalAction(
		t, plan, "same-change.txt", ActionSkip, "",
	)
	assertBidirectionalAction(
		t, plan, "both-delete.txt", ActionSkip, "",
	)
	assertBidirectionalAction(
		t, plan, "conflict.txt", ActionConflict, "",
	)
}

func TestBuildBidirectionalRestoresMissingRoot(t *testing.T) {
	plan, err := BuildBidirectional(
		Snapshot{"": directory("")}, Snapshot{}, nil, Options{},
	)
	if err != nil {
		t.Fatal(err)
	}
	assertBidirectionalAction(
		t, plan, "", ActionCreateDirectory, Remote,
	)
	if plan.Deletions != 0 {
		t.Fatalf("missing root planned as deletion: %#v", plan)
	}
}

func TestBuildBidirectionalFiltersBothTrees(t *testing.T) {
	local := Snapshot{
		"": directory(""), "keep.txt": file("keep.txt", "sha1:local"),
		"skip.bin": file("skip.bin", "sha1:local-bin"),
	}
	remote := Snapshot{
		"": directory(""), "other.txt": file("other.txt", "sha1:remote"),
		"skip.bin": file("skip.bin", "sha1:remote-bin"),
	}
	plan, err := BuildBidirectional(
		local, remote, nil, Options{Includes: []string{"*.txt"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	assertBidirectionalAction(
		t, plan, "keep.txt", ActionTransfer, Remote,
	)
	assertBidirectionalAction(
		t, plan, "other.txt", ActionTransfer, Local,
	)
	for _, action := range plan.Actions {
		if action.Path == "skip.bin" {
			t.Fatalf("excluded action remains: %#v", action)
		}
	}
}

func TestBuildBidirectionalProtectsChangedSubtreeFromDirectoryDeletion(
	t *testing.T,
) {
	baseline := Snapshot{
		"": directory(""), "docs": directory("docs"),
		"docs/old.txt": file("docs/old.txt", "sha1:old"),
	}
	previous := NewState(
		Binding{Direction: Bidirectional}, baseline, baseline,
	)
	local := Snapshot{"": directory("")}
	remote := cloneSnapshot(baseline)
	remote["docs/old.txt"] = file("docs/old.txt", "sha1:changed")
	remote["docs/new.txt"] = file("docs/new.txt", "sha1:new")

	plan, err := BuildBidirectional(local, remote, &previous, Options{})
	if err != nil {
		t.Fatal(err)
	}
	assertBidirectionalAction(
		t, plan, "docs", ActionConflict, "",
	)
	assertBidirectionalAction(
		t, plan, "docs/new.txt", ActionSkip, "",
	)
	for _, action := range plan.Actions {
		if action.Path == "docs" && action.Action == ActionDelete {
			t.Fatalf("changed subtree was deleted: %#v", plan)
		}
	}
}

func TestBuildBidirectionalDetectsUniqueFileRename(t *testing.T) {
	baseline := Snapshot{
		"": directory(""), "old.txt": file("old.txt", "sha1:content"),
	}
	previous := NewState(
		Binding{Direction: Bidirectional}, baseline, baseline,
	)
	local := Snapshot{
		"": directory(""), "new.txt": file("new.txt", "sha1:content"),
	}
	remote := cloneSnapshot(baseline)
	plan, err := BuildBidirectional(local, remote, &previous, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Moves != 1 || plan.Transfers != 0 || plan.Deletions != 0 {
		t.Fatalf("rename plan: %#v", plan)
	}
	for _, action := range plan.Actions {
		if action.Action == ActionMove {
			if action.FromPath != "old.txt" || action.Path != "new.txt" ||
				action.Target != Remote {
				t.Fatalf("move action: %#v", action)
			}
			return
		}
	}
	t.Fatalf("move action missing: %#v", plan)
}

func TestBuildBidirectionalDoesNotGuessAmbiguousRename(t *testing.T) {
	baseline := Snapshot{
		"":          directory(""),
		"old-a.txt": file("old-a.txt", "sha1:same"),
		"old-b.txt": file("old-b.txt", "sha1:same"),
	}
	previous := NewState(
		Binding{Direction: Bidirectional}, baseline, baseline,
	)
	local := Snapshot{
		"":          directory(""),
		"new-a.txt": file("new-a.txt", "sha1:same"),
		"new-b.txt": file("new-b.txt", "sha1:same"),
	}
	plan, err := BuildBidirectional(local, cloneSnapshot(baseline), &previous, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Moves != 0 || plan.Transfers != 2 || plan.Deletions != 2 {
		t.Fatalf("ambiguous rename plan: %#v", plan)
	}
}

func TestBuildBidirectionalRejectsCaseAndUnicodeCollisions(t *testing.T) {
	for _, remotePath := range []string{"readme.txt", "café.txt"} {
		localPath := "README.txt"
		if remotePath != "readme.txt" {
			localPath = "cafe\u0301.txt"
		}
		_, err := BuildBidirectional(
			Snapshot{"": directory(""), localPath: file(localPath, "sha1:a")},
			Snapshot{"": directory(""), remotePath: file(remotePath, "sha1:a")},
			nil, Options{},
		)
		if err == nil {
			t.Fatalf("accepted ambiguous paths %q and %q", localPath, remotePath)
		}
	}
}

func assertBidirectionalAction(
	t *testing.T,
	plan Plan,
	path string,
	kind ActionKind,
	target Side,
) {
	t.Helper()
	for _, action := range plan.Actions {
		if action.Path == path {
			if action.Action != kind || action.Target != target {
				t.Fatalf("%s: %#v", path, action)
			}
			return
		}
	}
	t.Fatalf("missing action for %q in %#v", path, plan)
}
