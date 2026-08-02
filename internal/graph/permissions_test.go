package graph

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mzner/ocis-cli/internal/httpapi"
)

const permissionsFixture = `{
	"@libre.graph.permissions.roles.allowedValues":[
		{"id":"viewer-id","displayName":"Viewer"},
		{"id":"manager-id","displayName":"Manager"}
	],
	"@libre.graph.permissions.actions.allowedValues":[
		"libre.graph/driveItem/permissions/create"
	],
	"value":[{
		"id":"u:alice","roles":["manager-id"],
		"grantedToV2":{"user":{"id":"alice-id","displayName":"Alice"}}
	}]
}`

func TestSpacePermissions(t *testing.T) {
	var removed bool
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch {
		case request.Method == http.MethodGet &&
			request.URL.Path == "/graph/v1.0/me":
			_, _ = io.WriteString(writer, `{
				"id":"alice-id","displayName":"Alice",
				"onPremisesSamAccountName":"alice"
			}`)
		case request.Method == http.MethodGet:
			_, _ = io.WriteString(writer, permissionsFixture)
		case request.Method == http.MethodPost:
			var body InviteRequest
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body.Recipients) != 1 ||
				body.Recipients[0].ObjectID != "bob-id" ||
				body.Recipients[0].Type != "user" ||
				len(body.Roles) != 1 || body.Roles[0] != "viewer-id" {
				t.Fatalf("invite: %#v", body)
			}
			_, _ = io.WriteString(writer, `{"value":[{
				"id":"u:bob","roles":["viewer-id"],
				"grantedToV2":{"user":{"id":"bob-id","displayName":"Bob"}}
			}]}`)
		case request.Method == http.MethodPatch:
			var body PermissionUpdateRequest
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body.Roles) != 1 || body.Roles[0] != "manager-id" {
				t.Fatalf("update: %#v", body)
			}
			_, _ = io.WriteString(writer, `{
				"id":"u:bob","roles":["manager-id"],
				"grantedToV2":{"user":{"id":"bob-id","displayName":"Bob"}}
			}`)
		case request.Method == http.MethodDelete:
			removed = true
			writer.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("request: %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(httpapi.Config{Server: server.URL}, server.Client())
	current, err := client.GetMe(context.Background())
	if err != nil || current.ID != "alice-id" || current.Username != "alice" {
		t.Fatalf("me: %#v, %v", current, err)
	}
	permissions, err := client.ListSpacePermissions(context.Background(), "space-id")
	if err != nil || len(permissions.Value) != 1 ||
		len(permissions.AllowedRoles) != 2 {
		t.Fatalf("permissions: %#v, %v", permissions, err)
	}
	added, err := client.AddSpaceMember(
		context.Background(), "space-id",
		InviteRequest{
			Recipients: []Recipient{{ObjectID: "bob-id", Type: "user"}},
			Roles:      []string{"viewer-id"},
		},
	)
	if err != nil || added.ID != "u:bob" {
		t.Fatalf("added: %#v, %v", added, err)
	}
	updated, err := client.UpdateSpaceMember(
		context.Background(), "space-id", "u:bob",
		PermissionUpdateRequest{Roles: []string{"manager-id"}},
	)
	if err != nil || updated.Roles[0] != "manager-id" {
		t.Fatalf("updated: %#v, %v", updated, err)
	}
	if err := client.RemoveSpaceMember(
		context.Background(), "space-id", "u:bob",
	); err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("permission was not removed")
	}
}

func TestAddSpaceMemberRejectsUnexpectedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		_, _ = io.WriteString(writer, `{"value":[]}`)
	}))
	defer server.Close()
	client := NewClient(httpapi.Config{Server: server.URL}, server.Client())
	_, err := client.AddSpaceMember(
		context.Background(), "space-id", InviteRequest{},
	)
	if err == nil {
		t.Fatal("empty permission response was accepted")
	}
}
