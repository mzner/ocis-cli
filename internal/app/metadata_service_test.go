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

func TestMetadataLifecycle(t *testing.T) {
	tags := []string{"approved"}
	favorite := false
	propertyValue := "draft"
	graphMutations := 0
	propertyMutations := 0
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case request.Method == "PROPFIND" &&
			strings.Contains(string(body), "review-status"):
			writePropertyResponse(
				writer, "https://example.test/metadata",
				"review-status", propertyValue, "HTTP/1.1 200 OK",
			)
		case request.Method == "PROPFIND":
			writeMetadataStat(writer, tags, favorite)
		case request.URL.Path ==
			"/graph/v1.0/extensions/org.libregraph/tags":
			var payload struct {
				ResourceID string   `json:"resourceId"`
				Tags       []string `json:"tags"`
			}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatal(err)
			}
			if payload.ResourceID != "storage$space!report" {
				t.Fatalf("resource ID: %q", payload.ResourceID)
			}
			graphMutations++
			if request.Method == http.MethodPut {
				tags = append(tags, payload.Tags...)
			} else {
				tags = nil
			}
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodOptions:
			writer.Header().Set(
				"Allow", "OPTIONS, PROPFIND, PROPPATCH",
			)
			writer.WriteHeader(http.StatusOK)
		case request.Method == "PROPPATCH":
			propertyMutations++
			if strings.Contains(string(body), "favorite") {
				favorite = strings.Contains(string(body), ">1<")
				writePropertyResponse(
					writer, "http://owncloud.org/ns", "favorite",
					"", "HTTP/1.1 200 OK",
				)
				return
			}
			if strings.Contains(string(body), "remove") {
				propertyValue = ""
				writePropertyResponse(
					writer, "https://example.test/metadata",
					"review-status", "", "HTTP/1.1 204 No Content",
				)
				return
			}
			propertyValue = "ready"
			writePropertyResponse(
				writer, "https://example.test/metadata",
				"review-status", "", "HTTP/1.1 200 OK",
			)
		default:
			t.Fatalf(
				"unexpected request: %s %s", request.Method, request.URL.Path,
			)
		}
	}))
	defer server.Close()
	configureMetadataTestProfile(t, server.URL)

	var output bytes.Buffer
	err := RunMetadataWithOptions(
		context.Background(),
		MetadataRequest{
			Operation: MetadataTagList, Path: "/report.txt",
		},
		"", RunOptions{Out: &output},
	)
	if err != nil || output.String() != "approved\n" {
		t.Fatalf("list: %q, %v", output.String(), err)
	}

	output.Reset()
	err = RunMetadataWithOptions(
		context.Background(),
		MetadataRequest{
			Operation: MetadataTagAdd, Path: "/report.txt",
			Tags: []string{"quarterly", "quarterly"},
		},
		"", RunOptions{Out: &output, OutputMode: appoutput.JSON},
	)
	if err != nil || graphMutations != 1 ||
		!strings.Contains(output.String(), `"quarterly"`) {
		t.Fatalf(
			"add: output=%q mutations=%d err=%v",
			output.String(), graphMutations, err,
		)
	}

	err = RunMetadataWithOptions(
		context.Background(),
		MetadataRequest{
			Operation: MetadataTagRemove, Path: "/report.txt",
			Tags: []string{"approved"}, DryRun: true,
		},
		"", RunOptions{Out: io.Discard},
	)
	if err != nil || graphMutations != 1 {
		t.Fatalf("tag dry-run mutated: %d, %v", graphMutations, err)
	}

	err = RunMetadataWithOptions(
		context.Background(),
		MetadataRequest{
			Operation: MetadataFavoriteSet, Path: "/report.txt",
		},
		"", RunOptions{Out: io.Discard},
	)
	if err != nil || !favorite || propertyMutations != 1 {
		t.Fatalf(
			"favorite: value=%t mutations=%d err=%v",
			favorite, propertyMutations, err,
		)
	}

	err = RunMetadataWithOptions(
		context.Background(),
		MetadataRequest{
			Operation: MetadataPropertySet, Path: "/report.txt",
			Namespace: "https://example.test/metadata",
			Name:      "review-status", Value: "ready",
		},
		"", RunOptions{Out: io.Discard},
	)
	if err != nil || propertyValue != "ready" {
		t.Fatalf("property set: value=%q err=%v", propertyValue, err)
	}

	output.Reset()
	err = RunMetadataWithOptions(
		context.Background(),
		MetadataRequest{
			Operation: MetadataPropertyGet, Path: "/report.txt",
			Namespace: "https://example.test/metadata",
			Name:      "review-status",
		},
		"", RunOptions{Out: &output},
	)
	if err != nil || output.String() != "ready\n" {
		t.Fatalf("property get: %q, %v", output.String(), err)
	}

	err = RunMetadataWithOptions(
		context.Background(),
		MetadataRequest{
			Operation: MetadataPropertyRemove, Path: "/report.txt",
			Namespace: "https://example.test/metadata",
			Name:      "review-status",
		},
		"", RunOptions{Out: io.Discard},
	)
	if err != nil || propertyValue != "" {
		t.Fatalf("property remove: value=%q err=%v", propertyValue, err)
	}
}

func TestMetadataValidatesCustomNamespaceBeforeLoadingProfile(t *testing.T) {
	t.Setenv(
		"OCIS_CONFIG", filepath.Join(t.TempDir(), "missing", "config.json"),
	)
	err := RunMetadataWithOptions(
		context.Background(),
		MetadataRequest{
			Operation: MetadataPropertySet, Path: "/report.txt",
			Namespace: "DAV:", Name: "displayname", Value: "other",
		},
		"", RunOptions{Out: io.Discard},
	)
	if !apperror.IsKind(err, apperror.KindUsage) ||
		!strings.Contains(err.Error(), "reserved") {
		t.Fatalf("error: %v", err)
	}
}

func TestMetadataRejectsUnadvertisedPropertyWrites(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch request.Method {
		case "PROPFIND":
			writeMetadataStat(writer, nil, false)
		case http.MethodOptions:
			writer.Header().Set("Allow", "OPTIONS, PROPFIND")
		default:
			t.Fatalf("method: %s", request.Method)
		}
	}))
	defer server.Close()
	configureMetadataTestProfile(t, server.URL)
	err := RunMetadataWithOptions(
		context.Background(),
		MetadataRequest{
			Operation: MetadataFavoriteSet, Path: "/report.txt",
		},
		"", RunOptions{Out: io.Discard},
	)
	if err == nil || !strings.Contains(err.Error(), "does not advertise PROPPATCH") {
		t.Fatalf("error: %v", err)
	}
}

func configureMetadataTestProfile(t *testing.T, server string) {
	t.Helper()
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := saveStore(
		defaultDependencies(),
		&store{
			Current: "work",
			Profiles: map[string]profile{"work": {
				Server: server, Username: "alice",
				AuthType: "basic", Password: "secret",
			}},
		},
	); err != nil {
		t.Fatal(err)
	}
}

func writeMetadataStat(
	writer http.ResponseWriter, tags []string, favorite bool,
) {
	writer.WriteHeader(http.StatusMultiStatus)
	favoriteValue := "0"
	if favorite {
		favoriteValue = "1"
	}
	_, _ = io.WriteString(writer,
		`<d:multistatus xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns">`+
			`<d:response><d:href>/report.txt</d:href><d:propstat>`+
			`<d:status>HTTP/1.1 200 OK</d:status><d:prop>`+
			`<d:displayname>report.txt</d:displayname>`+
			`<d:getcontentlength>5</d:getcontentlength>`+
			`<oc:fileid>storage$space!report</oc:fileid>`+
			`<oc:tags>`+strings.Join(tags, ",")+`</oc:tags>`+
			`<oc:favorite>`+favoriteValue+`</oc:favorite>`+
			`<oc:checksums><oc:checksum>SHA1:abc</oc:checksum>`+
			`</oc:checksums></d:prop></d:propstat></d:response>`+
			`</d:multistatus>`,
	)
}

func writePropertyResponse(
	writer http.ResponseWriter,
	namespace string,
	name string,
	value string,
	status string,
) {
	writer.WriteHeader(http.StatusMultiStatus)
	_, _ = io.WriteString(writer,
		`<d:multistatus xmlns:d="DAV:" xmlns:x="`+namespace+`">`+
			`<d:response><d:propstat><d:prop><x:`+name+`>`+
			value+`</x:`+name+`></d:prop><d:status>`+status+
			`</d:status></d:propstat></d:response></d:multistatus>`,
	)
}
