package admin

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/mzner/ocis-cli/internal/graph"
	"github.com/mzner/ocis-cli/internal/httpapi"
	"github.com/mzner/ocis-cli/internal/sharing"
)

func TestRoleResolutionUsesAdvertisedIDsAndRejectsAmbiguity(t *testing.T) {
	roles := advertisedRoles([]graph.Application{{
		ID: "app-1", DisplayName: "oCIS",
		AppRoles: []graph.AppRole{
			{ID: "role-admin", DisplayName: "Admin"},
			{ID: "role-user", DisplayName: "User"},
		},
	}})
	resolved, err := ResolveAdvertisedRole(roles, "admin")
	if err != nil || resolved.role.ID != "role-admin" {
		t.Fatalf("resolved: %#v, %v", resolved, err)
	}
	roles = append(roles, advertisedRole{
		application: graph.Application{ID: "app-2"},
		role:        graph.AppRole{ID: "other-admin", DisplayName: "Admin"},
	})
	if _, err := ResolveAdvertisedRole(roles, "Admin"); err == nil ||
		!strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguity error: %v", err)
	}
}

func TestMutationGuardHelpers(t *testing.T) {
	capabilities := sharing.Capabilities{}
	capabilities.Graph.Users.ReadOnlyAttributes = []string{"user.mail"}
	if err := RequireWritableUserFields(
		capabilities, "user.displayName", "user.mail",
	); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("writable field error: %v", err)
	}
	if err := RejectReadOnlyGroup(graph.DirectoryGroup{
		DisplayName: "External", GroupTypes: []string{"ReadOnly"},
	}); err == nil {
		t.Fatal("read-only group was accepted")
	}
	displayName := "Updated"
	fields := SelectedUserUpdateFields(UserUpdateRequest{
		DisplayName: &displayName, SetPassword: true,
	})
	if len(fields) != 2 || titleWord("delete") != "Delete" || titleWord("") != "" {
		t.Fatalf("fields=%#v title=%q", fields, titleWord("delete"))
	}
	if err := AdminMutationError(
		"group", errors.New("backend is configured read-only"),
	); err == nil || !strings.Contains(err.Error(), "read-only identity") {
		t.Fatalf("read-only backend error: %v", err)
	}
	unsupported := &httpapi.HTTPError{
		StatusCode: http.StatusNotImplemented, Status: "501 Not Implemented",
	}
	if err := AdminMutationError("group", unsupported); err == nil ||
		!strings.Contains(err.Error(), "does not expose") {
		t.Fatalf("unsupported mutation error: %v", err)
	}
	if err := roleServiceError(&httpapi.HTTPError{
		StatusCode: http.StatusNotFound, Status: "404 Not Found",
	}); err == nil || !strings.Contains(err.Error(), "role service") {
		t.Fatalf("role service error: %v", err)
	}
}
