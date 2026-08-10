package graph

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mzner/ocis-cli/internal/httpapi"
)

func TestSearchDirectory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.URL.Query().Get("$search") != "Einstein & Co" {
			t.Fatalf("query: %s", request.URL.RawQuery)
		}
		switch request.URL.Path {
		case "/graph/v1.0/users":
			_, _ = io.WriteString(writer, `{"value":[{
				"id":"user-id","displayName":"Albert Einstein",
				"onPremisesSamAccountName":"einstein",
				"mail":"albert@example.test","attributes":["Albert Einstein"]
			}]}`)
		case "/graph/v1.0/groups":
			_, _ = io.WriteString(writer, `{"value":[{
				"id":"group-id","displayName":"Einstein & Co",
				"groupTypes":["ReadOnly"]
			}]}`)
		default:
			t.Fatalf("path: %s", request.URL.Path)
		}
	}))
	defer server.Close()
	client := NewClient(httpapi.Config{Server: server.URL}, server.Client())

	users, err := client.SearchUsers(context.Background(), "Einstein & Co")
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].Username != "einstein" ||
		users[0].Mail != "albert@example.test" {
		t.Fatalf("users: %#v", users)
	}
	groups, err := client.SearchGroups(context.Background(), "Einstein & Co")
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].ID != "group-id" {
		t.Fatalf("groups: %#v", groups)
	}
}

func TestSearchFederatedUsersUsesRequiredFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.URL.Path != "/graph/v1.0/users" ||
			request.URL.Query().Get("$search") != "bob@example.test" ||
			request.URL.Query().Get("$filter") != "userType eq 'Federated'" {
			t.Fatalf("request: %s?%s", request.URL.Path, request.URL.RawQuery)
		}
		_, _ = io.WriteString(writer, `{"value":[{
			"id":"federated-id","displayName":"Bob","userType":"Federated",
			"identities":[{"issuer":"https://remote.test",
			"issuerAssignedId":"remote-id"}]
		}]}`)
	}))
	defer server.Close()
	client := NewClient(httpapi.Config{Server: server.URL}, server.Client())
	users, err := client.SearchFederatedUsers(
		context.Background(), "bob@example.test",
	)
	if err != nil || len(users) != 1 || users[0].UserType != "Federated" ||
		len(users[0].Identities) != 1 ||
		users[0].Identities[0].IssuerAssignedID != "remote-id" {
		t.Fatalf("users: %#v, %v", users, err)
	}
}

func TestAdministrativeDirectoryReads(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		requests = append(
			requests, request.Method+" "+request.URL.RequestURI(),
		)
		switch request.URL.Path {
		case "/graph/v1.0/users":
			if request.URL.Query().Get("$orderby") != "displayName asc" {
				t.Fatalf("user ordering: %s", request.URL.RawQuery)
			}
			_, _ = io.WriteString(writer, `{"value":[{
				"id":"user-id","displayName":"Alice Example",
				"onPremisesSamAccountName":"alice",
				"mail":"alice@example.test","accountEnabled":true,
				"userType":"Member"
			}]}`)
		case "/graph/v1.0/users/user-id":
			_, _ = io.WriteString(writer, `{
				"id":"user-id","displayName":"Alice Example",
				"onPremisesSamAccountName":"alice","accountEnabled":true
			}`)
		case "/graph/v1.0/groups":
			if request.URL.Query().Get("$search") != `"team"` ||
				request.URL.Query().Get("$orderby") != "displayName asc" {
				t.Fatalf("group query: %s", request.URL.RawQuery)
			}
			_, _ = io.WriteString(writer, `{"value":[{
				"id":"group-id","displayName":"Team",
				"description":"Example team","groupTypes":["ReadOnly"]
			}]}`)
		case "/graph/v1.0/groups/group-id":
			_, _ = io.WriteString(writer, `{
				"id":"group-id","displayName":"Team",
				"description":"Example team","groupTypes":["ReadOnly"]
			}`)
		case "/graph/v1.0/groups/group-id/members":
			_, _ = io.WriteString(writer, `[{
				"id":"user-id","displayName":"Alice Example",
				"onPremisesSamAccountName":"alice"
			}]`)
		default:
			t.Fatalf("path: %s", request.URL.Path)
		}
	}))
	defer server.Close()
	client := NewClient(httpapi.Config{Server: server.URL}, server.Client())

	users, err := client.ListUsers(
		context.Background(), DirectorySearch{},
	)
	if err != nil || len(users) != 1 ||
		users[0].AccountEnabled == nil || !*users[0].AccountEnabled {
		t.Fatalf("users: %#v, %v", users, err)
	}
	user, err := client.GetUser(context.Background(), "user-id")
	if err != nil || user.Username != "alice" {
		t.Fatalf("user: %#v, %v", user, err)
	}
	groups, err := client.ListGroups(context.Background(), DirectorySearch{
		Value: "team",
	})
	if err != nil || len(groups) != 1 ||
		groups[0].Description != "Example team" {
		t.Fatalf("groups: %#v, %v", groups, err)
	}
	group, err := client.GetGroup(context.Background(), "group-id")
	if err != nil || len(group.GroupTypes) != 1 {
		t.Fatalf("group: %#v, %v", group, err)
	}
	members, err := client.ListGroupMembers(
		context.Background(), "group-id",
	)
	if err != nil || len(members) != 1 ||
		members[0].ID != "user-id" {
		t.Fatalf("members: %#v, %v", members, err)
	}
	if len(requests) != 5 {
		t.Fatalf("requests: %#v", requests)
	}
}

func TestDirectorySearchExpression(t *testing.T) {
	for _, test := range []struct {
		name   string
		search DirectorySearch
		want   string
		err    bool
	}{
		{name: "empty", search: DirectorySearch{}, want: ""},
		{
			name: "literal phrase",
			search: DirectorySearch{
				Value: " CLI Admin-Test 2026 ",
			},
			want: `"CLI Admin-Test 2026"`,
		},
		{
			name: "raw",
			search: DirectorySearch{
				Value: `"engineering"`, Mode: DirectorySearchRaw,
			},
			want: `"engineering"`,
		},
		{
			name: "literal quote",
			search: DirectorySearch{
				Value: `Alice "Admin"`,
			},
			err: true,
		},
		{
			name: "unknown mode",
			search: DirectorySearch{
				Value: "alice", Mode: DirectorySearchMode(99),
			},
			err: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := directorySearchExpression(test.search)
			if (err != nil) != test.err || got != test.want {
				t.Fatalf("expression=%q err=%v", got, err)
			}
		})
	}
}
