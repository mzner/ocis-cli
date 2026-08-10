package notifications

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/mzner/ocis-cli/internal/httpapi"
)

func TestNotificationLifecycle(t *testing.T) {
	var dismissed string
	var dismissedMany []string
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("authorization: %q", request.Header.Get("Authorization"))
		}
		if request.Header.Get("OCS-APIRequest") != "true" {
			t.Fatalf("OCS-APIRequest: %q", request.Header.Get("OCS-APIRequest"))
		}
		switch request.Method + " " + request.URL.Path {
		case "GET " + endpoint:
			if request.URL.Query().Get("format") != "json" {
				t.Fatalf("query: %s", request.URL.RawQuery)
			}
			_, _ = io.WriteString(writer, `{"ocs":{"meta":{
				"status":"ok","statuscode":200,"message":"OK"
			},"data":[{
				"notification_id":"notification-1","app":"userlog",
				"user":"Alice","datetime":"2026-08-10T08:00:00Z",
				"object_id":"share-1","object_type":"share",
				"subject":"Resource shared","subjectRich":"Resource shared",
				"message":"Alice shared report.txt with you",
				"messageRich":"{user} shared {resource} with you",
				"messageRichParameters":{"resource":{"name":"report.txt"}}
			}]}}`)
		case "DELETE " + endpoint + "/notification-1":
			dismissed = "notification-1"
		case "DELETE " + endpoint:
			if request.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("content type: %q", request.Header.Get("Content-Type"))
			}
			var body struct {
				IDs []string `json:"ids"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			dismissedMany = body.IDs
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(httpapi.Config{
		Server: server.URL, AuthType: "oidc", AccessToken: "token",
	}, server.Client())
	ctx := context.Background()
	values, err := client.List(ctx)
	if err != nil || len(values) != 1 || values[0].ID != "notification-1" ||
		values[0].Subject != "Resource shared" ||
		values[0].MessageDetails["resource"] == nil {
		t.Fatalf("notifications: %#v, %v", values, err)
	}
	if err := client.Dismiss(ctx, "notification-1"); err != nil ||
		dismissed != "notification-1" {
		t.Fatalf("dismissed=%q error=%v", dismissed, err)
	}
	if err := client.DismissMany(
		ctx, []string{"notification-1", "notification-2"},
	); err != nil || !reflect.DeepEqual(
		dismissedMany, []string{"notification-1", "notification-2"},
	) {
		t.Fatalf("dismissed=%v error=%v", dismissedMany, err)
	}
}

func TestListAcceptsNullData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		_, _ = io.WriteString(writer, `{"ocs":{"meta":{
			"status":"ok","statuscode":200,"message":"OK"
		},"data":null}}`)
	}))
	defer server.Close()
	client := NewClient(httpapi.Config{Server: server.URL}, server.Client())
	values, err := client.List(context.Background())
	if err != nil || len(values) != 0 {
		t.Fatalf("notifications: %#v, %v", values, err)
	}
}

func TestNotificationClientValidatesDismissals(t *testing.T) {
	client := NewClient(httpapi.Config{Server: "http://127.0.0.1:1"}, nil)
	if err := client.Dismiss(context.Background(), " "); err == nil {
		t.Fatal("dismissed an empty notification ID")
	}
	if err := client.DismissMany(context.Background(), nil); err == nil {
		t.Fatal("dismissed an empty notification collection")
	}
	if err := client.DismissMany(
		context.Background(), []string{"notification-1", " "},
	); err == nil {
		t.Fatal("dismissed a collection containing an empty ID")
	}
}

func TestListReturnsOCSError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		_, _ = io.WriteString(writer, `{"ocs":{"meta":{
			"status":"failure","statuscode":997,"message":"Unauthenticated"
		},"data":null}}`)
	}))
	defer server.Close()
	client := NewClient(httpapi.Config{Server: server.URL}, server.Client())
	if _, err := client.List(context.Background()); err == nil {
		t.Fatal("accepted an OCS authentication error")
	}
}
