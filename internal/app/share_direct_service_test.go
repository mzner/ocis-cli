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

type directShareServerState struct {
	invited bool
	updated bool
	removed bool
}

func TestDirectShareUseCases(t *testing.T) {
	state := &directShareServerState{}
	server := newDirectShareServer(t, state)
	defer server.Close()
	configureSpaceTestProfile(t, server.URL, "")

	var roles bytes.Buffer
	if err := RunShareWithOptions(
		context.Background(),
		ShareRequest{Operation: ShareRoles, Path: "/report.txt"},
		"", RunOptions{Out: &roles},
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(roles.String(), "Can view") ||
		!strings.Contains(roles.String(), "viewer-id") {
		t.Fatalf("roles: %s", roles.String())
	}

	var dryRun bytes.Buffer
	if err := RunShareWithOptions(
		context.Background(),
		ShareRequest{
			Operation: ShareDirectAdd, Path: "/report.txt",
			Recipient: "bob", RecipientType: "user",
			Role: "viewer", DryRun: true,
		},
		"", RunOptions{Out: &dryRun},
	); err != nil {
		t.Fatal(err)
	}
	if state.invited || !strings.Contains(dryRun.String(), "Would share") {
		t.Fatalf("invited=%t output=%s", state.invited, dryRun.String())
	}

	var added bytes.Buffer
	if err := RunShareWithOptions(
		context.Background(),
		ShareRequest{
			Operation: ShareDirectAdd, Path: "/report.txt",
			Recipient: "bob", RecipientType: "user", Role: "viewer",
		},
		"", RunOptions{Out: &added},
	); err != nil {
		t.Fatal(err)
	}
	if !state.invited || !strings.Contains(added.String(), "share-id") {
		t.Fatalf("invited=%t output=%s", state.invited, added.String())
	}

	var listed bytes.Buffer
	if err := RunShareWithOptions(
		context.Background(), ShareRequest{Operation: ShareList},
		"", RunOptions{Out: &listed, OutputMode: appoutput.JSON},
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listed.String(), `"recipientName": "Bob"`) ||
		!strings.Contains(listed.String(), `"type": "user"`) {
		t.Fatalf("list: %s", listed.String())
	}

	var received bytes.Buffer
	if err := RunShareWithOptions(
		context.Background(), ShareRequest{Operation: ShareReceived},
		"", RunOptions{Out: &received},
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(received.String(), "Alice") {
		t.Fatalf("received: %s", received.String())
	}

	var updatePlan bytes.Buffer
	if err := RunShareWithOptions(
		context.Background(),
		ShareRequest{
			Operation: ShareDirectUpdate, ID: "share-id",
			Role: "editor", DryRun: true,
		},
		"", RunOptions{Out: &updatePlan},
	); err != nil {
		t.Fatal(err)
	}
	if state.updated || !strings.Contains(updatePlan.String(), "Can edit") {
		t.Fatalf("updated=%t output=%s", state.updated, updatePlan.String())
	}
	if err := RunShareWithOptions(
		context.Background(),
		ShareRequest{
			Operation: ShareDirectUpdate, ID: "share-id", Role: "editor",
		},
		"", RunOptions{Out: io.Discard},
	); err != nil {
		t.Fatal(err)
	}
	if !state.updated {
		t.Fatal("share was not updated")
	}

	var removePlan bytes.Buffer
	if err := RunShareWithOptions(
		context.Background(),
		ShareRequest{
			Operation: ShareRemove, ID: "share-id",
			Confirmed: true, DryRun: true,
		},
		"", RunOptions{Out: &removePlan},
	); err != nil {
		t.Fatal(err)
	}
	if state.removed || !strings.Contains(removePlan.String(), "Would remove") {
		t.Fatalf("removed=%t output=%s", state.removed, removePlan.String())
	}
	if err := RunShareWithOptions(
		context.Background(),
		ShareRequest{
			Operation: ShareRemove, ID: "share-id", Confirmed: true,
		},
		"", RunOptions{Out: io.Discard},
	); err != nil {
		t.Fatal(err)
	}
	if !state.removed {
		t.Fatal("share was not removed")
	}
}

func TestDirectShareRemoveFailsClosedBeforeLoadingProfile(t *testing.T) {
	t.Setenv(
		"OCIS_CONFIG", filepath.Join(t.TempDir(), "missing", "config.json"),
	)
	err := RunShareWithOptions(
		context.Background(),
		ShareRequest{Operation: ShareRemove, ID: "share-id", DryRun: true},
		"", RunOptions{Out: io.Discard},
	)
	if !apperror.IsKind(err, apperror.KindUsage) ||
		!strings.Contains(err.Error(), "explicit confirmation") {
		t.Fatalf("error: %v", err)
	}
}

func TestDirectShareRejectsAmbiguousRoleAndPublicLinkUpdate(t *testing.T) {
	state := &directShareServerState{}
	server := newDirectShareServer(t, state)
	defer server.Close()
	configureSpaceTestProfile(t, server.URL, "")

	err := RunShareWithOptions(
		context.Background(),
		ShareRequest{
			Operation: ShareDirectUpdate, ID: "link-id", Role: "viewer",
		},
		"", RunOptions{Out: io.Discard},
	)
	if !apperror.IsKind(err, apperror.KindUsage) ||
		!strings.Contains(err.Error(), "public_link") {
		t.Fatalf("public link update: %v", err)
	}

	err = RunShareWithOptions(
		context.Background(),
		ShareRequest{
			Operation: ShareDirectUpdate, ID: "share-id", Role: "missing",
		},
		"", RunOptions{Out: io.Discard},
	)
	if !apperror.IsKind(err, apperror.KindUsage) ||
		!strings.Contains(err.Error(), "available roles") {
		t.Fatalf("missing role: %v", err)
	}
}

func newDirectShareServer(
	t *testing.T, state *directShareServerState,
) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		itemBase := "/graph/v1beta1/drives/storage$space/items/" +
			"storage$space!file"
		switch {
		case request.URL.Path == "/ocs/v2.php/cloud/capabilities":
			writeAppOCS(writer, `{"capabilities":{"files_sharing":{
				"api_enabled":true,"group_sharing":true,"sharing_roles":true,
				"public":{"enabled":true}
			}}}`)
		case request.Method == "PROPFIND" &&
			request.URL.Path ==
				"/remote.php/dav/files/alice/report.txt":
			writer.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(writer, appVersionFile)
		case request.Method == http.MethodGet &&
			request.URL.Path == "/graph/v1.0/users":
			_, _ = io.WriteString(writer, `{"value":[{
				"id":"bob-id","displayName":"Bob",
				"onPremisesSamAccountName":"bob","mail":"bob@example.test"
			}]}`)
		case request.Method == http.MethodGet &&
			request.URL.Path == itemBase+"/permissions":
			_, _ = io.WriteString(writer, `{
				"@libre.graph.permissions.roles.allowedValues":[
					{"id":"viewer-id","displayName":"Can view"},
					{"id":"editor-id","displayName":"Can edit"}
				],
				"value":[{"id":"share-id","roles":["viewer-id"]}]
			}`)
		case request.Method == http.MethodPost &&
			request.URL.Path == itemBase+"/invite":
			var body graphInviteBody
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body.Recipients) != 1 ||
				body.Recipients[0].ObjectID != "bob-id" ||
				len(body.Roles) != 1 || body.Roles[0] != "viewer-id" {
				t.Fatalf("invite: %#v", body)
			}
			state.invited = true
			_, _ = io.WriteString(
				writer,
				`{"value":[{"id":"share-id","roles":["viewer-id"]}]}`,
			)
		case request.Method == http.MethodPatch &&
			request.URL.Path == itemBase+"/permissions/share-id":
			state.updated = true
			_, _ = io.WriteString(
				writer, `{"id":"share-id","roles":["editor-id"]}`,
			)
		case request.Method == http.MethodDelete &&
			request.URL.Path == itemBase+"/permissions/share-id":
			state.removed = true
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodGet &&
			request.URL.Path ==
				"/ocs/v2.php/apps/files_sharing/api/v1/shares":
			if request.URL.Query().Get("shared_with_me") == "true" {
				writeAppOCS(writer, `[{
					"id":"received-id","share_type":0,
					"path":"/Shares/incoming.txt","uid_owner":"alice",
					"displayname_owner":"Alice","permissions":1,"state":0,
					"file_source":"storage$other!incoming"
				}]`)
				return
			}
			writeAppOCS(writer, `[
				{"id":"share-id","share_type":0,"path":"/report.txt",
				 "share_with":"bob","share_with_displayname":"Bob",
				 "permissions":1,"file_source":"storage$space!file"},
				{"id":"link-id","share_type":3,"path":"/report.txt",
				 "url":"https://cloud.test/s/token","permissions":1}
			]`)
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
	}))
}

type graphInviteBody struct {
	Recipients []struct {
		ObjectID string `json:"objectId"`
	} `json:"recipients"`
	Roles []string `json:"roles"`
}
