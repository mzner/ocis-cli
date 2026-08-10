package federation

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mzner/ocis-cli/internal/httpapi"
)

func TestInvitationAndConnectionLifecycle(t *testing.T) {
	var accepted, deleted bool
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("authorization: %q", request.Header.Get("Authorization"))
		}
		switch request.Method + " " + request.URL.Path {
		case "POST /sciencemesh/generate-invite":
			var body CreateInvitationRequest
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Recipient != "bob@example.test" || body.Description != "Work" {
				t.Fatalf("body: %#v", body)
			}
			_, _ = io.WriteString(writer, `{
				"token":"invite-token","description":"Work",
				"expiration":1786291200,"invite_link":"https://mesh.test/invite"
			}`)
		case "GET /sciencemesh/list-invite":
			_, _ = io.WriteString(writer, `[{"token":"invite-token","expiration":1786291200}]`)
		case "POST /sciencemesh/accept-invite":
			var body AcceptInvitationRequest
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			accepted = body.Token == "invite-token" &&
				body.ProviderDomain == "cloud.example.test"
		case "GET /sciencemesh/find-accepted-users":
			_, _ = io.WriteString(writer, `[{
				"display_name":"Bob","idp":"https://cloud.example.test",
				"user_id":"federated-id","mail":"bob@example.test"
			}]`)
		case "DELETE /sciencemesh/delete-accepted-user":
			var body DeleteConnectionRequest
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			deleted = body.Provider == "https://cloud.example.test" &&
				body.UserID == "federated-id"
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()
	client := NewClient(httpapi.Config{
		Server: server.URL, AuthType: "oidc", AccessToken: "token",
	}, server.Client())
	ctx := context.Background()
	created, err := client.CreateInvitation(ctx, CreateInvitationRequest{
		Recipient: "bob@example.test", Description: "Work",
	})
	if err != nil || created.Token != "invite-token" ||
		created.InviteLink != "https://mesh.test/invite" {
		t.Fatalf("created: %#v, %v", created, err)
	}
	invites, err := client.ListInvitations(ctx)
	if err != nil || len(invites) != 1 || invites[0].Token != "invite-token" {
		t.Fatalf("invites: %#v, %v", invites, err)
	}
	if err := client.AcceptInvitation(ctx, AcceptInvitationRequest{
		Token: "invite-token", ProviderDomain: "cloud.example.test",
	}); err != nil || !accepted {
		t.Fatalf("accepted=%t error=%v", accepted, err)
	}
	connections, err := client.ListConnections(ctx)
	if err != nil || len(connections) != 1 ||
		connections[0].UserID != "federated-id" {
		t.Fatalf("connections: %#v, %v", connections, err)
	}
	if err := client.DeleteConnection(ctx, DeleteConnectionRequest{
		Provider: "https://cloud.example.test", UserID: "federated-id",
	}); err != nil || !deleted {
		t.Fatalf("deleted=%t error=%v", deleted, err)
	}
}

func TestFederationClientRejectsIncompleteMutations(t *testing.T) {
	client := NewClient(httpapi.Config{Server: "http://127.0.0.1:1"}, nil)
	if err := client.AcceptInvitation(
		context.Background(), AcceptInvitationRequest{},
	); err == nil {
		t.Fatal("accepted an empty invitation")
	}
	if err := client.DeleteConnection(
		context.Background(), DeleteConnectionRequest{},
	); err == nil {
		t.Fatal("deleted an empty connection")
	}
}
