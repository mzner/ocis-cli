package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

const appNotificationResponse = `{"ocs":{"meta":{
	"status":"ok","statuscode":200,"message":"OK"
},"data":[{
	"notification_id":"notification-1","app":"userlog","user":"Alice",
	"datetime":"2026-08-10T08:00:00Z","object_id":"share-1",
	"object_type":"share","subject":"Resource shared",
	"message":"Alice shared report.txt with you"
},{
	"notification_id":"notification-2","app":"userlog","user":"System",
	"datetime":"2026-08-10T09:00:00Z","object_id":"file-1",
	"object_type":"resource","subject":"Virus found",
	"message":"A virus was found in upload.txt"
}]}}`

func TestNotificationUseCases(t *testing.T) {
	var dismissed string
	var dismissedMany []string
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch request.Method + " " + request.URL.Path {
		case "GET /ocs/v2.php/apps/notifications/api/v1/notifications":
			_, _ = io.WriteString(writer, appNotificationResponse)
		case "DELETE /ocs/v2.php/apps/notifications/api/v1/notifications/notification-1":
			dismissed = "notification-1"
		case "DELETE /ocs/v2.php/apps/notifications/api/v1/notifications":
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
	configureSpaceTestProfile(t, server.URL, "")

	var list bytes.Buffer
	if err := RunNotificationWithOptions(
		context.Background(), NotificationRequest{
			Operation: NotificationList, Search: "virus",
		}, "", RunOptions{Out: &list, OutputMode: appoutput.JSON},
	); err != nil || !strings.Contains(list.String(), `"id": "notification-2"`) ||
		strings.Contains(list.String(), `"id": "notification-1"`) {
		t.Fatalf("list=%q error=%v", list.String(), err)
	}

	var info bytes.Buffer
	if err := RunNotificationWithOptions(
		context.Background(), NotificationRequest{
			Operation: NotificationInfo, IDs: []string{"notification-1"},
		}, "", RunOptions{Out: &info},
	); err != nil || !strings.Contains(info.String(), "Resource shared") ||
		!strings.Contains(info.String(), "share share-1") {
		t.Fatalf("info=%q error=%v", info.String(), err)
	}

	if err := RunNotificationWithOptions(
		context.Background(), NotificationRequest{
			Operation: NotificationDismiss, IDs: []string{"notification-1"},
			DryRun: true,
		}, "", RunOptions{Out: io.Discard},
	); err != nil || dismissed != "" {
		t.Fatalf("dry-run dismissed=%q error=%v", dismissed, err)
	}
	if err := RunNotificationWithOptions(
		context.Background(), NotificationRequest{
			Operation: NotificationDismiss, IDs: []string{"notification-1"},
		}, "", RunOptions{Out: io.Discard},
	); err != nil || dismissed != "notification-1" {
		t.Fatalf("dismissed=%q error=%v", dismissed, err)
	}

	if err := RunNotificationWithOptions(
		context.Background(), NotificationRequest{
			Operation: NotificationClear, Confirmed: true,
		}, "", RunOptions{Out: io.Discard},
	); err != nil || !reflect.DeepEqual(
		dismissedMany, []string{"notification-1", "notification-2"},
	) {
		t.Fatalf("dismissed=%v error=%v", dismissedMany, err)
	}
}

func TestNotificationClearFailsClosed(t *testing.T) {
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "missing", "config.json"))
	err := RunNotificationWithOptions(
		context.Background(), NotificationRequest{
			Operation: NotificationClear,
		}, "", RunOptions{Out: io.Discard},
	)
	if !apperror.IsKind(err, apperror.KindUsage) ||
		!strings.Contains(err.Error(), "explicit confirmation") {
		t.Fatalf("error: %v", err)
	}
}

func TestNotificationResolutionRejectsUnknownID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		_, _ = io.WriteString(writer, appNotificationResponse)
	}))
	defer server.Close()
	configureSpaceTestProfile(t, server.URL, "")
	err := RunNotificationWithOptions(
		context.Background(), NotificationRequest{
			Operation: NotificationInfo, IDs: []string{"missing"},
		}, "", RunOptions{Out: io.Discard},
	)
	if !apperror.IsKind(err, apperror.KindNotFound) ||
		!strings.Contains(err.Error(), "notification list") {
		t.Fatalf("error: %v", err)
	}
}
