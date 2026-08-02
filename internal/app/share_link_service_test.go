package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

func TestPublicLinkInfoAndUpdate(t *testing.T) {
	state := struct {
		name       string
		linkType   string
		expiration any
		password   bool
		patches    int
		passwords  int
	}{
		name: "Report", linkType: "view",
		expiration: "2026-08-31T00:00:00Z", password: false,
	}
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		const permissionPath = "/graph/v1beta1/drives/storage$space/items/" +
			"storage$space!file/permissions/42"
		switch {
		case request.Method == http.MethodGet &&
			strings.HasSuffix(request.URL.Path, "/shares/42"):
			writeAppOCS(writer, `[{
				"id":"42","share_type":3,
				"url":"https://cloud.test/s/token","path":"/report.pdf",
				"name":"Report","permissions":1,"space_id":"space-id",
				"file_source":"storage$space!file"
			}]`)
		case request.Method == http.MethodGet &&
			request.URL.Path == permissionPath:
			expiration, err := json.Marshal(state.expiration)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.WriteString(writer, `{
				"id":"42","hasPassword":`+
				boolJSON(state.password)+`,"expirationDateTime":`+
				string(expiration)+`,"link":{"type":"`+state.linkType+
				`","webUrl":"https://cloud.test/s/token",`+
				`"@libre.graph.displayName":`+stringJSON(state.name)+`}}`)
		case request.Method == http.MethodPost &&
			request.URL.Path == permissionPath+"/setPassword":
			var body map[string]string
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			state.password = body["password"] != ""
			state.passwords++
			_, _ = io.WriteString(
				writer, `{"id":"42","hasPassword":`+
					boolJSON(state.password)+`}`,
			)
		case request.Method == http.MethodPatch &&
			request.URL.Path == permissionPath:
			var body struct {
				Expiration json.RawMessage `json:"expirationDateTime"`
				Link       struct {
					Type *string `json:"type"`
					Name *string `json:"@libre.graph.displayName"`
				} `json:"link"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Link.Type != nil {
				state.linkType = *body.Link.Type
			}
			if body.Link.Name != nil {
				state.name = *body.Link.Name
			}
			if body.Expiration != nil {
				if string(body.Expiration) == "null" {
					state.expiration = nil
				} else if err := json.Unmarshal(
					body.Expiration, &state.expiration,
				); err != nil {
					t.Fatal(err)
				}
			}
			state.patches++
			_, _ = io.WriteString(
				writer, `{"id":"42","link":{"type":"`+
					state.linkType+`"}}`,
			)
		default:
			t.Fatalf(
				"unexpected request: %s %s",
				request.Method, request.URL.Path,
			)
		}
	}))
	defer server.Close()

	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := saveStore(defaultDependencies(), &store{
		Current: "work",
		Profiles: map[string]profile{"work": {
			Server: server.URL, Username: "alice",
			AuthType: "basic", Password: "secret",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	options := RunOptions{Out: &output, OutputMode: appoutput.JSON}
	if err := RunShareWithOptions(
		context.Background(),
		ShareRequest{Operation: ShareLinkInfo, ID: "42"},
		"", options,
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"passwordProtected": false`) ||
		!strings.Contains(output.String(), `"linkType": "view"`) {
		t.Fatalf("info output: %s", output.String())
	}

	if err := RunShareWithOptions(
		context.Background(),
		ShareRequest{
			Operation: ShareLinkUpdate, ID: "42",
			Name: "Dry run", UpdateName: true,
			UpdatePassword: true, DryRun: true,
		},
		"", RunOptions{Out: io.Discard},
	); err != nil {
		t.Fatal(err)
	}
	if state.name != "Report" || state.patches != 0 ||
		state.passwords != 0 {
		t.Fatalf("dry run changed state: %#v", state)
	}

	output.Reset()
	if err := RunShareWithOptions(
		context.Background(),
		ShareRequest{
			Operation: ShareLinkUpdate, ID: "42",
			Name: "Quarterly", UpdateName: true,
			Expiration: "2026-09-30", UpdateExpiration: true,
			Permissions: 15, UpdateAccess: true,
			Password: "new-secret", UpdatePassword: true,
		},
		"", options,
	); err != nil {
		t.Fatal(err)
	}
	if state.name != "Quarterly" || state.linkType != "edit" ||
		state.expiration != "2026-09-30T00:00:00Z" ||
		!state.password || state.patches != 1 || state.passwords != 1 {
		t.Fatalf("updated state: %#v", state)
	}

	if err := RunShareWithOptions(
		context.Background(),
		ShareRequest{
			Operation: ShareLinkUpdate, ID: "42",
			UpdateExpiration: true,
			UpdatePassword:   true, RemovePassword: true,
		},
		"", RunOptions{Out: io.Discard},
	); err != nil {
		t.Fatal(err)
	}
	if state.expiration != nil || state.password ||
		state.patches != 2 || state.passwords != 2 {
		t.Fatalf("cleared state: %#v", state)
	}
}

func TestPublicLinkUpdateValidation(t *testing.T) {
	err := RunShareWithOptions(
		context.Background(),
		ShareRequest{Operation: ShareLinkUpdate, ID: "42"},
		"", RunOptions{Out: io.Discard},
	)
	if !apperror.IsKind(err, apperror.KindUsage) {
		t.Fatalf("empty update: %v", err)
	}
	err = RunShareWithOptions(
		context.Background(),
		ShareRequest{
			Operation: ShareLinkUpdate, ID: "42",
			UpdateExpiration: true, Expiration: "tomorrow",
		},
		"", RunOptions{Out: io.Discard},
	)
	if !apperror.IsKind(err, apperror.KindUsage) {
		t.Fatalf("invalid expiration: %v", err)
	}
}

func boolJSON(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func stringJSON(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}
