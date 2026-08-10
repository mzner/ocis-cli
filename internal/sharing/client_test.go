package sharing

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/mzner/ocis-cli/internal/httpapi"
)

func TestCreateListAndRevokePublicLink(t *testing.T) {
	var revoked bool
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.Header.Get("OCS-APIRequest") != "true" {
			t.Fatal("missing OCS header")
		}
		switch request.Method {
		case http.MethodPost:
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatal(err)
			}
			values, err := url.ParseQuery(string(body))
			if err != nil {
				t.Fatal(err)
			}
			if values.Get("space_ref") != "space-id/reports/report.pdf" ||
				values.Get("shareType") != "3" || values.Get("permissions") != "1" {
				t.Fatalf("form: %#v", values)
			}
			writeOCS(writer, `{
				"id":"42","share_type":3,"url":"https://cloud.test/s/token",
				"path":"/reports/report.pdf","permissions":1
			}`)
		case http.MethodGet:
			if request.URL.Query().Get("space_ref") != "space-id/reports" {
				t.Fatalf("query: %s", request.URL.RawQuery)
			}
			writeOCS(writer, `[
				{"id":42,"share_type":"public_link","url":"https://cloud.test/s/token",
				 "file_target":"/report.pdf","permissions":"1"},
				{"id":43,"share_type":0,"file_target":"/private.pdf","permissions":1}
			]`)
		case http.MethodDelete:
			revoked = true
			writeOCS(writer, `null`)
		default:
			t.Fatalf("method: %s", request.Method)
		}
	}))
	defer server.Close()
	client := NewClient(httpapi.Config{Server: server.URL}, server.Client())
	link, err := client.CreateLink(context.Background(), CreateRequest{
		Path: "/reports/report.pdf", SpaceID: "space-id", Permissions: 1,
	})
	if err != nil || link.ID != "42" {
		t.Fatalf("create: %#v, %v", link, err)
	}
	links, err := client.ListLinks(context.Background(), ListRequest{
		Path: "/reports", SpaceID: "space-id",
	})
	if err != nil || len(links) != 1 || links[0].ID != "42" {
		t.Fatalf("list: %#v, %v", links, err)
	}
	if err := client.RevokeLink(context.Background(), link.ID); err != nil {
		t.Fatal(err)
	}
	if !revoked {
		t.Fatal("link was not revoked")
	}
}

func TestCapabilities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.URL.Path != "/ocs/v2.php/cloud/capabilities" {
			t.Fatalf("path: %s", request.URL.Path)
		}
		writeOCS(writer, `{"capabilities":{
			"dav":{"reports":["search-files"]},
			"files":{"tus_support":{
				"version":"1.0.0","resumable":"1.0.0",
				"extension":"creation,creation-with-upload",
				"max_chunk_size":10000000,"http_method_override":"true"
			}},
			"files_sharing":{"api_enabled":true,"group_sharing":true,
			"sharing_roles":true,
			"federation":{"outgoing":true,"incoming":true},"public":{
				"enabled":true,"password":{"enforced":true},
				"expire_date":{"enabled":true}
			}},
			"spaces":{"enabled":true,"projects":true,"version":"1.0.0"},
			"auth":{"mfa":{"enabled":true,
				"levelnames":["urn:mfa"],"session_duration":300}},
			"graph":{"users":{"read_only_attributes":["user.mail"],
				"create_disabled":true,"delete_disabled":true}}
		}}`)
	}))
	defer server.Close()
	client := NewClient(httpapi.Config{Server: server.URL}, server.Client())
	capabilities, err := client.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !capabilities.Sharing.APIEnabled ||
		len(capabilities.DAV.Reports) != 1 ||
		capabilities.DAV.Reports[0] != "search-files" ||
		!capabilities.Sharing.GroupEnabled ||
		!capabilities.Sharing.SharingRoles ||
		!capabilities.Sharing.Federation.Outgoing ||
		!capabilities.Sharing.Federation.Incoming ||
		!capabilities.Sharing.Public.Password.Enforced ||
		capabilities.Files.TUS.MaxChunkSize != 10000000 ||
		len(capabilities.Files.TUS.Extensions) != 2 ||
		!capabilities.Files.TUS.HTTPMethodOverride ||
		!capabilities.Spaces.Projects ||
		!capabilities.Auth.MFA.Enabled ||
		len(capabilities.Auth.MFA.LevelNames) != 1 ||
		capabilities.Auth.MFA.SessionDuration != 300 ||
		!capabilities.Graph.Users.CreateDisabled ||
		!capabilities.Graph.Users.DeleteDisabled ||
		len(capabilities.Graph.Users.ReadOnlyAttributes) != 1 {
		t.Fatalf("capabilities: %#v", capabilities)
	}
}

func TestOCSFailureReturnsTypedError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		_, _ = io.WriteString(writer, `{"ocs":{"meta":{
			"status":"failure","statuscode":404,"message":"missing"
		},"data":[]}}`)
	}))
	defer server.Close()
	client := NewClient(httpapi.Config{Server: server.URL}, server.Client())
	_, err := client.ListLinks(context.Background(), ListRequest{})
	statusErr, ok := err.(interface{ HTTPStatusCode() int })
	if !ok || statusErr.HTTPStatusCode() != http.StatusNotFound {
		t.Fatalf("error: %v", err)
	}
}

func TestPersonalCreateUsesPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		body, _ := io.ReadAll(request.Body)
		values, _ := url.ParseQuery(string(body))
		if values.Get("path") != "/demo" || values.Get("space_ref") != "" {
			t.Fatalf("form: %#v", values)
		}
		writeOCS(writer, `{"id":1,"share_type":3,"url":"https://cloud.test/s/a"}`)
	}))
	defer server.Close()
	client := NewClient(httpapi.Config{Server: server.URL}, server.Client())
	if _, err := client.CreateLink(context.Background(), CreateRequest{
		Path: "/demo", Permissions: 1,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestGetPublicLinkByID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.Method != http.MethodGet ||
			request.URL.Path !=
				"/ocs/v2.php/apps/files_sharing/api/v1/shares/42" {
			t.Fatalf("request: %s %s", request.Method, request.URL.Path)
		}
		writeOCS(writer, `[{
			"id":"42","share_type":3,"url":"https://cloud.test/s/token",
			"path":"/reports/report.pdf","name":"Report","permissions":1,
			"expiration":"2026-08-31","space_id":"space-id",
			"file_source":"storage$space!file",
			"share_with":"***redacted***"
		}]`)
	}))
	defer server.Close()
	client := NewClient(httpapi.Config{Server: server.URL}, server.Client())
	link, err := client.GetLink(context.Background(), "42")
	if err != nil || link.ID != "42" ||
		link.ResourceID != "storage$space!file" ||
		!link.PasswordProtected {
		t.Fatalf("link: %#v, %v", link, err)
	}
}

func writeOCS(writer http.ResponseWriter, data string) {
	writer.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(writer, `{"ocs":{"meta":{
		"status":"ok","statuscode":200,"message":"OK"
	},"data":`+data+`}}`)
}
