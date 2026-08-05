package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

func TestShareOverviewListsCurrentSharesAcrossSpaces(t *testing.T) {
	server := newShareOverviewServer(t)
	defer server.Close()
	configureSpaceTestProfile(t, server.URL, "")

	var output bytes.Buffer
	if err := RunShareWithOptions(
		context.Background(), ShareRequest{Operation: ShareOverview}, "",
		RunOptions{Out: &output, OutputMode: appoutput.JSON},
	); err != nil {
		t.Fatal(err)
	}
	items := decodeShareOverview(t, output.Bytes())
	if len(items) != 4 {
		t.Fatalf("overview = %#v", items)
	}
	assertOverviewItem(t, items, "out-personal", func(item ShareOverviewItem) bool {
		return item.Direction == "outgoing" && item.State == "active" &&
			item.SpaceName == "Personal" && item.PartyName == "Bob" &&
			item.Permission == "read"
	})
	assertOverviewItem(t, items, "out-project", func(item ShareOverviewItem) bool {
		return item.Direction == "outgoing" && item.SpaceName == "Engineering" &&
			item.Type == "public_link" && item.PartyName == "Release link"
	})
	assertOverviewItem(t, items, "in-accepted", func(item ShareOverviewItem) bool {
		return item.Direction == "received" && item.State == "accepted" &&
			item.SpaceName == "Shares" && item.PartyName == "Alice"
	})
	assertOverviewItem(t, items, "in-pending", func(item ShareOverviewItem) bool {
		return item.Direction == "received" && item.State == "pending" &&
			item.SpaceName == "Engineering" && item.PartyName == "Carol"
	})
	if slices.ContainsFunc(items, func(item ShareOverviewItem) bool {
		return item.ShareID == "in-declined"
	}) {
		t.Fatalf("default overview contains declined share: %#v", items)
	}

	output.Reset()
	if err := RunShareWithOptions(
		context.Background(), ShareRequest{Operation: ShareOverview}, "",
		RunOptions{Out: &output},
	); err != nil {
		t.Fatal(err)
	}
	human := output.String()
	for _, expected := range []string{
		"DIRECTION", "STATE", "SPACE", "SHARE ID", "out-personal", "in-pending",
	} {
		if !strings.Contains(human, expected) {
			t.Fatalf("human overview does not contain %q:\n%s", expected, human)
		}
	}
	if strings.Contains(human, "in-declined") {
		t.Fatalf("human overview contains declined share:\n%s", human)
	}
}

func TestShareOverviewFiltersDirectionStateAndSpace(t *testing.T) {
	server := newShareOverviewServer(t)
	defer server.Close()
	configureSpaceTestProfile(t, server.URL, "")

	for _, test := range []struct {
		name    string
		request ShareRequest
		space   string
		wantIDs []string
	}{
		{
			name: "pending received", request: ShareRequest{
				Operation: ShareOverview, Direction: "received", State: "pending",
			}, wantIDs: []string{"in-pending"},
		},
		{
			name: "declined only", request: ShareRequest{
				Operation: ShareOverview, State: "declined",
			}, wantIDs: []string{"in-declined"},
		},
		{
			name: "project", request: ShareRequest{
				Operation: ShareOverview,
			}, space: "Engineering", wantIDs: []string{"out-project", "in-pending"},
		},
		{
			name: "received virtual space", request: ShareRequest{
				Operation: ShareOverview,
			}, space: "Shares", wantIDs: []string{"in-accepted", "in-pending"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := RunShareWithOptions(
				context.Background(), test.request, "", RunOptions{
					Out: &output, OutputMode: appoutput.JSON, Space: test.space,
				},
			); err != nil {
				t.Fatal(err)
			}
			items := decodeShareOverview(t, output.Bytes())
			actual := make([]string, 0, len(items))
			for _, item := range items {
				actual = append(actual, item.ShareID)
			}
			slices.Sort(actual)
			slices.Sort(test.wantIDs)
			if !slices.Equal(actual, test.wantIDs) {
				t.Fatalf("share IDs = %v, want %v; items=%#v", actual, test.wantIDs, items)
			}
		})
	}
}

func TestShareOverviewRejectsInvalidFiltersBeforeLoadingProfile(t *testing.T) {
	t.Setenv(
		"OCIS_CONFIG", filepath.Join(t.TempDir(), "missing", "config.json"),
	)
	for _, request := range []ShareRequest{
		{Operation: ShareOverview, Direction: "sideways"},
		{Operation: ShareOverview, State: "expired"},
		{Operation: ShareOverview, Direction: "outgoing", State: "pending"},
	} {
		err := RunShareWithOptions(
			context.Background(), request, "", RunOptions{Out: io.Discard},
		)
		if !apperror.IsKind(err, apperror.KindUsage) {
			t.Fatalf("request %#v: %v", request, err)
		}
	}
}

func newShareOverviewServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch request.URL.Path {
		case "/graph/v1.0/me/drives":
			_, _ = io.WriteString(writer, `{"value":[
				{"id":"storage$alice","name":"Personal","driveType":"personal","driveAlias":"personal/alice"},
				{"id":"storage$project","name":"Engineering","driveType":"project","driveAlias":"project/engineering"},
				{"id":"virtual$shares","name":"Shares","driveType":"virtual","driveAlias":"virtual/shares"}
			]}`)
		case "/ocs/v2.php/apps/files_sharing/api/v1/shares":
			query := request.URL.Query()
			if query.Get("shared_with_me") == "true" {
				if query.Get("state") != "all" || query.Get("share_types") != "0,1" {
					t.Fatalf("received query: %s", request.URL.RawQuery)
				}
				writeAppOCS(writer, `[
					{"id":"in-accepted","share_type":0,"path":"/Shares/plan.txt","uid_owner":"alice","displayname_owner":"Alice","permissions":1,"state":0,"space_id":"storage$alice-other!alice"},
					{"id":"in-pending","share_type":1,"path":"/Shares/design","uid_owner":"carol","displayname_owner":"Carol","permissions":15,"state":1,"space_id":"storage$project!project"},
					{"id":"in-declined","share_type":0,"path":"/Shares/old.txt","uid_owner":"dave","displayname_owner":"Dave","permissions":1,"state":2,"space_id":"storage$project!project"}
				]`)
				return
			}
			if query.Get("reshares") != "true" {
				t.Fatalf("outgoing query: %s", request.URL.RawQuery)
			}
			if query.Get("space_ref") == "storage$project" {
				writeAppOCS(writer, `[{
					"id":"out-project","share_type":3,"path":"/release",
					"name":"Release link","url":"https://cloud.test/s/release",
					"permissions":1,"space_id":"storage$project!project"
				}]`)
				return
			}
			if query.Get("space_ref") != "" {
				t.Fatalf("unexpected space filter: %s", request.URL.RawQuery)
			}
			writeAppOCS(writer, `[
				{"id":"out-personal","share_type":0,"path":"/report.txt","share_with":"bob","share_with_displayname":"Bob","permissions":1,"space_id":"storage$alice!alice"},
				{"id":"out-project","share_type":3,"path":"/release","name":"Release link","url":"https://cloud.test/s/release","permissions":1,"space_id":"storage$project!project"}
			]`)
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL)
		}
	}))
}

func decodeShareOverview(t *testing.T, data []byte) []ShareOverviewItem {
	t.Helper()
	var envelope struct {
		Type string              `json:"type"`
		Data []ShareOverviewItem `json:"data"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Type != "share-overview" {
		t.Fatalf("output type = %q", envelope.Type)
	}
	return envelope.Data
}

func assertOverviewItem(
	t *testing.T, items []ShareOverviewItem, id string,
	check func(ShareOverviewItem) bool,
) {
	t.Helper()
	for _, item := range items {
		if item.ShareID == id {
			if !check(item) {
				t.Fatalf("share %s = %#v", id, item)
			}
			return
		}
	}
	t.Fatalf("share %s not found in %#v", id, items)
}
