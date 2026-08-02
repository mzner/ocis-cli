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

func TestDriveAdministration(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		requests = append(requests, request.Method+" "+request.URL.Path)
		switch request.Method {
		case http.MethodGet:
			_, _ = io.WriteString(writer, `{
				"id":"space-id","name":"Engineering","driveType":"project"
			}`)
		case http.MethodPatch:
			if request.Header.Get("Restore") == "true" {
				var body map[string]any
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if len(body) != 0 {
					t.Fatalf("restore body: %#v", body)
				}
				_, _ = io.WriteString(writer, `{
					"id":"space-id","name":"Engineering","driveType":"project"
				}`)
				return
			}
			var body UpdateDriveRequest
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Name == nil || *body.Name != "Platform" ||
				body.Description == nil || *body.Description != "" ||
				body.Quota == nil || body.Quota.Total != 5_000 {
				t.Fatalf("update body: %#v", body)
			}
			_, _ = io.WriteString(writer, `{
				"id":"space-id","name":"Platform","driveType":"project",
				"quota":{"total":5000}
			}`)
		case http.MethodDelete:
			if len(requests) == 4 && request.Header.Get("Purge") != "" {
				t.Fatalf("disable request unexpectedly purged")
			}
			if len(requests) == 5 && request.Header.Get("Purge") != "true" {
				t.Fatalf("purge header: %q", request.Header.Get("Purge"))
			}
			writer.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("request: %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(httpapi.Config{Server: server.URL}, server.Client())
	drive, err := client.GetDrive(context.Background(), "space-id")
	if err != nil || drive.Name != "Engineering" {
		t.Fatalf("get: %#v, %v", drive, err)
	}
	name, description := "Platform", ""
	drive, err = client.UpdateDrive(
		context.Background(), "space-id",
		UpdateDriveRequest{
			Name: &name, Description: &description,
			Quota: &CreateQuota{Total: 5_000},
		},
	)
	if err != nil || drive.Name != "Platform" {
		t.Fatalf("update: %#v, %v", drive, err)
	}
	if _, err := client.RestoreDrive(context.Background(), "space-id"); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteDrive(context.Background(), "space-id", false); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteDrive(context.Background(), "space-id", true); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 5 {
		t.Fatalf("requests: %#v", requests)
	}
}

func TestDriveAdministrationReturnsTypedError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		writer.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	client := NewClient(httpapi.Config{Server: server.URL}, server.Client())
	err := client.DeleteDrive(context.Background(), "space-id", false)
	statusErr, ok := err.(interface{ HTTPStatusCode() int })
	if !ok || statusErr.HTTPStatusCode() != http.StatusForbidden {
		t.Fatalf("error: %v", err)
	}
}
