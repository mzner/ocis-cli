package sharing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mzner/ocis-cli/internal/httpapi"
)

func TestListOutgoingAndReceivedShares(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		query := request.URL.Query()
		if query.Get("shared_with_me") == "true" {
			if query.Get("share_types") != "0,1" ||
				query.Get("path") != "/Shares/report.pdf" {
				t.Fatalf("received query: %s", request.URL.RawQuery)
			}
			writeOCS(writer, `[{
				"id":"received-id","share_type":0,
				"path":"/Shares/report.pdf","item_type":"file",
				"uid_owner":"alice","displayname_owner":"Alice",
				"permissions":1,"state":0,
				"file_source":"storage$space!file"
			}]`)
			return
		}
		if query.Get("space_ref") != "storage$space/reports" ||
			query.Get("reshares") != "true" {
			t.Fatalf("outgoing query: %s", request.URL.RawQuery)
		}
		writeOCS(writer, `[
			{"id":"link-id","share_type":3,"path":"/reports",
			 "url":"https://cloud.test/s/token","permissions":1},
			{"id":"user-id","share_type":"user","path":"/reports",
			 "share_with":"bob","share_with_displayname":"Bob",
			 "permissions":"15","file_source":"storage$space!folder"}
		]`)
	}))
	defer server.Close()

	client := NewClient(httpapi.Config{Server: server.URL}, server.Client())
	outgoing, err := client.ListShares(
		context.Background(),
		ShareListRequest{Path: "/reports", SpaceID: "storage$space"},
	)
	if err != nil || len(outgoing) != 2 ||
		outgoing[0].Type != "public_link" ||
		outgoing[1].RecipientName != "Bob" ||
		outgoing[1].ResourceID != "storage$space!folder" {
		t.Fatalf("outgoing: %#v, %v", outgoing, err)
	}
	received, err := client.ListShares(
		context.Background(),
		ShareListRequest{Path: "/Shares/report.pdf", Received: true},
	)
	if err != nil || len(received) != 1 ||
		received[0].OwnerName != "Alice" ||
		received[0].Type != "user" {
		t.Fatalf("received: %#v, %v", received, err)
	}
}
