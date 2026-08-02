package sync

import (
	"reflect"
	"testing"
)

func TestBuildInitialPlanFailsClosedOnDifferentExistingFile(t *testing.T) {
	source := Snapshot{
		"":        directory(""),
		"new.txt": file("new.txt", "sha1:source"),
	}
	destination := Snapshot{
		"":        directory(""),
		"old.txt": file("old.txt", "sha1:old"),
		"new.txt": file("new.txt", "sha1:destination"),
	}
	plan, err := Build(Push, source, destination, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Conflicts != 1 || plan.Deletions != 0 {
		t.Fatalf("plan: %#v", plan)
	}
	assertAction(t, plan, "new.txt", ActionConflict, false)
	assertAction(t, plan, "old.txt", ActionSkip, false)
}

func TestBuildTransfersSourceOnlyChangeAndConflictsDestinationChange(
	t *testing.T,
) {
	baselineSource := Snapshot{
		"":           directory(""),
		"source.txt": file("source.txt", "sha1:old-source"),
		"remote.txt": file("remote.txt", "sha1:old-remote"),
	}
	baselineDestination := cloneSnapshot(baselineSource)
	previous := NewState(
		Binding{Direction: Push}, baselineSource, baselineDestination,
	)
	source := cloneSnapshot(baselineSource)
	source["source.txt"] = file("source.txt", "sha1:new-source")
	destination := cloneSnapshot(baselineDestination)
	destination["remote.txt"] = file("remote.txt", "sha1:new-remote")

	plan, err := Build(Push, source, destination, &previous, Options{})
	if err != nil {
		t.Fatal(err)
	}
	assertAction(t, plan, "source.txt", ActionTransfer, false)
	assertAction(t, plan, "remote.txt", ActionConflict, false)

	overwrite, err := Build(
		Push, source, destination, &previous,
		Options{Overwrite: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	assertAction(t, overwrite, "remote.txt", ActionTransfer, false)
}

func TestBuildDeletionRequiresExplicitPolicyAndProtectsChangedDestination(
	t *testing.T,
) {
	baselineSource := Snapshot{
		"":            directory(""),
		"removed.txt": file("removed.txt", "sha1:old"),
	}
	baselineDestination := cloneSnapshot(baselineSource)
	previous := NewState(
		Binding{Direction: Pull}, baselineSource, baselineDestination,
	)
	source := Snapshot{"": directory("")}
	destination := cloneSnapshot(baselineDestination)

	kept, err := Build(Pull, source, destination, &previous, Options{})
	if err != nil {
		t.Fatal(err)
	}
	assertAction(t, kept, "removed.txt", ActionSkip, false)

	deleted, err := Build(
		Pull, source, destination, &previous, Options{Delete: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	assertAction(t, deleted, "removed.txt", ActionDelete, false)

	destination["removed.txt"] = file("removed.txt", "sha1:changed")
	conflict, err := Build(
		Pull, source, destination, &previous, Options{Delete: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	assertAction(t, conflict, "removed.txt", ActionConflict, false)
}

func TestBuildCreatesMissingTreeInStableOrder(t *testing.T) {
	source := Snapshot{
		"":                 directory(""),
		"docs":             directory("docs"),
		"docs/nested":      directory("docs/nested"),
		"docs/nested/a.md": file("docs/nested/a.md", "sha1:a"),
	}
	plan, err := Build(Push, source, Snapshot{}, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, action := range plan.Actions {
		if action.Action != ActionSkip {
			paths = append(paths, action.Path)
		}
	}
	want := []string{"", "docs", "docs/nested", "docs/nested/a.md"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths: got %#v, want %#v", paths, want)
	}
}

func TestFilterIncludesParentsAndExcludesSubtrees(t *testing.T) {
	snapshot := Snapshot{
		"":                    directory(""),
		"docs":                directory("docs"),
		"docs/report.md":      file("docs/report.md", "sha1:report"),
		"docs/private":        directory("docs/private"),
		"docs/private/key.md": file("docs/private/key.md", "sha1:key"),
		"image.png":           file("image.png", "sha1:image"),
	}
	filtered, err := Filter(
		snapshot, []string{"*.md"}, []string{"private"},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := Snapshot{
		"":               directory(""),
		"docs":           directory("docs"),
		"docs/report.md": file("docs/report.md", "sha1:report"),
	}
	if !reflect.DeepEqual(filtered, want) {
		t.Fatalf("filtered: %#v", filtered)
	}
	if _, err := Filter(snapshot, []string{"["}, nil); err == nil {
		t.Fatal("invalid glob accepted")
	}
}

func TestEntryEqualityPrefersChecksumThenETag(t *testing.T) {
	local := Entry{Type: "file", Size: 4, Checksum: "SHA1:ABCD"}
	remote := Entry{
		Type: "file", Size: 4, Checksum: "sha1:abcd", ETag: `"remote"`,
	}
	if !local.Equal(remote) {
		t.Fatal("matching checksums were not equal")
	}
	if !(Entry{Type: "file", ETag: `"same"`}).
		Equal(Entry{Type: "file", ETag: `"same"`}) {
		t.Fatal("matching ETags were not equal")
	}
	if (Entry{Type: "file", Size: 4}).
		Equal(Entry{Type: "file", Size: 4}) {
		t.Fatal("size alone must not establish equality")
	}
}

func TestEntryEqualityToleratesTimestampPrecisionButNotClockDrift(t *testing.T) {
	left := Entry{
		Type: "file", Size: 4, Modified: "2026-08-01T10:00:00Z",
	}
	right := Entry{
		Type: "file", Size: 4, Modified: "2026-08-01T10:00:02Z",
	}
	if !left.Equal(right) {
		t.Fatal("two-second timestamp precision difference was not tolerated")
	}
	right.Modified = "2026-08-01T10:00:03Z"
	if left.Equal(right) {
		t.Fatal("three-second timestamp drift was treated as equal")
	}
	if !left.Converged(Entry{Type: "file", Size: 4}) {
		t.Fatal("verified equal-size transfer did not converge")
	}
}

func TestNormalizePatterns(t *testing.T) {
	includes, excludes, err := NormalizePatterns(
		[]string{"*.txt", "docs/*", "*.txt"},
		[]string{"*.tmp", "*.bak", "*.tmp"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(includes, []string{"*.txt", "docs/*"}) {
		t.Fatalf("includes=%#v", includes)
	}
	if !reflect.DeepEqual(excludes, []string{"*.bak", "*.tmp"}) {
		t.Fatalf("excludes=%#v", excludes)
	}
	if _, _, err := NormalizePatterns([]string{"["}, nil); err == nil {
		t.Fatal("invalid include accepted")
	}
	if _, _, err := NormalizePatterns(nil, []string{"["}); err == nil {
		t.Fatal("invalid exclude accepted")
	}
}

func assertAction(
	t *testing.T,
	plan Plan,
	path string,
	kind ActionKind,
	replace bool,
) {
	t.Helper()
	for _, action := range plan.Actions {
		if action.Path == path {
			if action.Action != kind || action.Replace != replace {
				t.Fatalf("%s: %#v", path, action)
			}
			return
		}
	}
	t.Fatalf("missing action for %q in %#v", path, plan)
}

func directory(path string) Entry {
	return Entry{Path: path, Type: "directory"}
}

func file(path, checksum string) Entry {
	return Entry{
		Path: path, Type: "file", Size: 4, Checksum: checksum,
	}
}

func cloneSnapshot(value Snapshot) Snapshot {
	result := make(Snapshot, len(value))
	for key, entry := range value {
		result[key] = entry
	}
	return result
}
