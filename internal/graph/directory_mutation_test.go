package graph

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mzner/ocis-cli/internal/httpapi"
)

func TestAdministrativeDirectoryMutationsAndRoles(t *testing.T) {
	var requests []string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		requests = append(
			requests, request.Method+" "+request.URL.RequestURI(),
		)
		switch request.Method + " " + request.URL.Path {
		case "GET /graph/v1.0/users":
			if request.URL.Query().Get("$top") != "1" {
				t.Fatalf("admin preflight query: %s", request.URL.RawQuery)
			}
			_, _ = io.WriteString(writer, `{"value":[]}`)
		case "POST /graph/v1.0/users":
			var body CreateUserRequest
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Username != "alice" || body.Password == nil ||
				body.Password.Password != "secret" {
				t.Fatalf("create user body: %#v", body)
			}
			_, _ = io.WriteString(writer, `{
				"id":"user/1","onPremisesSamAccountName":"alice",
				"displayName":"Alice"
			}`)
		case "PATCH /graph/v1.0/users/user/1":
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["displayName"] != "Alice Updated" {
				t.Fatalf("update user body: %#v", body)
			}
			_, _ = io.WriteString(writer, `{
				"id":"user/1","onPremisesSamAccountName":"alice",
				"displayName":"Alice Updated"
			}`)
		case "DELETE /graph/v1.0/users/user/1":
			writer.WriteHeader(http.StatusNoContent)
		case "POST /graph/v1.0/groups":
			_, _ = io.WriteString(writer, `{
				"id":"group/1","displayName":"Engineering"
			}`)
		case "PATCH /graph/v1.0/groups/group/1",
			"DELETE /graph/v1.0/groups/group/1":
			writer.WriteHeader(http.StatusNoContent)
		case "POST /graph/v1.0/groups/group/1/members/$ref":
			var body map[string]string
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			want := server.URL + "/graph/v1.0/users/user%2F1"
			if body["@odata.id"] != want {
				t.Fatalf("member reference: %q, want %q", body["@odata.id"], want)
			}
			writer.WriteHeader(http.StatusNoContent)
		case "DELETE /graph/v1.0/groups/group/1/members/user/1/$ref":
			writer.WriteHeader(http.StatusNoContent)
		case "GET /graph/v1.0/applications":
			_, _ = io.WriteString(writer, `{"value":[{
				"id":"app-1","displayName":"oCIS",
				"appRoles":[{"id":"role-1","displayName":"Admin"}]
			}]}`)
		case "GET /graph/v1.0/users/user/1/appRoleAssignments":
			_, _ = io.WriteString(writer, `{"value":[{
				"id":"assignment-1","appRoleId":"role-1",
				"principalId":"user/1","resourceId":"app-1"
			}]}`)
		case "POST /graph/v1.0/users/user/1/appRoleAssignments":
			_, _ = io.WriteString(writer, `{
				"id":"assignment-1","appRoleId":"role-1",
				"principalId":"user/1","resourceId":"app-1"
			}`)
		case "DELETE /graph/v1.0/users/user/1/appRoleAssignments/assignment-1":
			writer.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.RequestURI())
		}
	}))
	defer server.Close()
	client := NewClient(httpapi.Config{Server: server.URL}, server.Client())
	ctx := context.Background()

	if err := client.CheckAdminMFA(ctx); err != nil {
		t.Fatal(err)
	}
	user, err := client.CreateUser(ctx, CreateUserRequest{
		Username: "alice", DisplayName: "Alice", Mail: "alice@example.test",
		Password: &PasswordProfile{Password: "secret"},
	})
	if err != nil || user.ID != "user/1" {
		t.Fatalf("create user: %#v, %v", user, err)
	}
	name := "Alice Updated"
	if _, err := client.UpdateUser(
		ctx, user.ID, UpdateUserRequest{DisplayName: &name},
	); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteUser(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	group, err := client.CreateGroup(
		ctx, CreateGroupRequest{DisplayName: "Engineering"},
	)
	if err != nil || group.ID != "group/1" {
		t.Fatalf("create group: %#v, %v", group, err)
	}
	groupName := "Platform"
	if err := client.UpdateGroup(
		ctx, group.ID, UpdateGroupRequest{DisplayName: &groupName},
	); err != nil {
		t.Fatal(err)
	}
	if err := client.AddGroupMember(ctx, group.ID, user.ID); err != nil {
		t.Fatal(err)
	}
	if err := client.RemoveGroupMember(ctx, group.ID, user.ID); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteGroup(ctx, group.ID); err != nil {
		t.Fatal(err)
	}
	applications, err := client.ListApplications(ctx)
	if err != nil || len(applications) != 1 {
		t.Fatalf("applications: %#v, %v", applications, err)
	}
	assignments, err := client.ListAppRoleAssignments(ctx, user.ID)
	if err != nil || len(assignments) != 1 {
		t.Fatalf("assignments: %#v, %v", assignments, err)
	}
	assignment, err := client.AssignAppRole(ctx, AppRoleAssignment{
		AppRoleID: "role-1", PrincipalID: user.ID, ResourceID: "app-1",
	})
	if err != nil || assignment.ID != "assignment-1" {
		t.Fatalf("assignment: %#v, %v", assignment, err)
	}
	if err := client.RemoveAppRoleAssignment(
		ctx, user.ID, assignment.ID,
	); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 13 {
		t.Fatalf("requests: %s", strings.Join(requests, "\n"))
	}
}
