package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

func TestReceivedShareLifecycleAndStateFiltering(t *testing.T) {
	state := 1
	mutations := 0
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		const endpoint = "/ocs/v2.php/apps/files_sharing/api/v1/shares"
		switch {
		case request.Method == http.MethodGet && request.URL.Path == endpoint:
			requested := request.URL.Query().Get("state")
			if requested != "all" && requested != "" &&
				requested != "0" && requested != "1" && requested != "2" {
				t.Fatalf("unexpected state filter: %q", requested)
			}
			writeAppOCS(writer, `[{
				"id":"received-id","share_type":0,
				"path":"/Shares/incoming.txt","uid_owner":"alice",
				"displayname_owner":"Alice","permissions":1,
				"state":`+strconv.Itoa(state)+`,
				"file_source":"storage$other!incoming"
			}]`)
		case request.URL.Path == endpoint+"/pending/received-id" &&
			request.Method == http.MethodPost:
			mutations++
			state = 0
			writeAppOCS(writer, `{}`)
		case request.URL.Path == endpoint+"/pending/received-id" &&
			request.Method == http.MethodDelete:
			mutations++
			state = 2
			writeAppOCS(writer, `{}`)
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL)
		}
	}))
	defer server.Close()
	configureSpaceTestProfile(t, server.URL, "")

	var pending bytes.Buffer
	if err := RunShareWithOptions(
		context.Background(), ShareRequest{
			Operation: ShareReceived, State: "pending",
		}, "", RunOptions{Out: &pending, OutputMode: appoutput.JSON},
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pending.String(), `"state": 1`) ||
		!strings.Contains(pending.String(), `"stateName": "pending"`) {
		t.Fatalf("pending output: %s", pending.String())
	}

	var plan bytes.Buffer
	if err := RunShareWithOptions(
		context.Background(), ShareRequest{
			Operation: ShareAccept, ID: "received-id", DryRun: true,
		}, "", RunOptions{Out: &plan},
	); err != nil {
		t.Fatal(err)
	}
	if mutations != 0 || !strings.Contains(plan.String(), "Would accept") {
		t.Fatalf("mutations=%d plan=%s", mutations, plan.String())
	}

	if err := RunShareWithOptions(
		context.Background(), ShareRequest{
			Operation: ShareAccept, ID: "received-id",
		}, "", RunOptions{Out: io.Discard},
	); err != nil {
		t.Fatal(err)
	}
	if state != 0 || mutations != 1 {
		t.Fatalf("state=%d mutations=%d after accept", state, mutations)
	}

	var declined bytes.Buffer
	if err := RunShareWithOptions(
		context.Background(), ShareRequest{
			Operation: ShareDecline, ID: "received-id",
		}, "", RunOptions{Out: &declined, OutputMode: appoutput.JSON},
	); err != nil {
		t.Fatal(err)
	}
	if state != 2 || mutations != 2 ||
		!strings.Contains(declined.String(), `"state": "declined"`) {
		t.Fatalf(
			"state=%d mutations=%d output=%s", state, mutations, declined.String(),
		)
	}
}

func TestReceivedShareValidationAndMissingIDFailBeforeMutation(t *testing.T) {
	err := RunShareWithOptions(
		context.Background(), ShareRequest{
			Operation: ShareReceived, State: "future",
		}, "", RunOptions{Out: io.Discard},
	)
	if !apperror.IsKind(err, apperror.KindUsage) ||
		!strings.Contains(err.Error(), "expected accepted, pending, declined, or all") {
		t.Fatalf("state error: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.Method != http.MethodGet ||
			request.URL.Query().Get("state") != "all" {
			t.Fatalf("unexpected mutation: %s %s", request.Method, request.URL)
		}
		writeAppOCS(writer, `[]`)
	}))
	defer server.Close()
	configureSpaceTestProfile(t, server.URL, "")
	err = RunShareWithOptions(
		context.Background(), ShareRequest{
			Operation: ShareAccept, ID: "missing",
		}, "", RunOptions{Out: io.Discard},
	)
	if !apperror.IsKind(err, apperror.KindNotFound) ||
		!strings.Contains(err.Error(), `received share "missing" was not found`) {
		t.Fatalf("missing error: %v", err)
	}
}
