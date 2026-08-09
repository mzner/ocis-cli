package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mzner/ocis-cli/internal/apperror"
	"github.com/mzner/ocis-cli/internal/graph"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

func TestAdministrativeInventory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch request.URL.Path {
		case "/graph/v1.0/users":
			if request.URL.Query().Get("$top") == "1" {
				_, _ = io.WriteString(writer, `{"value":[]}`)
				return
			}
			if request.URL.Query().Get("$orderby") != "displayName asc" {
				t.Fatalf("user query: %s", request.URL.RawQuery)
			}
			_, _ = io.WriteString(writer, `{"value":[
				{"id":"user-z","displayName":"Zoe Example",
				 "onPremisesSamAccountName":"zoe","accountEnabled":false},
				{"id":"user-a","displayName":"Alice Example",
				 "onPremisesSamAccountName":"alice",
				 "mail":"alice@example.test","accountEnabled":true}
			]}`)
		case "/graph/v1.0/users/alice":
			_, _ = io.WriteString(writer, `{
				"id":"user-a","displayName":"Alice Example",
				"onPremisesSamAccountName":"alice",
				"mail":"alice@example.test","accountEnabled":true
			}`)
		case "/graph/v1.0/groups":
			if request.URL.Query().Get("$search") != `"engineers"` {
				t.Fatalf("group query: %s", request.URL.RawQuery)
			}
			_, _ = io.WriteString(writer, `{"value":[{
				"id":"group-1","displayName":"Engineers",
				"description":"Product engineering",
				"groupTypes":["ReadOnly"]
			}]}`)
		case "/graph/v1.0/groups/Engineers":
			_, _ = io.WriteString(writer, `{
				"id":"group-1","displayName":"Engineers",
				"description":"Product engineering",
				"groupTypes":["ReadOnly"]
			}`)
		case "/graph/v1.0/groups/group-1/members":
			_, _ = io.WriteString(writer, `[{
				"id":"user-a","displayName":"Alice Example",
				"onPremisesSamAccountName":"alice",
				"mail":"alice@example.test"
			}]`)
		case "/graph/v1.0/drives":
			_, _ = io.WriteString(writer, `{"value":[{
				"id":"space-1","name":"Engineering",
				"description":"Engineering documents",
				"driveType":"project","driveAlias":"engineering",
				"owner":{"user":{"id":"user-a","displayName":"Alice Example"}},
				"quota":{"used":25,"total":100,"remaining":75,"state":"normal"}
			}]}`)
		case "/graph/v1beta1/drives/space-1/root/permissions":
			writer.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(writer, "not a Space Manager")
		default:
			t.Fatalf(
				"unexpected request: %s %s",
				request.Method, request.URL.RequestURI(),
			)
		}
	}))
	defer server.Close()
	configureAdminTestProfile(t, server.URL)

	var output bytes.Buffer
	err := RunAdminWithOptions(
		context.Background(), AdminRequest{Operation: AdminUserList},
		"", RunOptions{Out: &output},
	)
	if err != nil {
		t.Fatal(err)
	}
	userList := output.String()
	if !strings.Contains(userList, "STATUS") ||
		!strings.Contains(userList, "USERNAME") ||
		!strings.Contains(userList, "DISPLAY NAME") ||
		strings.Index(userList, "Alice Example") >
			strings.Index(userList, "Zoe Example") {
		t.Fatalf("user list: %q", userList)
	}

	output.Reset()
	err = RunAdminWithOptions(
		context.Background(),
		AdminRequest{Operation: AdminUserInfo, Identifier: "alice"},
		"", RunOptions{Out: &output, OutputMode: appoutput.JSON},
	)
	if err != nil || !strings.Contains(
		output.String(), `"type": "admin-user"`,
	) || !strings.Contains(output.String(), `"id": "user-a"`) {
		t.Fatalf("user info: %q, %v", output.String(), err)
	}

	output.Reset()
	err = RunAdminWithOptions(
		context.Background(),
		AdminRequest{Operation: AdminUserInfo, Identifier: "alice"},
		"", RunOptions{Out: &output},
	)
	if err != nil || !strings.Contains(
		output.String(), "Username:      alice",
	) || !strings.Contains(output.String(), "Account:       enabled") {
		t.Fatalf("human user info: %q, %v", output.String(), err)
	}

	output.Reset()
	err = RunAdminWithOptions(
		context.Background(),
		AdminRequest{
			Operation: AdminGroupList,
			Search:    "engineers",
		},
		"", RunOptions{Out: &output},
	)
	if err != nil || !strings.Contains(
		output.String(), "read-only",
	) || !strings.Contains(output.String(), "Product engineering") {
		t.Fatalf("group list: %q, %v", output.String(), err)
	}

	output.Reset()
	err = RunAdminWithOptions(
		context.Background(),
		AdminRequest{
			Operation:  AdminGroupInfo,
			Identifier: "Engineers",
		},
		"", RunOptions{Out: &output},
	)
	if err != nil || !strings.Contains(
		output.String(), "Access:       read-only",
	) || !strings.Contains(output.String(), "Types:        ReadOnly") {
		t.Fatalf("group info: %q, %v", output.String(), err)
	}

	output.Reset()
	err = RunAdminWithOptions(
		context.Background(),
		AdminRequest{
			Operation:  AdminGroupMemberList,
			Identifier: "Engineers",
		},
		"", RunOptions{Out: &output},
	)
	if err != nil || !strings.Contains(
		output.String(), "Group: Engineers (group-1)",
	) || !strings.Contains(output.String(), "alice") {
		t.Fatalf("group members: %q, %v", output.String(), err)
	}

	output.Reset()
	err = RunAdminWithOptions(
		context.Background(),
		AdminRequest{Operation: AdminSpaceList},
		"", RunOptions{Out: &output},
	)
	if err != nil || !strings.Contains(
		output.String(), "TYPE",
	) || !strings.Contains(output.String(), "Engineering") ||
		!strings.Contains(output.String(), "Alice Example") {
		t.Fatalf("space list: %q, %v", output.String(), err)
	}

	output.Reset()
	err = RunAdminWithOptions(
		context.Background(),
		AdminRequest{
			Operation:  AdminSpaceInfo,
			Identifier: "engineering",
		},
		"", RunOptions{Out: &output},
	)
	if err != nil || !strings.Contains(
		output.String(), "Members: unavailable (not permitted)",
	) {
		t.Fatalf("space info: %q, %v", output.String(), err)
	}
}

func TestAdministrativeInventoryPermissionDenied(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		writer.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(writer, "search term too short")
	}))
	defer server.Close()
	configureAdminTestProfile(t, server.URL)

	err := RunAdminWithOptions(
		context.Background(), AdminRequest{Operation: AdminUserList},
		"", RunOptions{Out: io.Discard},
	)
	if !apperror.IsKind(err, apperror.KindAuthentication) ||
		!strings.Contains(err.Error(), "403 Forbidden") {
		t.Fatalf("error: %v", err)
	}
}

func TestAdministrativeInventoryUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		writer.WriteHeader(http.StatusNotImplemented)
	}))
	defer server.Close()
	configureAdminTestProfile(t, server.URL)

	err := RunAdminWithOptions(
		context.Background(), AdminRequest{Operation: AdminGroupList},
		"", RunOptions{Out: io.Discard},
	)
	if err == nil || !strings.Contains(
		err.Error(),
		"server does not expose administrative account management through LibreGraph",
	) {
		t.Fatalf("error: %v", err)
	}
}

func TestAdministrativeInventoryRejectsSpaceSelectionBeforeProfile(t *testing.T) {
	t.Setenv(
		"OCIS_CONFIG", filepath.Join(t.TempDir(), "missing", "config.json"),
	)
	err := RunAdminWithOptions(
		context.Background(), AdminRequest{Operation: AdminSpaceList},
		"", RunOptions{Out: io.Discard, Space: "project"},
	)
	if !apperror.IsKind(err, apperror.KindUsage) ||
		!strings.Contains(err.Error(), "--space cannot be used") {
		t.Fatalf("error: %v", err)
	}
}

func TestResolveAdminSpaceFailsClosedOnAmbiguousName(t *testing.T) {
	_, err := resolveAdminSpace([]space{
		{ID: "space-1", Name: "Shared"},
		{ID: "space-2", Name: "shared"},
	}, "shared")
	if !apperror.IsKind(err, apperror.KindUsage) ||
		!strings.Contains(err.Error(), "use its ID") {
		t.Fatalf("error: %v", err)
	}
}

func TestAdminValidationAndFormatting(t *testing.T) {
	t.Setenv(
		"OCIS_CONFIG", filepath.Join(t.TempDir(), "missing", "config.json"),
	)
	for _, request := range []AdminRequest{
		{Operation: AdminUserInfo},
		{
			Operation: AdminUserList,
			Search:    "alice",
			RawSearch: `"alice"`,
		},
		{
			Operation: AdminGroupList,
			Search:    `Engineering "Leads"`,
		},
		{Operation: AdminOperation("future-operation")},
	} {
		err := RunAdminWithOptions(
			context.Background(), request, "", RunOptions{Out: io.Discard},
		)
		if !apperror.IsKind(err, apperror.KindUsage) {
			t.Fatalf("request %#v: %v", request, err)
		}
	}

	disabled := false
	if adminUserStatus(nil) != "unknown" ||
		adminUserStatus(&disabled) != "disabled" {
		t.Fatal("unexpected account status formatting")
	}
	if adminGroupAccess(directoryGroupForTest()) != "writable" {
		t.Fatal("ordinary group was not writable")
	}
}

func directoryGroupForTest() graph.DirectoryGroup {
	return graph.DirectoryGroup{ID: "group-id", DisplayName: "Group"}
}

func configureAdminTestProfile(t *testing.T, server string) {
	t.Helper()
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := saveStore(
		defaultDependencies(),
		&store{
			Current: "work",
			Profiles: map[string]profile{"work": {
				Server: server, Insecure: true, Username: "alice",
				AuthType: "basic", Password: "secret",
			}},
		},
	); err != nil {
		t.Fatal(err)
	}
}
