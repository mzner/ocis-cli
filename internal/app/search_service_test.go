package app

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

func TestSearchScopesImplicitPersonalSpaceAndRendersJSON(t *testing.T) {
	var receivedPattern string
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch {
		case request.Method == http.MethodGet &&
			request.URL.Path == "/ocs/v2.php/cloud/capabilities":
			writeAppOCS(writer, `{"capabilities":{
				"dav":{"reports":["search-files"]}
			}}`)
		case request.Method == http.MethodGet &&
			request.URL.Path == "/graph/v1.0/me/drives":
			_, _ = io.WriteString(writer, `{"value":[
				{"id":"storage$personal","name":"Personal","driveType":"personal"},
				{"id":"storage$project","name":"Engineering","driveType":"project"}
			]}`)
		case request.Method == "REPORT" &&
			request.URL.Path == "/remote.php/dav/spaces":
			var body struct {
				Search struct {
					Pattern string `xml:"pattern"`
				} `xml:"search"`
			}
			if err := xml.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			receivedPattern = body.Search.Pattern
			writer.Header().Set("Content-Range", "rows 0-0/1")
			writer.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(writer, appSearchResponse)
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()
	configureSearchProfile(t, server.URL)

	minimum := int64(10)
	after := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	var output bytes.Buffer
	err := RunSearchWithOptions(context.Background(), SearchRequest{
		Query: "report", Path: "/Reports", ResourceType: "file",
		MediaType: "pdf", MinSize: &minimum, ModifiedAfter: &after,
		Limit: 25,
	}, "", RunOptions{
		Out: &output, OutputMode: appoutput.JSON,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`name:"*report*"`, "mediatype:file", `mediatype:"pdf"`,
		"size>=10", "mtime>=2026-07-01T00:00:00Z",
		"scope:storage$personal!personal/Reports",
	} {
		if !strings.Contains(receivedPattern, expected) {
			t.Fatalf("pattern missing %q: %s", expected, receivedPattern)
		}
	}
	var envelope appoutput.Envelope
	if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Type != "search-results" {
		t.Fatalf("envelope: %#v", envelope)
	}
	encoded, _ := json.Marshal(envelope.Data)
	if !strings.Contains(string(encoded), `"spaceName":"Personal"`) ||
		!strings.Contains(string(encoded), `"total":1`) {
		t.Fatalf("data: %s", encoded)
	}
}

func TestSearchAllSpacesDoesNotInjectScope(t *testing.T) {
	var receivedPattern string
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch request.URL.Path {
		case "/ocs/v2.php/cloud/capabilities":
			writeAppOCS(writer, `{"capabilities":{
				"dav":{"reports":["search-files"]}
			}}`)
		case "/graph/v1.0/me/drives":
			_, _ = io.WriteString(writer, `{"value":[]}`)
		case "/remote.php/dav/spaces":
			var body struct {
				Search struct {
					Pattern string `xml:"pattern"`
				} `xml:"search"`
			}
			if err := xml.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			receivedPattern = body.Search.Pattern
			writer.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(writer, `<d:multistatus xmlns:d="DAV:"/>`)
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()
	configureSearchProfile(t, server.URL)
	if err := RunSearchWithOptions(context.Background(), SearchRequest{
		Query: "name:*budget*", Raw: true, AllSpaces: true, Limit: 10,
	}, "", RunOptions{Out: io.Discard}); err != nil {
		t.Fatal(err)
	}
	if receivedPattern != "name:*budget*" {
		t.Fatalf("pattern: %s", receivedPattern)
	}
}

func TestSearchRequiresAdvertisedCapability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.URL.Path != "/ocs/v2.php/cloud/capabilities" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		writeAppOCS(writer, `{"capabilities":{"dav":{"reports":[]}}}`)
	}))
	defer server.Close()
	configureSearchProfile(t, server.URL)
	err := RunSearchWithOptions(context.Background(), SearchRequest{
		Query: "report", Limit: 10,
	}, "", RunOptions{Out: io.Discard})
	if !apperror.IsKind(err, apperror.KindConflict) ||
		!strings.Contains(err.Error(), "does not advertise") {
		t.Fatalf("error: %v", err)
	}
}

func TestSearchValidationIsFailFast(t *testing.T) {
	minimum, maximum := int64(20), int64(10)
	tests := []struct {
		name    string
		request SearchRequest
		options RunOptions
	}{
		{name: "empty", request: SearchRequest{}},
		{
			name:    "scope injection",
			request: SearchRequest{Query: "report scope:storage$space!space"},
		},
		{
			name:    "all spaces and selection",
			request: SearchRequest{Query: "report", AllSpaces: true},
			options: RunOptions{Space: "Engineering"},
		},
		{
			name: "all spaces and path",
			request: SearchRequest{
				Query: "report", AllSpaces: true, Path: "/Reports",
			},
		},
		{
			name:    "raw content",
			request: SearchRequest{Query: "report", Raw: true, Content: true},
		},
		{
			name:    "invalid type",
			request: SearchRequest{Query: "report", ResourceType: "folder"},
		},
		{
			name: "size range",
			request: SearchRequest{
				Query: "report", MinSize: &minimum, MaxSize: &maximum,
			},
		},
		{name: "limit", request: SearchRequest{Query: "report", Limit: 1001}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := RunSearchWithOptions(
				context.Background(), test.request, "", test.options,
			)
			if !apperror.IsKind(err, apperror.KindUsage) {
				t.Fatalf("error: %v", err)
			}
		})
	}
}

func TestBuildSearchPatternRejectsInvalidScopeCombination(t *testing.T) {
	_, err := buildSearchPattern(SearchRequest{
		Query: "report", AllSpaces: true, Path: "/Reports",
	}, nil)
	if !apperror.IsKind(err, apperror.KindUsage) {
		t.Fatalf("error: %v", err)
	}
	_, err = buildSearchPattern(SearchRequest{
		Query: "name:*report*", Raw: true, Content: true,
	}, nil)
	if !apperror.IsKind(err, apperror.KindUsage) {
		t.Fatalf("error: %v", err)
	}
}

func configureSearchProfile(t *testing.T, server string) {
	t.Helper()
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := saveStore(defaultDependencies(), &store{
		Current: "work",
		Profiles: map[string]profile{"work": {
			Server: server, Username: "alice",
			AuthType: "basic", Password: "secret",
		}},
	}); err != nil {
		t.Fatal(err)
	}
}

const appSearchResponse = `<d:multistatus xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns">
	<d:response>
		<d:href>/remote.php/dav/spaces/storage$personal/Reports/report.pdf</d:href>
		<d:propstat><d:prop>
			<oc:name>report.pdf</oc:name>
			<oc:spaceid>storage$personal</oc:spaceid>
			<d:getcontenttype>application/pdf</d:getcontenttype>
			<d:getcontentlength>42</d:getcontentlength>
			<d:resourcetype/><oc:score>1</oc:score>
		</d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat>
	</d:response>
</d:multistatus>`
