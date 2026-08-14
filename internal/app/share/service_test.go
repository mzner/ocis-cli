package share

import (
	"strings"
	"testing"

	"github.com/mzner/ocis-cli/internal/apperror"
	"github.com/mzner/ocis-cli/internal/graph"
	"github.com/mzner/ocis-cli/internal/sharing"
)

func TestRecipientSelection(t *testing.T) {
	candidates := []recipient{
		{ID: "one", DisplayName: "Alice", Username: "alice"},
		{ID: "two", DisplayName: "Alice", Username: "alice-two"},
	}
	selected, err := selectRecipient(candidates, "alice-two", "user", usageShare)
	if err != nil || selected.ID != "two" {
		t.Fatalf("selected=%#v error=%v", selected, err)
	}
	if _, err := selectRecipient(candidates, "Alice", "user", usageShare); err == nil ||
		!strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous error=%v", err)
	}
	if _, err := selectRecipient(nil, "missing", "user", usageShare); err == nil ||
		!apperror.IsKind(err, apperror.KindUsage) {
		t.Fatalf("missing error=%v", err)
	}
	selected, err = selectRecipient(candidates[:1], "partial", "user", usageShare)
	if err != nil || selected.ID != "one" {
		t.Fatalf("single selected=%#v error=%v", selected, err)
	}
}

func TestShareStateAndRoleHelpers(t *testing.T) {
	for value, want := range map[string]string{
		"": "current", " rejected ": "declined", "ALL": "all",
	} {
		if got := normalizeOverviewState(value); got != want {
			t.Fatalf("state %q = %q, want %q", value, got, want)
		}
	}
	for _, value := range []string{"", "all", "accepted", "pending", "declined"} {
		if _, _, err := receivedShareStateFilter(value); err != nil {
			t.Fatalf("state %q: %v", value, err)
		}
	}
	if _, _, err := receivedShareStateFilter("future"); err == nil {
		t.Fatal("future state accepted")
	}
	for permissions, want := range map[int]string{
		1: "read", 3: "edit", 4: "upload", 5: "upload", 15: "edit", 7: "7",
	} {
		if got := PermissionName(permissions); got != want {
			t.Fatalf("permission %d = %q, want %q", permissions, got, want)
		}
	}
	for value, want := range map[string]string{
		"Viewer": "view", "Can read": "view", "Editor": "edit",
		"Uploader": "upload", "Manager": "manage", "Custom": "",
	} {
		if got := canonicalShareRole(value); got != want {
			t.Fatalf("role %q = %q, want %q", value, got, want)
		}
	}
}

func TestOverviewAndSpaceHelpers(t *testing.T) {
	spaces := []graph.Drive{
		{ID: "personal", Name: "Home", DriveAlias: "personal/alice"},
		{ID: "project", Name: "Project", DriveAlias: "project/work"},
	}
	selected, err := resolveSpace(spaces, "project/work")
	if err != nil || selected.ID != "project" {
		t.Fatalf("selected=%#v error=%v", selected, err)
	}
	if _, err := resolveSpace(spaces, "missing"); err == nil {
		t.Fatal("missing Space accepted")
	}
	state := 1
	received := sharing.Share{
		ID: "share", State: &state, SpaceID: "project!item", Owner: "owner",
		OwnerName: "Owner", Path: "/report.pdf", Type: "user", Permissions: 1,
	}
	if !overviewReceivedStateMatches(received, shareStatePending) ||
		overviewReceivedStateMatches(received, shareStateDeclined) {
		t.Fatalf("received state mismatch: %#v", received)
	}
	item := overviewItem(received, shareDirectionReceived, spaces)
	if item.SpaceName != "Project" || item.PartyName != "Owner" ||
		item.Permission != "read" {
		t.Fatalf("overview=%#v", item)
	}
	if !shareSpaceMatches("project!item", "project") ||
		shareSpaceMatches("another!item", "project") {
		t.Fatal("Space matching failed")
	}
}

func TestPublicLinkTypeHelpers(t *testing.T) {
	for permissions, want := range map[int]string{
		1: "view", 5: "upload", 15: "edit",
	} {
		got, err := publicLinkType(permissions)
		if err != nil || got != want || publicLinkPermissions(got) == 0 {
			t.Fatalf("permissions %d = %q, %v", permissions, got, err)
		}
	}
	if _, err := publicLinkType(2); err == nil || publicLinkPermissions("future") != 0 {
		t.Fatal("unsupported link type accepted")
	}
}
