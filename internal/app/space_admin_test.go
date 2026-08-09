package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

const appSpacePermissions = `{
	"@libre.graph.permissions.roles.allowedValues":[
		{"id":"viewer-id","displayName":"Can view"},
		{"id":"editor-id","displayName":"Can edit with versions and trashbin"},
		{"id":"manager-id","displayName":"Can manage"}
	],
	"@libre.graph.permissions.actions.allowedValues":[
		"libre.graph/driveItem/permissions/read",
		"libre.graph/driveItem/permissions/create",
		"libre.graph/driveItem/permissions/update",
		"libre.graph/driveItem/permissions/delete"
	],
	"value":[{
		"id":"u:alice","roles":["manager-id"],
		"grantedToV2":{"user":{"id":"alice-id","displayName":"Alice"}}
	}]
}`

type spaceAdminServerState struct {
	invite       map[string]any
	memberUpdate map[string]any
	spaceUpdate  map[string]any
	removed      bool
	disabled     bool
	restored     bool
	purged       bool
}

func TestSpaceMemberUseCases(t *testing.T) {
	state := &spaceAdminServerState{}
	server := newSpaceAdminServer(t, state)
	defer server.Close()
	configureSpaceTestProfile(t, server.URL, "")

	var listed bytes.Buffer
	if err := RunSpaceMemberWithOptions(
		context.Background(),
		SpaceMemberRequest{Operation: SpaceMemberList, Space: "Engineering"},
		"", RunOptions{Out: &listed, OutputMode: appoutput.JSON},
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listed.String(), `"displayName": "Alice"`) ||
		!strings.Contains(listed.String(), `"role": "can manage"`) {
		t.Fatalf("members: %s", listed.String())
	}

	var added bytes.Buffer
	if err := RunSpaceMemberWithOptions(
		context.Background(),
		SpaceMemberRequest{
			Operation: SpaceMemberAdd, Space: "Engineering",
			RecipientID: "bob-id", RecipientType: "user", Role: "viewer",
		},
		"", RunOptions{Out: &added},
	); err != nil {
		t.Fatal(err)
	}
	if state.invite["recipients"] == nil ||
		!strings.Contains(added.String(), "Added user Bob") {
		t.Fatalf("invite: %#v, output: %s", state.invite, added.String())
	}

	var updated bytes.Buffer
	if err := RunSpaceMemberWithOptions(
		context.Background(),
		SpaceMemberRequest{
			Operation: SpaceMemberUpdate, Space: "Engineering",
			PermissionID: "u:bob", Role: "manager",
		},
		"", RunOptions{Out: &updated},
	); err != nil {
		t.Fatal(err)
	}
	if state.memberUpdate["roles"] == nil ||
		!strings.Contains(updated.String(), "Updated Bob to can manage") {
		t.Fatalf(
			"update: %#v, output: %s", state.memberUpdate, updated.String(),
		)
	}

	if err := RunSpaceMemberWithOptions(
		context.Background(),
		SpaceMemberRequest{
			Operation: SpaceMemberRemove, Space: "Engineering",
			PermissionID: "u:bob",
		},
		"", RunOptions{Out: io.Discard},
	); err != nil {
		t.Fatal(err)
	}
	if !state.removed {
		t.Fatal("member was not removed")
	}
}

func TestSpaceMemberDryRunAndValidation(t *testing.T) {
	state := &spaceAdminServerState{}
	server := newSpaceAdminServer(t, state)
	defer server.Close()
	configureSpaceTestProfile(t, server.URL, "")

	var rendered bytes.Buffer
	err := RunSpaceMemberWithOptions(
		context.Background(),
		SpaceMemberRequest{
			Operation: SpaceMemberAdd, Space: "Engineering",
			RecipientID: "group-id", RecipientType: "group",
			Role: "editor", DryRun: true,
		},
		"", RunOptions{Out: &rendered, OutputMode: appoutput.JSON},
	)
	if err != nil {
		t.Fatal(err)
	}
	if state.invite != nil || !strings.Contains(rendered.String(), `"dryRun": true`) {
		t.Fatalf("invite: %#v, output: %s", state.invite, rendered.String())
	}

	err = RunSpaceMemberWithOptions(
		context.Background(),
		SpaceMemberRequest{
			Operation: SpaceMemberAdd, Space: "Engineering",
			RecipientID: "bob-id", RecipientType: "device", Role: "viewer",
		},
		"", RunOptions{Out: io.Discard},
	)
	if !apperror.IsKind(err, apperror.KindUsage) {
		t.Fatalf("invalid recipient type: %v", err)
	}
	err = RunSpaceMemberWithOptions(
		context.Background(),
		SpaceMemberRequest{
			Operation: SpaceMemberUpdate, Space: "Engineering",
			PermissionID: "u:bob", Role: "owner",
		},
		"", RunOptions{Out: io.Discard},
	)
	if !apperror.IsKind(err, apperror.KindUsage) ||
		!strings.Contains(err.Error(), "available roles") {
		t.Fatalf("invalid role: %v", err)
	}
}

func TestRoleSemanticAliases(t *testing.T) {
	for value, expected := range map[string]string{
		"Can view": "viewer", "viewer": "viewer", "read": "viewer",
		"Can edit with versions and trashbin": "editor", "write": "editor",
		"Can manage": "manager", "manager": "manager",
		"custom role": "",
	} {
		if actual := roleSemantic(value); actual != expected {
			t.Errorf("%q: got %q, want %q", value, actual, expected)
		}
	}
}

func TestSpaceUpdateUseCase(t *testing.T) {
	state := &spaceAdminServerState{}
	server := newSpaceAdminServer(t, state)
	defer server.Close()
	configureSpaceTestProfile(t, server.URL, "")

	name, description, alias := " Platform ", "", "project/platform"
	quota := int64(10_000)
	var rendered bytes.Buffer
	err := RunSpaceUpdateWithOptions(
		context.Background(),
		SpaceUpdateRequest{
			Identifier: "Engineering", Name: &name,
			Description: &description, Alias: &alias, Quota: &quota,
		},
		"", RunOptions{Out: &rendered},
	)
	if err != nil {
		t.Fatal(err)
	}
	if state.spaceUpdate["name"] != "Platform" ||
		state.spaceUpdate["description"] != "" ||
		state.spaceUpdate["driveAlias"] != "project/platform" ||
		!strings.Contains(rendered.String(), "Updated project space Platform") {
		t.Fatalf(
			"update: %#v, output: %s", state.spaceUpdate, rendered.String(),
		)
	}

	state.spaceUpdate = nil
	err = RunSpaceUpdateWithOptions(
		context.Background(),
		SpaceUpdateRequest{
			Identifier: "Engineering", Description: &description, DryRun: true,
		},
		"", RunOptions{Out: io.Discard},
	)
	if err != nil || state.spaceUpdate != nil {
		t.Fatalf("dry run: %#v, %v", state.spaceUpdate, err)
	}
	err = RunSpaceUpdateWithOptions(
		context.Background(),
		SpaceUpdateRequest{Identifier: "Engineering"},
		"", RunOptions{Out: io.Discard},
	)
	if !apperror.IsKind(err, apperror.KindUsage) {
		t.Fatalf("empty update: %v", err)
	}
}

func TestSpaceLifecycleUseCases(t *testing.T) {
	state := &spaceAdminServerState{}
	server := newSpaceAdminServer(t, state)
	defer server.Close()
	configureSpaceTestProfile(t, server.URL, "space-id")

	var disabled bytes.Buffer
	if err := RunSpaceLifecycleWithOptions(
		context.Background(),
		SpaceLifecycleRequest{
			Operation: SpaceDisable, Identifier: "Engineering",
		},
		"", RunOptions{Out: &disabled},
	); err != nil {
		t.Fatal(err)
	}
	if !state.disabled || !strings.Contains(disabled.String(), "Use this ID") {
		t.Fatalf("disabled: %t, output: %s", state.disabled, disabled.String())
	}
	persisted, err := loadStore(defaultDependencies())
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Profiles["work"].DefaultSpace != "" {
		t.Fatalf("default space was not cleared: %#v", persisted.Profiles["work"])
	}

	if err := RunSpaceLifecycleWithOptions(
		context.Background(),
		SpaceLifecycleRequest{
			Operation: SpaceRestore, Identifier: "space-id",
		},
		"", RunOptions{Out: io.Discard},
	); err != nil {
		t.Fatal(err)
	}
	if !state.restored {
		t.Fatal("space was not restored")
	}
	if err := RunSpaceLifecycleWithOptions(
		context.Background(),
		SpaceLifecycleRequest{
			Operation: SpaceDelete, Identifier: "space-id", Permanent: true,
		},
		"", RunOptions{Out: io.Discard},
	); err != nil {
		t.Fatal(err)
	}
	if !state.purged {
		t.Fatal("space was not permanently deleted")
	}
}

func TestSpaceLifecycleDryRunNeedsNoProfileForStableID(t *testing.T) {
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "missing", "config.json"))
	for _, operation := range []SpaceLifecycleOperation{
		SpaceRestore, SpaceDelete,
	} {
		var rendered bytes.Buffer
		err := RunSpaceLifecycleWithOptions(
			context.Background(),
			SpaceLifecycleRequest{
				Operation: operation, Identifier: "space-id", DryRun: true,
				Permanent: operation == SpaceDelete,
			},
			"", RunOptions{Out: &rendered},
		)
		if err != nil || !strings.Contains(rendered.String(), "Would") {
			t.Fatalf("%s: output=%q err=%v", operation, rendered.String(), err)
		}
	}
}

func TestSpaceDeleteRequiresPermanentIntentAtApplicationBoundary(t *testing.T) {
	err := RunSpaceLifecycleWithOptions(
		context.Background(),
		SpaceLifecycleRequest{
			Operation: SpaceDelete, Identifier: "space-id", DryRun: true,
		},
		"", RunOptions{Out: io.Discard},
	)
	if !apperror.IsKind(err, apperror.KindUsage) ||
		!strings.Contains(err.Error(), "permanent deletion requires") {
		t.Fatalf("error: %v", err)
	}
}

func TestSpaceUnsetAndCurrent(t *testing.T) {
	state := &spaceAdminServerState{}
	server := newSpaceAdminServer(t, state)
	defer server.Close()
	configureSpaceTestProfile(t, server.URL, "space-id")

	var current bytes.Buffer
	if err := RunSpaceWithOptions(
		context.Background(),
		SpaceRequest{Operation: SpaceCurrent},
		"", RunOptions{Out: &current},
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(current.String(), "Default: Engineering (space-id)") {
		t.Fatalf("current: %s", current.String())
	}

	var unset bytes.Buffer
	if err := RunSpaceWithOptions(
		context.Background(),
		SpaceRequest{Operation: SpaceUnset},
		"", RunOptions{Out: &unset},
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unset.String(), "Using personal files") {
		t.Fatalf("unset: %s", unset.String())
	}
	persisted, err := loadStore(defaultDependencies())
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Profiles["work"].DefaultSpace != "" {
		t.Fatalf("default Space: %q", persisted.Profiles["work"].DefaultSpace)
	}

	current.Reset()
	if err := RunSpaceWithOptions(
		context.Background(),
		SpaceRequest{Operation: SpaceCurrent},
		"", RunOptions{Out: &current},
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(current.String(), "personal files (implicit)") {
		t.Fatalf("current after unset: %s", current.String())
	}
}

func TestSpaceStatDegradesWhenMembersAreForbidden(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch request.URL.Path {
		case "/graph/v1.0/drives":
			_, _ = io.WriteString(writer, `{"value":[{
				"id":"space-id","name":"Engineering","driveType":"project",
				"quota":{"used":5,"total":10}
			}]}`)
		default:
			writer.WriteHeader(http.StatusForbidden)
		}
	}))
	defer server.Close()
	configureSpaceTestProfile(t, server.URL, "")
	var rendered bytes.Buffer
	err := RunSpaceWithOptions(
		context.Background(),
		SpaceRequest{Operation: SpaceStat, Identifier: "Engineering"},
		"", RunOptions{Out: &rendered},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered.String(), "Members: unavailable") {
		t.Fatalf("output: %s", rendered.String())
	}
}

func newSpaceAdminServer(
	t *testing.T, state *spaceAdminServerState,
) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch {
		case request.Method == http.MethodGet &&
			(request.URL.Path == "/graph/v1.0/me/drives" ||
				request.URL.Path == "/graph/v1.0/drives"):
			_, _ = io.WriteString(writer, `{"value":[{
				"id":"space-id","name":"Engineering","description":"Shared work",
				"driveType":"project","driveAlias":"project/engineering",
				"quota":{"used":5,"total":10,"state":"normal"}
			}]}`)
		case request.Method == http.MethodGet &&
			request.URL.Path ==
				"/graph/v1beta1/drives/space-id/root/permissions":
			_, _ = io.WriteString(writer, appSpacePermissions)
		case request.Method == http.MethodGet &&
			request.URL.Path == "/graph/v1.0/me":
			_, _ = io.WriteString(writer, `{
				"id":"alice-id","displayName":"Alice",
				"onPremisesSamAccountName":"alice"
			}`)
		case request.Method == http.MethodGet &&
			request.URL.Path == "/graph/v1.0/users":
			_, _ = io.WriteString(writer, `{"value":[{
				"id":"bob-id","displayName":"Bob",
				"onPremisesSamAccountName":"bob","mail":"bob@example.test"
			}]}`)
		case request.Method == http.MethodGet &&
			request.URL.Path == "/graph/v1.0/groups":
			_, _ = io.WriteString(writer, `{"value":[{
				"id":"group-id","displayName":"Developers"
			}]}`)
		case request.Method == http.MethodPost &&
			request.URL.Path ==
				"/graph/v1beta1/drives/space-id/root/invite":
			decodeJSONBody(t, request, &state.invite)
			_, _ = io.WriteString(writer, `{"value":[{
				"id":"u:bob","roles":["viewer-id"],
				"grantedToV2":{"user":{"id":"bob-id","displayName":"Bob"}}
			}]}`)
		case request.Method == http.MethodPatch &&
			request.URL.Path ==
				"/graph/v1beta1/drives/space-id/root/permissions/u:bob":
			decodeJSONBody(t, request, &state.memberUpdate)
			_, _ = io.WriteString(writer, `{
				"id":"u:bob","roles":["manager-id"],
				"grantedToV2":{"user":{"id":"bob-id","displayName":"Bob"}}
			}`)
		case request.Method == http.MethodDelete &&
			request.URL.Path ==
				"/graph/v1beta1/drives/space-id/root/permissions/u:bob":
			state.removed = true
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodPatch &&
			request.URL.Path == "/graph/v1.0/drives/space-id" &&
			request.Header.Get("Restore") == "true":
			state.restored = true
			_, _ = io.WriteString(writer, `{
				"id":"space-id","name":"Engineering","driveType":"project"
			}`)
		case request.Method == http.MethodPatch &&
			request.URL.Path == "/graph/v1.0/drives/space-id":
			decodeJSONBody(t, request, &state.spaceUpdate)
			_, _ = io.WriteString(writer, `{
				"id":"space-id","name":"Platform","driveType":"project",
				"driveAlias":"project/platform","quota":{"total":10000}
			}`)
		case request.Method == http.MethodDelete &&
			request.URL.Path == "/graph/v1.0/drives/space-id":
			if request.Header.Get("Purge") == "true" {
				state.purged = true
			} else {
				state.disabled = true
			}
			writer.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
	}))
}

func configureSpaceTestProfile(
	t *testing.T, serverURL string, defaultSpace string,
) {
	t.Helper()
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := saveStore(defaultDependencies(), &store{
		Current: "work",
		Profiles: map[string]profile{"work": {
			Server: serverURL, Insecure: true, Username: "alice",
			AuthType: "basic", Password: "secret",
			DefaultSpace: defaultSpace,
		}},
	}); err != nil {
		t.Fatal(err)
	}
}

func decodeJSONBody(
	t *testing.T, request *http.Request, destination *map[string]any,
) {
	t.Helper()
	*destination = map[string]any{}
	if err := json.NewDecoder(request.Body).Decode(destination); err != nil {
		t.Fatal(err)
	}
}
