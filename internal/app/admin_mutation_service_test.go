package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

func TestAdminUserCreateRequiresAdminAndDoesNotExposePassword(t *testing.T) {
	var created atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch request.Method + " " + request.URL.Path {
		case "GET /graph/v1.0/users":
			if request.URL.Query().Get("$top") != "1" {
				t.Fatalf("preflight query: %s", request.URL.RawQuery)
			}
			_, _ = io.WriteString(writer, `{"value":[]}`)
		case "GET /ocs/v2.php/cloud/capabilities":
			writeAdminCapabilities(writer, `{
				"graph":{"users":{"create_disabled":false}}
			}`)
		case "POST /graph/v1.0/users":
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(body, []byte(`"password":"top-secret"`)) {
				t.Fatalf("password not sent in request body: %s", body)
			}
			created.Add(1)
			_, _ = io.WriteString(writer, `{
				"id":"user-1","onPremisesSamAccountName":"new-user",
				"displayName":"New User","mail":"new@example.test"
			}`)
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.RequestURI())
		}
	}))
	defer server.Close()
	configureAdminTestProfile(t, server.URL)

	var out bytes.Buffer
	err := RunAdminUserCreateWithOptions(
		context.Background(),
		AdminUserCreateRequest{
			Username: "new-user", DisplayName: "New User",
			Mail: "new@example.test", Password: "top-secret",
		},
		"", RunOptions{Out: &out, OutputMode: appoutput.JSON},
	)
	if err != nil {
		t.Fatal(err)
	}
	if created.Load() != 1 || !strings.Contains(out.String(), `"id": "user-1"`) {
		t.Fatalf("result: created=%d output=%q", created.Load(), out.String())
	}
	if strings.Contains(out.String(), "top-secret") {
		t.Fatalf("password leaked to output: %q", out.String())
	}
}

func TestAdminMutationFailsClosedForNonAdminAndMissingMFA(t *testing.T) {
	for _, test := range []struct {
		name     string
		mfa      bool
		wantText string
	}{
		{name: "non-admin", wantText: "403 Forbidden"},
		{name: "MFA required", mfa: true, wantText: "auth login work --mfa"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var mutations atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(
				writer http.ResponseWriter, request *http.Request,
			) {
				if request.Method != http.MethodGet ||
					request.URL.Path != "/graph/v1.0/users" ||
					request.URL.Query().Get("$top") != "1" {
					mutations.Add(1)
					t.Fatalf(
						"mutation attempted before authorization: %s %s",
						request.Method, request.URL.RequestURI(),
					)
				}
				if test.mfa {
					writer.Header().Set("X-Ocis-Mfa-Required", "true")
				}
				writer.WriteHeader(http.StatusForbidden)
				_, _ = io.WriteString(writer, "forbidden")
			}))
			defer server.Close()
			configureAdminTestProfile(t, server.URL)

			err := RunAdminGroupCreateWithOptions(
				context.Background(),
				AdminGroupCreateRequest{Name: "Engineering", DryRun: true},
				"", RunOptions{Out: io.Discard},
			)
			if !apperror.IsKind(err, apperror.KindAuthentication) ||
				!strings.Contains(err.Error(), test.wantText) {
				t.Fatalf("error: %v", err)
			}
			if mutations.Load() != 0 {
				t.Fatalf("mutations: %d", mutations.Load())
			}
		})
	}
}

func TestAdminUserCannotDisableCurrentAccount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch request.URL.RequestURI() {
		case "/graph/v1.0/users?$top=1":
			_, _ = io.WriteString(writer, `{"value":[]}`)
		case "/graph/v1.0/users/alice":
			_, _ = io.WriteString(writer, `{
				"id":"user-1","onPremisesSamAccountName":"alice",
				"displayName":"Alice"
			}`)
		case "/graph/v1.0/me":
			_, _ = io.WriteString(writer, `{
				"id":"user-1","onPremisesSamAccountName":"alice",
				"displayName":"Alice"
			}`)
		default:
			t.Fatalf("unexpected request: %s", request.URL.RequestURI())
		}
	}))
	defer server.Close()
	configureAdminTestProfile(t, server.URL)

	err := RunAdminUserStateWithOptions(
		context.Background(),
		AdminUserStateRequest{Identifier: "alice", Enabled: false},
		"", RunOptions{Out: io.Discard},
	)
	if !apperror.IsKind(err, apperror.KindConflict) ||
		!strings.Contains(err.Error(), "currently authenticated account") {
		t.Fatalf("error: %v", err)
	}
}

func TestAdminMutationServicesHappyPath(t *testing.T) {
	var mutations atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch request.Method + " " + request.URL.Path {
		case "GET /graph/v1.0/users":
			if request.URL.Query().Get("$top") != "1" {
				t.Fatalf("preflight query: %s", request.URL.RawQuery)
			}
			_, _ = io.WriteString(writer, `{"value":[]}`)
		case "GET /graph/v1.0/drives":
			if request.URL.Query().Get("$top") != "1" {
				t.Fatalf("MFA query: %s", request.URL.RawQuery)
			}
			_, _ = io.WriteString(writer, `{"value":[]}`)
		case "GET /ocs/v2.php/cloud/capabilities":
			writeAdminCapabilities(writer, `{
				"graph":{"users":{"read_only_attributes":[]}}
			}`)
		case "GET /graph/v1.0/users/target":
			_, _ = io.WriteString(writer, `{
				"id":"user-target",
				"onPremisesSamAccountName":"target",
				"displayName":"Target User","mail":"target@example.test"
			}`)
		case "GET /graph/v1.0/me":
			_, _ = io.WriteString(writer, `{
				"id":"user-admin",
				"onPremisesSamAccountName":"admin",
				"displayName":"Administrator"
			}`)
		case "PATCH /graph/v1.0/users/user-target":
			mutations.Add(1)
			_, _ = io.WriteString(writer, `{
				"id":"user-target",
				"onPremisesSamAccountName":"target",
				"displayName":"Updated User","accountEnabled":true
			}`)
		case "DELETE /graph/v1.0/users/user-target":
			mutations.Add(1)
			writer.WriteHeader(http.StatusNoContent)
		case "POST /graph/v1.0/groups":
			mutations.Add(1)
			_, _ = io.WriteString(writer, `{
				"id":"group-target","displayName":"Engineering"
			}`)
		case "GET /graph/v1.0/groups/team":
			_, _ = io.WriteString(writer, `{
				"id":"group-target","displayName":"Engineering"
			}`)
		case "PATCH /graph/v1.0/groups/group-target",
			"DELETE /graph/v1.0/groups/group-target",
			"POST /graph/v1.0/groups/group-target/members/$ref",
			"DELETE /graph/v1.0/groups/group-target/members/user-target/$ref":
			mutations.Add(1)
			writer.WriteHeader(http.StatusNoContent)
		case "GET /graph/v1.0/applications":
			_, _ = io.WriteString(writer, `{"value":[{
				"id":"application-1","displayName":"oCIS",
				"appRoles":[
					{"id":"role-user","displayName":"User"},
					{"id":"role-admin","displayName":"Admin"}
				]
			}]}`)
		case "GET /graph/v1.0/users/user-target/appRoleAssignments":
			_, _ = io.WriteString(writer, `{"value":[{
				"id":"assignment-1","appRoleId":"role-user",
				"principalId":"user-target","principalType":"User",
				"resourceId":"application-1",
				"resourceDisplayName":"oCIS"
			}]}`)
		case "POST /graph/v1.0/users/user-target/appRoleAssignments":
			mutations.Add(1)
			_, _ = io.WriteString(writer, `{
				"id":"assignment-2","appRoleId":"role-admin",
				"principalId":"user-target","resourceId":"application-1"
			}`)
		case "DELETE /graph/v1.0/users/user-target/appRoleAssignments/assignment-1":
			mutations.Add(1)
			writer.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf(
				"unexpected request: %s %s",
				request.Method, request.URL.RequestURI(),
			)
		}
	}))
	defer server.Close()
	configureAdminTestProfile(t, server.URL)
	options := RunOptions{Out: io.Discard}
	ctx := context.Background()

	displayName := "Updated User"
	if err := RunAdminUserUpdateWithOptions(
		ctx, AdminUserUpdateRequest{
			Identifier: "target", DisplayName: &displayName,
		}, "", options,
	); err != nil {
		t.Fatal(err)
	}
	if err := RunAdminUserStateWithOptions(
		ctx, AdminUserStateRequest{
			Identifier: "target", Enabled: true,
		}, "", options,
	); err != nil {
		t.Fatal(err)
	}
	if err := RunAdminGroupCreateWithOptions(
		ctx, AdminGroupCreateRequest{Name: "Engineering"}, "", options,
	); err != nil {
		t.Fatal(err)
	}
	if err := RunAdminGroupUpdateWithOptions(
		ctx, AdminGroupUpdateRequest{
			Identifier: "team", Name: "Platform",
		}, "", options,
	); err != nil {
		t.Fatal(err)
	}
	if err := RunAdminGroupMemberMutationWithOptions(
		ctx, AdminGroupMemberMutationRequest{
			Group: "team", User: "target",
		}, "", options,
	); err != nil {
		t.Fatal(err)
	}
	if err := RunAdminGroupMemberMutationWithOptions(
		ctx, AdminGroupMemberMutationRequest{
			Group: "team", User: "target", Remove: true,
		}, "", options,
	); err != nil {
		t.Fatal(err)
	}
	if err := RunAdminRoleWithOptions(
		ctx, AdminRoleRequest{Operation: AdminRoleAvailable}, "", options,
	); err != nil {
		t.Fatal(err)
	}
	if err := RunAdminRoleWithOptions(
		ctx, AdminRoleRequest{
			Operation: AdminRoleList, User: "target",
		}, "", options,
	); err != nil {
		t.Fatal(err)
	}
	if err := RunAdminRoleWithOptions(
		ctx, AdminRoleRequest{
			Operation: AdminRoleGrant, User: "target", Role: "Admin",
		}, "", options,
	); err != nil {
		t.Fatal(err)
	}
	if err := RunAdminRoleWithOptions(
		ctx, AdminRoleRequest{
			Operation: AdminRoleRevoke, User: "target",
			Role: "assignment-1",
		}, "", options,
	); err != nil {
		t.Fatal(err)
	}
	if err := RunAdminSpaceMFACheckWithOptions(ctx, "", options); err != nil {
		t.Fatal(err)
	}
	if err := RunAdminGroupDeleteWithOptions(
		ctx, AdminGroupDeleteRequest{Identifier: "team"}, "", options,
	); err != nil {
		t.Fatal(err)
	}
	if err := RunAdminUserDeleteWithOptions(
		ctx, AdminUserDeleteRequest{Identifier: "target"}, "", options,
	); err != nil {
		t.Fatal(err)
	}
	if mutations.Load() != 10 {
		t.Fatalf("mutations = %d, want 10", mutations.Load())
	}
}

func TestResolveMFAACRUsesServerCapability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.URL.Path != "/ocs/v2.php/cloud/capabilities" {
			t.Fatalf("path: %s", request.URL.Path)
		}
		writeAdminCapabilities(writer, `{
			"auth":{"mfa":{"enabled":true,
				"levelnames":["urn:ocis:mfa","urn:fallback"]}}
		}`)
	}))
	defer server.Close()
	acr, err := resolveMFAACR(
		context.Background(),
		profile{Server: server.URL, Insecure: true, AuthType: "basic"},
		"", RunOptions{Timeout: time.Second}.normalized(),
	)
	if err != nil || acr != "urn:ocis:mfa" {
		t.Fatalf("ACR: %q, %v", acr, err)
	}
}

func writeAdminCapabilities(writer http.ResponseWriter, capabilities string) {
	writer.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(writer, `{"ocs":{"meta":{
		"status":"ok","statuscode":200,"message":"OK"
	},"data":{"capabilities":`+capabilities+`}}}`)
}
