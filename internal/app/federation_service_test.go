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

func TestFederationInvitationAndConnectionUseCases(t *testing.T) {
	var accepted, deleted bool
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch request.Method + " " + request.URL.Path {
		case "GET /ocs/v2.php/cloud/capabilities":
			writeAppOCS(writer, `{"capabilities":{"files_sharing":{
				"federation":{"outgoing":true,"incoming":true}
			}}}`)
		case "POST /sciencemesh/generate-invite":
			_, _ = io.WriteString(writer, `{
				"token":"invite-token","description":"Work",
				"expiration":1786291200
			}`)
		case "GET /sciencemesh/list-invite":
			_, _ = io.WriteString(writer, `[{"token":"invite-token","expiration":1786291200}]`)
		case "POST /sciencemesh/accept-invite":
			var body map[string]string
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			accepted = body["token"] == "invite-token" &&
				body["providerDomain"] == "remote.example.test"
		case "GET /sciencemesh/find-accepted-users":
			_, _ = io.WriteString(writer, `[{
				"display_name":"Bob","idp":"https://remote.example.test",
				"user_id":"federated-id","mail":"bob@example.test"
			}]`)
		case "DELETE /sciencemesh/delete-accepted-user":
			deleted = true
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()
	configureSpaceTestProfile(t, server.URL, "")

	var invitation bytes.Buffer
	if err := RunFederationWithOptions(
		context.Background(), FederationRequest{
			Operation: FederationInviteCreate, Email: "bob@example.test",
			Description: "Work",
		}, "", RunOptions{Out: &invitation},
	); err != nil || !strings.Contains(invitation.String(), "invite-token") {
		t.Fatalf("invitation=%q error=%v", invitation.String(), err)
	}
	if err := RunFederationWithOptions(
		context.Background(), FederationRequest{
			Operation: FederationInviteAccept, Token: "invite-token",
			Provider: "https://remote.example.test/",
		}, "", RunOptions{Out: io.Discard},
	); err != nil || !accepted {
		t.Fatalf("accepted=%t error=%v", accepted, err)
	}
	var connections bytes.Buffer
	if err := RunFederationWithOptions(
		context.Background(), FederationRequest{
			Operation: FederationConnectionList, Identifier: "bob",
		}, "", RunOptions{Out: &connections, OutputMode: appoutput.JSON},
	); err != nil || !strings.Contains(connections.String(), `"userId": "federated-id"`) {
		t.Fatalf("connections=%q error=%v", connections.String(), err)
	}
	if err := RunFederationWithOptions(
		context.Background(), FederationRequest{
			Operation: FederationConnectionRemove, Identifier: "bob@example.test",
			Confirmed: true, DryRun: true,
		}, "", RunOptions{Out: io.Discard},
	); err != nil || deleted {
		t.Fatalf("dry-run deleted=%t error=%v", deleted, err)
	}
	if err := RunFederationWithOptions(
		context.Background(), FederationRequest{
			Operation: FederationConnectionRemove, Identifier: "federated-id",
			Provider: "remote.example.test", UserID: true, Confirmed: true,
		}, "", RunOptions{Out: io.Discard},
	); err != nil || !deleted {
		t.Fatalf("deleted=%t error=%v", deleted, err)
	}
}

func TestFederationConnectionRemoveFailsClosed(t *testing.T) {
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "missing", "config.json"))
	err := RunFederationWithOptions(
		context.Background(), FederationRequest{
			Operation: FederationConnectionRemove, Identifier: "federated-id",
		}, "", RunOptions{Out: io.Discard},
	)
	if !apperror.IsKind(err, apperror.KindUsage) ||
		!strings.Contains(err.Error(), "explicit confirmation") {
		t.Fatalf("error: %v", err)
	}
}

func TestNormalizeProviderDomain(t *testing.T) {
	for value, expected := range map[string]string{
		"cloud.example.test":          "cloud.example.test",
		"cloud.example.test:9200":     "cloud.example.test:9200",
		"https://cloud.example.test/": "cloud.example.test",
	} {
		actual, err := normalizeProviderDomain(value)
		if err != nil || actual != expected {
			t.Errorf("%q: got %q, %v", value, actual, err)
		}
	}
	for _, value := range []string{
		"file:///tmp/token", "https://user@example.test", "https://example.test/path",
	} {
		if _, err := normalizeProviderDomain(value); err == nil {
			t.Errorf("accepted invalid provider %q", value)
		}
	}
}

func TestFederatedShareUseCase(t *testing.T) {
	var invited bool
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		itemBase := "/graph/v1beta1/drives/storage$space/items/storage$space!file"
		switch {
		case request.URL.Path == "/ocs/v2.php/cloud/capabilities":
			writeAppOCS(writer, `{"capabilities":{"files_sharing":{
				"federation":{"outgoing":true,"incoming":true}
			}}}`)
		case request.Method == "PROPFIND":
			writer.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(writer, appVersionFile)
		case request.Method == http.MethodGet &&
			request.URL.Path == itemBase+"/permissions":
			if !strings.Contains(
				request.URL.Query().Get("$filter"), `@Subject.UserType=="Federated"`,
			) {
				t.Fatalf("missing federated role filter: %s", request.URL.RawQuery)
			}
			_, _ = io.WriteString(writer, `{
				"@libre.graph.permissions.roles.allowedValues":[{
					"id":"viewer-id","displayName":"Can view"
				}],"value":[]
			}`)
		case request.Method == http.MethodGet &&
			request.URL.Path == "/graph/v1.0/users":
			if request.URL.Query().Get("$filter") != "userType eq 'Federated'" {
				t.Fatalf("filter: %s", request.URL.RawQuery)
			}
			_, _ = io.WriteString(writer, `{"value":[{
				"id":"federated-id","displayName":"Bob","userType":"Federated",
				"mail":"bob@example.test"
			}]}`)
		case request.Method == http.MethodPost &&
			request.URL.Path == itemBase+"/invite":
			var body graphInviteBody
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			invited = len(body.Recipients) == 1 &&
				body.Recipients[0].ObjectID == "federated-id" &&
				len(body.Roles) == 1 && body.Roles[0] == "viewer-id"
			_, _ = io.WriteString(writer, `{"value":[{"id":"ocm-share-id"}]}`)
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()
	configureSpaceTestProfile(t, server.URL, "")
	var output bytes.Buffer
	err := RunShareWithOptions(
		context.Background(), ShareRequest{
			Operation: ShareFederatedAdd, Path: "/report.txt",
			Recipient: "bob@example.test", RecipientType: "federated",
			Role: "viewer", Federated: true,
		}, "", RunOptions{Out: &output},
	)
	if err != nil || !invited || !strings.Contains(output.String(), "ocm-share-id") {
		t.Fatalf("invited=%t output=%q error=%v", invited, output.String(), err)
	}
}
