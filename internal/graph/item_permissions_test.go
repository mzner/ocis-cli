package graph

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mzner/ocis-cli/internal/httpapi"
)

func TestItemPermissionLifecycle(t *testing.T) {
	var invited, updated, removed bool
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		base := "/graph/v1beta1/drives/storage$space/items/" +
			"storage$space!file"
		switch {
		case request.Method == http.MethodGet &&
			request.URL.Path == base+"/permissions":
			_, _ = io.WriteString(writer, `{
				"@libre.graph.permissions.roles.allowedValues":[
					{"id":"editor-id","displayName":"Can edit"},
					{"id":"viewer-id","displayName":"Can view"}
				],
				"value":[{"id":"permission-id","roles":["viewer-id"]}]
			}`)
		case request.Method == http.MethodPost &&
			request.URL.Path == base+"/invite":
			var requestBody InviteRequest
			if err := json.NewDecoder(request.Body).Decode(&requestBody); err != nil {
				t.Fatal(err)
			}
			if len(requestBody.Recipients) != 1 ||
				requestBody.Recipients[0].ObjectID != "user-id" ||
				requestBody.Recipients[0].Type != "user" ||
				len(requestBody.Roles) != 1 ||
				requestBody.Roles[0] != "viewer-id" {
				t.Fatalf("invite: %#v", requestBody)
			}
			invited = true
			_, _ = io.WriteString(
				writer,
				`{"value":[{"id":"new-permission","roles":["viewer-id"]}]}`,
			)
		case request.Method == http.MethodPatch &&
			request.URL.Path == base+"/permissions/permission-id":
			updated = true
			_, _ = io.WriteString(
				writer,
				`{"id":"permission-id","roles":["editor-id"]}`,
			)
		case request.Method == http.MethodDelete &&
			request.URL.Path == base+"/permissions/permission-id":
			removed = true
			writer.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(httpapi.Config{Server: server.URL}, server.Client())
	resourceID := "storage$space!file"
	permissions, err := client.ListItemPermissions(
		context.Background(), resourceID,
	)
	if err != nil || len(permissions.AllowedRoles) != 2 ||
		permissions.AllowedRoles[0].ID != "editor-id" {
		t.Fatalf("permissions: %#v, %v", permissions, err)
	}
	permission, err := client.InviteItem(
		context.Background(), resourceID,
		InviteRequest{
			Recipients: []Recipient{{ObjectID: "user-id", Type: "user"}},
			Roles:      []string{"viewer-id"},
		},
	)
	if err != nil || permission.ID != "new-permission" || !invited {
		t.Fatalf("invite: %#v, %v", permission, err)
	}
	permission, err = client.UpdateItemPermission(
		context.Background(), resourceID, "permission-id",
		PermissionUpdateRequest{Roles: []string{"editor-id"}},
	)
	if err != nil || permission.Roles[0] != "editor-id" || !updated {
		t.Fatalf("update: %#v, %v", permission, err)
	}
	if err := client.RemoveItemPermission(
		context.Background(), resourceID, "permission-id",
	); err != nil || !removed {
		t.Fatalf("remove: %v", err)
	}
}

func TestListFederatedItemPermissionsUsesRoleFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		want := `@libre.graph.permissions.roles.allowedValues/rolePermissions/any(p:contains(p/condition, '@Subject.UserType=="Federated"'))`
		if request.URL.Query().Get("$filter") != want {
			t.Fatalf("filter: %q", request.URL.Query().Get("$filter"))
		}
		_, _ = io.WriteString(writer, `{
			"@libre.graph.permissions.roles.allowedValues":[{
				"id":"viewer-id","displayName":"Can view"
			}],"value":[]
		}`)
	}))
	defer server.Close()
	client := NewClient(httpapi.Config{Server: server.URL}, server.Client())
	permissions, err := client.ListFederatedItemPermissions(
		context.Background(), "storage$space!file",
	)
	if err != nil || len(permissions.AllowedRoles) != 1 ||
		permissions.AllowedRoles[0].ID != "viewer-id" {
		t.Fatalf("permissions: %#v, %v", permissions, err)
	}
}

func TestPublicLinkPermissionLifecycle(t *testing.T) {
	var patched, passwordSet bool
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		const resource = "/graph/v1beta1/drives/storage$space/items/" +
			"storage$space!file/permissions/link-id"
		switch {
		case request.Method == http.MethodGet &&
			request.URL.Path == resource:
			_, _ = io.WriteString(writer, `{
				"id":"link-id","hasPassword":false,
				"expirationDateTime":"2026-08-31T00:00:00Z",
				"link":{"type":"view","webUrl":"https://cloud.test/s/token",
				"@libre.graph.displayName":"Report"}
			}`)
		case request.Method == http.MethodPatch &&
			request.URL.Path == resource:
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(body), `"expirationDateTime":null`) ||
				!strings.Contains(string(body), `"type":"edit"`) ||
				!strings.Contains(
					string(body), `"@libre.graph.displayName":"Quarterly"`,
				) {
				t.Fatalf("patch body: %s", body)
			}
			patched = true
			_, _ = io.WriteString(writer, `{
				"id":"link-id","link":{"type":"edit",
				"@libre.graph.displayName":"Quarterly"}
			}`)
		case request.Method == http.MethodPost &&
			request.URL.Path == resource+"/setPassword":
			var body map[string]string
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["password"] != "secret" {
				t.Fatalf("password body: %#v", body)
			}
			passwordSet = true
			_, _ = io.WriteString(
				writer, `{"id":"link-id","hasPassword":true}`,
			)
		default:
			t.Fatalf(
				"unexpected request: %s %s",
				request.Method, request.URL.Path,
			)
		}
	}))
	defer server.Close()
	client := NewClient(httpapi.Config{Server: server.URL}, server.Client())
	const resourceID = "storage$space!file"

	permission, err := client.GetItemPermission(
		context.Background(), resourceID, "link-id",
	)
	if err != nil || permission.Link == nil ||
		permission.Link.Type != "view" ||
		permission.ExpirationDateTime == nil {
		t.Fatalf("get permission: %#v, %v", permission, err)
	}
	name, linkType := "Quarterly", "edit"
	update := LinkPermissionUpdateRequest{
		Link: &SharingLinkUpdate{
			DisplayName: &name, Type: &linkType,
		},
	}
	update.ClearExpiration()
	permission, err = client.UpdateLinkPermission(
		context.Background(), resourceID, "link-id", update,
	)
	if err != nil || permission.Link == nil ||
		permission.Link.Type != "edit" || !patched {
		t.Fatalf("update permission: %#v, %v", permission, err)
	}
	permission, err = client.SetItemPermissionPassword(
		context.Background(), resourceID, "link-id", "secret",
	)
	if err != nil || !permission.HasPassword || !passwordSet {
		t.Fatalf("set password: %#v, %v", permission, err)
	}

	expires := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	update = LinkPermissionUpdateRequest{}
	update.SetExpiration(expires)
	data, err := json.Marshal(update)
	if err != nil || !strings.Contains(
		string(data), `"expirationDateTime":"2026-09-01T00:00:00Z"`,
	) {
		t.Fatalf("expiration JSON: %s, %v", data, err)
	}
}

func TestItemPermissionsValidateResourceAndInviteShape(t *testing.T) {
	client := NewClient(httpapi.Config{Server: "https://cloud.test"}, nil)
	if _, err := client.ListItemPermissions(
		context.Background(), "invalid",
	); err == nil {
		t.Fatal("invalid resource ID was accepted")
	}
	if _, err := client.InviteItem(
		context.Background(), "storage$space!file", InviteRequest{},
	); err == nil {
		t.Fatal("empty invitation was accepted")
	}
	if err := client.RemoveItemPermission(
		context.Background(), "storage$space!file", "",
	); err == nil {
		t.Fatal("empty permission ID was accepted")
	}
	if _, err := client.UpdateLinkPermission(
		context.Background(), "storage$space!file", "link-id",
		LinkPermissionUpdateRequest{},
	); err == nil {
		t.Fatal("empty public-link update was accepted")
	}
}
