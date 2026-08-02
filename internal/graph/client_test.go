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

func TestListMyDrives(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.URL.Path != "/graph/v1.0/me/drives" {
			t.Fatalf("path: %s", request.URL.Path)
		}
		_, _ = io.WriteString(writer, `{"value":[{
			"id":"space-id","name":"Engineering","driveType":"project",
			"driveAlias":"project/engineering","quota":{"used":5,"total":10}
		}]}`)
	}))
	defer server.Close()
	client := NewClient(httpapi.Config{Server: server.URL}, server.Client())
	spaces, err := client.ListMyDrives(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(spaces) != 1 || spaces[0].ID != "space-id" ||
		spaces[0].Quota.Total != 10 {
		t.Fatalf("spaces: %#v", spaces)
	}
}

func TestListDrives(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.URL.Path != "/graph/v1.0/drives" {
			t.Fatalf("path: %s", request.URL.Path)
		}
		_, _ = io.WriteString(writer, `{"value":[{
			"id":"admin-visible-space","name":"Operations","driveType":"project"
		}]}`)
	}))
	defer server.Close()
	client := NewClient(httpapi.Config{Server: server.URL}, server.Client())
	spaces, err := client.ListDrives(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(spaces) != 1 || spaces[0].ID != "admin-visible-space" {
		t.Fatalf("spaces: %#v", spaces)
	}
}

func TestListMyDrivesReturnsTypedHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	client := NewClient(httpapi.Config{Server: server.URL}, server.Client())
	_, err := client.ListMyDrives(context.Background())
	statusErr, ok := err.(interface{ HTTPStatusCode() int })
	if !ok || statusErr.HTTPStatusCode() != http.StatusUnauthorized {
		t.Fatalf("error: %v", err)
	}
}

func TestCreateDrive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.Method != http.MethodPost ||
			request.URL.Path != "/graph/v1.0/drives" {
			t.Fatalf("request: %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("content type: %q", request.Header.Get("Content-Type"))
		}
		var body CreateDriveRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Name != "Engineering" || body.Description != "Shared work" ||
			body.DriveType != "project" || body.Quota == nil ||
			body.Quota.Total != 2_000_000_000 {
			t.Fatalf("body: %#v", body)
		}
		writer.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(writer, `{
			"id":"space-id","name":"Engineering","description":"Shared work",
			"driveType":"project","quota":{"total":2000000000}
		}`)
	}))
	defer server.Close()
	client := NewClient(httpapi.Config{Server: server.URL}, server.Client())
	drive, err := client.CreateDrive(context.Background(), CreateDriveRequest{
		Name: "Engineering", Description: "Shared work", DriveType: "project",
		Quota: &CreateQuota{Total: 2_000_000_000},
	})
	if err != nil {
		t.Fatal(err)
	}
	if drive.ID != "space-id" || drive.Quota.Total != 2_000_000_000 {
		t.Fatalf("drive: %#v", drive)
	}
}

func TestCreateDriveOmitsDefaultQuota(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if _, ok := body["quota"]; ok {
			t.Fatalf("default quota must be omitted: %#v", body)
		}
		writer.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(writer, `{
			"id":"space-id","name":"Engineering","driveType":"project"
		}`)
	}))
	defer server.Close()
	client := NewClient(httpapi.Config{Server: server.URL}, server.Client())
	_, err := client.CreateDrive(context.Background(), CreateDriveRequest{
		Name: "Engineering", DriveType: "project",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCreateDriveReturnsTypedHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		writer.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(writer, `{"error":{"message":"access denied"}}`)
	}))
	defer server.Close()
	client := NewClient(httpapi.Config{Server: server.URL}, server.Client())
	_, err := client.CreateDrive(context.Background(), CreateDriveRequest{
		Name: "Engineering", DriveType: "project",
	})
	statusErr, ok := err.(interface{ HTTPStatusCode() int })
	if !ok || statusErr.HTTPStatusCode() != http.StatusForbidden {
		t.Fatalf("error: %v", err)
	}
}
