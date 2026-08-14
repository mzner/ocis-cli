package app

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mzner/ocis-cli/internal/apperror"
	"github.com/mzner/ocis-cli/internal/auth"
	"github.com/mzner/ocis-cli/internal/credentials"
	appoutput "github.com/mzner/ocis-cli/internal/output"
	"github.com/zalando/go-keyring"
)

func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}

func TestServerOutputUsesInjectedWriterAndStableJSON(t *testing.T) {
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	var output bytes.Buffer
	err := RunServerWithOptions(context.Background(), ServerRequest{
		Operation: "add", Name: "work", Server: "https://cloud.example",
	}, RunOptions{Out: &output, OutputMode: appoutput.JSON})
	if err != nil {
		t.Fatal(err)
	}
	var envelope appoutput.Envelope
	if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.SchemaVersion != appoutput.SchemaVersion || envelope.Type != "server" {
		t.Fatalf("envelope: %#v", envelope)
	}
}

func TestFilesystemDryRunDoesNotContactServer(t *testing.T) {
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := saveStore(defaultDependencies(), &store{Current: "work", Profiles: map[string]profile{
		"work": {
			Server: "http://127.0.0.1:1", Insecure: true, Username: "alice",
			AuthType: "basic", Password: "secret",
		},
	}}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err := RunFilesystemWithOptions(context.Background(), FilesystemRequest{
		Operation: "remove", Source: "/report.txt", DryRun: true,
	}, "", RunOptions{Out: &output})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Would delete /report.txt") {
		t.Fatalf("output: %q", output.String())
	}
}

func TestFilesystemDryRunPlansAllTransferKinds(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		requests++
		if request.Method != "PROPFIND" {
			t.Fatalf("unexpected dry-run request: %s", request.Method)
		}
		writer.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := saveStore(defaultDependencies(), &store{Current: "work", Profiles: map[string]profile{
		"work": {
			Server: server.URL, Insecure: true, Username: "alice",
			AuthType: "basic", Password: "secret",
		},
	}}); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "source.txt")
	if err := os.WriteFile(source, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	tests := []FilesystemRequest{
		{
			Operation: "upload", Source: source, Destination: "/source.txt",
			DryRun: true,
		},
		{
			Operation: "download", Source: "/report.txt", Destination: "report.txt",
			DryRun: true,
		},
		{
			Operation: "cp", Source: "/source.txt", Destination: "/copy.txt",
			DryRun: true,
		},
	}
	for _, request := range tests {
		var output bytes.Buffer
		err := RunFilesystemWithOptions(
			context.Background(), request, "", RunOptions{Out: &output},
		)
		if err != nil {
			t.Fatalf("%s: %v", request.Operation, err)
		}
		if !strings.Contains(output.String(), "Would") {
			t.Fatalf("%s output: %q", request.Operation, output.String())
		}
	}
	if requests != 1 {
		t.Fatalf("dry-run requests: got %d, want destination resolution only", requests)
	}
}

func TestFilesystemMapsDAVStatusToStableKind(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case "PROPFIND":
			writer.WriteHeader(http.StatusNotFound)
		case http.MethodGet:
			_, _ = io.WriteString(writer, `{"value":[]}`)
		default:
			t.Fatalf("unexpected method: %s", request.Method)
		}
	}))
	defer server.Close()
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := saveStore(defaultDependencies(), &store{Current: "work", Profiles: map[string]profile{
		"work": {
			Server: server.URL, Insecure: true, Username: "alice",
			AuthType: "basic", Password: "secret",
		},
	}}); err != nil {
		t.Fatal(err)
	}
	err := RunFilesystemWithOptions(
		context.Background(), FilesystemRequest{Operation: "stat", Source: "/missing"},
		"", RunOptions{Out: io.Discard, Retries: 0},
	)
	if !apperror.IsKind(err, apperror.KindNotFound) || apperror.ExitCode(err) != 4 {
		t.Fatalf("classification: %v", err)
	}
}

func TestFilesystemStatSuggestsMatchingSpace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch {
		case request.Method == "PROPFIND":
			writer.WriteHeader(http.StatusNotFound)
		case request.Method == http.MethodGet &&
			request.URL.Path == "/graph/v1.0/me/drives":
			_, _ = io.WriteString(writer, `{"value":[{
				"id":"space-id","name":"cli","driveType":"project"
			}]}`)
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := saveStore(defaultDependencies(), &store{
		Current: "work",
		Profiles: map[string]profile{"work": {
			Server: server.URL, Insecure: true, Username: "alice",
			AuthType: "basic", Password: "secret",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	err := RunFilesystemWithOptions(
		context.Background(),
		FilesystemRequest{Operation: FilesystemStat, Source: "cli"},
		"", RunOptions{Out: io.Discard},
	)
	if !apperror.IsKind(err, apperror.KindNotFound) ||
		!strings.Contains(err.Error(), "ocis space info cli") ||
		!strings.Contains(err.Error(), "ocis --space cli stat /") {
		t.Fatalf("error: %v", err)
	}
}

func TestFilesystemUseCasesEndToEnd(t *testing.T) {
	var lock sync.Mutex
	methods := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		lock.Lock()
		methods[request.Method]++
		lock.Unlock()
		switch request.Method {
		case "PROPFIND":
			writer.WriteHeader(http.StatusMultiStatus)
			if request.Header.Get("Depth") == "1" {
				_, _ = io.WriteString(writer, appDAVList)
			} else {
				_, _ = io.WriteString(writer, appDAVFile)
			}
		case http.MethodGet:
			writer.Header().Set("Content-Length", "5")
			_, _ = io.WriteString(writer, "hello")
		case http.MethodPut:
			body, err := io.ReadAll(request.Body)
			if err != nil || string(body) != "hello" {
				t.Errorf("upload body: %q, %v", body, err)
			}
			writer.WriteHeader(http.StatusCreated)
		case "MKCOL", "MOVE", "COPY":
			writer.WriteHeader(http.StatusCreated)
		case http.MethodDelete:
			writer.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected method: %s", request.Method)
		}
	}))
	defer server.Close()
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := saveStore(defaultDependencies(), &store{Current: "work", Profiles: map[string]profile{
		"work": {
			Server: server.URL, Insecure: true, Username: "alice",
			AuthType: "basic", Password: "secret",
		},
	}}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	source := filepath.Join(root, "source.txt")
	destination := filepath.Join(root, "download.txt")
	if err := os.WriteFile(source, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	options := RunOptions{Out: io.Discard, Err: io.Discard, Quiet: true}
	requests := []FilesystemRequest{
		{Operation: "list", Source: "/"},
		{Operation: "stat", Source: "/report.txt"},
		{Operation: "mkdir", Source: "/archive"},
		{Operation: "upload", Source: source, Destination: "/source.txt", Verify: true},
		{Operation: "download", Source: "/report.txt", Destination: destination, Verify: true},
		{Operation: "cp", Source: "/report.txt", Destination: "/copy.txt"},
		{Operation: "mv", Source: "/copy.txt", Destination: "/moved.txt"},
		{Operation: "remove", Source: "/moved.txt"},
	}
	for _, request := range requests {
		if err := RunFilesystemWithOptions(
			context.Background(), request, "", options,
		); err != nil {
			t.Fatalf("%s: %v", request.Operation, err)
		}
	}
	for _, method := range []string{
		"PROPFIND", http.MethodGet, http.MethodPut, "MKCOL", "COPY", "MOVE", http.MethodDelete,
	} {
		if methods[method] == 0 {
			t.Errorf("%s was not requested", method)
		}
	}
}

func TestDoctorValidatesProfileAndCapabilities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.URL.Path == "/ocs/v2.php/cloud/capabilities":
			writeAppOCS(writer, `{"capabilities":{
				"core":{"support-sse":true},
				"files":{"archivers":[{
					"enabled":true,"version":"2.0.0","formats":["zip","tar"],
					"archiver_url":"/archiver","max_num_files":"1000","max_size":"1000000"
				}]},
				"files_sharing":{"api_enabled":true,"public":{
					"enabled":true,"password":{"enforced":false},
					"expire_date":{"enabled":true}
				}},
				"spaces":{"enabled":true,"projects":true,"version":"1.0.0"}
			}}`)
		case request.Method == http.MethodOptions:
			writer.Header().Set("DAV", "1, 3")
			writer.Header().Set("Allow", "OPTIONS, PROPFIND, GET")
			writer.WriteHeader(http.StatusOK)
		case request.Method == "PROPFIND":
			writer.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(writer, appDAVFile)
		default:
			t.Fatalf("unexpected method: %s", request.Method)
		}
	}))
	defer server.Close()
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := saveStore(defaultDependencies(), &store{Current: "work", Profiles: map[string]profile{
		"work": {
			Server: server.URL, Insecure: true, Username: "alice",
			AuthType: "basic", Password: "secret",
		},
	}}); err != nil {
		t.Fatal(err)
	}
	var rendered bytes.Buffer
	if err := RunDoctorWithOptions(context.Background(), "", RunOptions{
		Out: &rendered, OutputMode: appoutput.JSON,
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered.String(), `"type": "diagnostic"`) ||
		!strings.Contains(rendered.String(), `"DAV capabilities"`) ||
		!strings.Contains(rendered.String(), `"public links"`) ||
		!strings.Contains(rendered.String(), `"archive downloads"`) ||
		!strings.Contains(rendered.String(), `"real-time events"`) {
		t.Fatalf("output: %s", rendered.String())
	}
}

func TestSpaceSelectionPersistsAndUsesSpaceDAVEndpoint(t *testing.T) {
	var davPath string
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch {
		case request.URL.Path == "/graph/v1.0/me/drives" ||
			request.URL.Path == "/graph/v1.0/drives":
			_, _ = io.WriteString(writer, `{"value":[{
				"id":"space-id","name":"Engineering","driveType":"project",
				"driveAlias":"project/engineering","quota":{"used":5,"total":10}
			}]}`)
		case request.URL.Path ==
			"/graph/v1beta1/drives/space-id/root/permissions":
			_, _ = io.WriteString(writer, `{
				"@libre.graph.permissions.roles.allowedValues":[{
					"id":"manager-id","displayName":"Manager"
				}],
				"@libre.graph.permissions.actions.allowedValues":[
					"libre.graph/driveItem/permissions/read",
					"libre.graph/driveItem/permissions/create",
					"libre.graph/driveItem/permissions/update",
					"libre.graph/driveItem/permissions/delete"
				],
				"value":[{
					"id":"u:alice","roles":["manager-id"],
					"grantedToV2":{"user":{
						"id":"alice-id","displayName":"Alice"
					}}
				}]
			}`)
		case request.URL.Path == "/graph/v1.0/me":
			_, _ = io.WriteString(writer, `{
				"id":"alice-id","displayName":"Alice",
				"onPremisesSamAccountName":"alice"
			}`)
		case request.Method == "PROPFIND":
			davPath = request.URL.Path
			writer.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(writer, appDAVList)
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := saveStore(defaultDependencies(), &store{
		Current: "work",
		Profiles: map[string]profile{"work": {
			Server: server.URL, Insecure: true, Username: "alice",
			AuthType: "basic", Password: "secret",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	var spaces bytes.Buffer
	if err := RunSpaceWithOptions(context.Background(), SpaceRequest{
		Operation: SpaceList,
	}, "", RunOptions{Out: &spaces, OutputMode: appoutput.JSON}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(spaces.String(), `"Engineering"`) {
		t.Fatalf("spaces: %s", spaces.String())
	}
	var humanSpaces bytes.Buffer
	if err := RunSpaceWithOptions(context.Background(), SpaceRequest{
		Operation: SpaceList,
	}, "", RunOptions{Out: &humanSpaces}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(humanSpaces.String(), "Engineering") {
		t.Fatalf("human spaces: %s", humanSpaces.String())
	}
	var spaceDetails bytes.Buffer
	if err := RunSpaceWithOptions(context.Background(), SpaceRequest{
		Operation: SpaceStat, Identifier: "space-id",
	}, "", RunOptions{Out: &spaceDetails}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(spaceDetails.String(), "Quota: 5 / 10 bytes") ||
		!strings.Contains(spaceDetails.String(), "Manage members: yes") {
		t.Fatalf("space details: %s", spaceDetails.String())
	}
	if err := RunSpaceWithOptions(context.Background(), SpaceRequest{
		Operation: SpaceUse, Identifier: "project/engineering",
	}, "", RunOptions{Out: io.Discard}); err != nil {
		t.Fatal(err)
	}
	persisted, err := loadStore(defaultDependencies())
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Profiles["work"].DefaultSpace != "space-id" {
		t.Fatalf("default space: %#v", persisted.Profiles["work"])
	}
	if err := RunFilesystemWithOptions(context.Background(), FilesystemRequest{
		Operation: FilesystemList, Source: "/",
	}, "", RunOptions{Out: io.Discard}); err != nil {
		t.Fatal(err)
	}
	if davPath != "/dav/spaces/space-id/" {
		t.Fatalf("DAV path: %q", davPath)
	}
}

func TestCreateProjectSpace(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.Method != http.MethodPost ||
			request.URL.Path != "/graph/v1.0/drives" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		if err := json.NewDecoder(request.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		writer.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(writer, `{
			"id":"space-id","name":"Engineering","description":"Shared work",
			"driveType":"project","quota":{"total":2000000000}
		}`)
	}))
	defer server.Close()
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := saveStore(defaultDependencies(), &store{
		Current: "work",
		Profiles: map[string]profile{"work": {
			Server: server.URL, Insecure: true, Username: "alice",
			AuthType: "basic", Password: "secret",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	quota := int64(2_000_000_000)
	var rendered bytes.Buffer
	err := RunSpaceCreateWithOptions(context.Background(), SpaceCreateRequest{
		Name: " Engineering ", Description: "Shared work",
		Quota: &quota,
	}, "", RunOptions{Out: &rendered})
	if err != nil {
		t.Fatal(err)
	}
	if requestBody["name"] != "Engineering" ||
		requestBody["driveType"] != "project" {
		t.Fatalf("request: %#v", requestBody)
	}
	requestQuota, ok := requestBody["quota"].(map[string]any)
	if !ok || requestQuota["total"] != float64(quota) {
		t.Fatalf("quota: %#v", requestBody["quota"])
	}
	if !strings.Contains(
		rendered.String(), "Created project space Engineering (space-id)",
	) {
		t.Fatalf("output: %q", rendered.String())
	}
}

func TestCreateProjectSpaceDryRunNeedsNoProfile(t *testing.T) {
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "missing", "config.json"))
	var rendered bytes.Buffer
	err := RunSpaceCreateWithOptions(context.Background(), SpaceCreateRequest{
		Name: "Engineering", DryRun: true,
	}, "", RunOptions{Out: &rendered, OutputMode: appoutput.JSON})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered.String(), `"dryRun": true`) ||
		!strings.Contains(rendered.String(), `"server-default"`) {
		t.Fatalf("output: %s", rendered.String())
	}
}

func TestCreateProjectSpaceMapsForbiddenToAuthenticationError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		writer.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(writer, `{"error":{"message":"access denied"}}`)
	}))
	defer server.Close()
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := saveStore(defaultDependencies(), &store{
		Current: "work",
		Profiles: map[string]profile{"work": {
			Server: server.URL, Insecure: true, Username: "alice",
			AuthType: "basic", Password: "secret",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	err := RunSpaceCreateWithOptions(context.Background(), SpaceCreateRequest{
		Name: "Engineering",
	}, "", RunOptions{Out: io.Discard, Retries: 0})
	if !apperror.IsKind(err, apperror.KindAuthentication) ||
		apperror.ExitCode(err) != 3 {
		t.Fatalf("classification: %v", err)
	}
}

func TestCreateProjectSpaceValidatesInput(t *testing.T) {
	negative := int64(-1)
	for _, request := range []SpaceCreateRequest{
		{Name: " "},
		{Name: "Engineering", Quota: &negative},
	} {
		err := RunSpaceCreateWithOptions(
			context.Background(), request, "", RunOptions{Out: io.Discard},
		)
		if !apperror.IsKind(err, apperror.KindUsage) ||
			apperror.ExitCode(err) != 2 {
			t.Fatalf("classification: %v", err)
		}
	}
}

func TestPublicLinkUseCases(t *testing.T) {
	var createdForm url.Values
	var revoked bool
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch {
		case request.URL.Path == "/graph/v1.0/me/drives":
			_, _ = io.WriteString(writer, `{"value":[{
				"id":"space-id","name":"Engineering","driveType":"project"
			}]}`)
		case request.URL.Path == "/ocs/v2.php/cloud/capabilities":
			writeAppOCS(writer, `{"capabilities":{
				"files_sharing":{"api_enabled":true,"public":{"enabled":true}},
				"spaces":{"enabled":true}
			}}`)
		case request.Method == http.MethodPost:
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatal(err)
			}
			createdForm, err = url.ParseQuery(string(body))
			if err != nil {
				t.Fatal(err)
			}
			writeAppOCS(writer, `{
				"id":"42","share_type":3,"url":"https://cloud.test/s/token",
				"path":"/demo","permissions":1
			}`)
		case request.Method == http.MethodGet:
			writeAppOCS(writer, `[{
				"id":"42","share_type":3,"url":"https://cloud.test/s/token",
				"path":"/demo","permissions":1
			}]`)
		case request.Method == http.MethodDelete:
			revoked = true
			writeAppOCS(writer, `null`)
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := saveStore(defaultDependencies(), &store{
		Current: "work",
		Profiles: map[string]profile{"work": {
			Server: server.URL, Insecure: true, Username: "alice",
			AuthType: "basic", Password: "secret",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	options := RunOptions{Out: io.Discard, Space: "Engineering"}
	if err := RunShareWithOptions(context.Background(), ShareRequest{
		Operation: ShareCreate, Path: "/demo", Permissions: 1,
	}, "", options); err != nil {
		t.Fatal(err)
	}
	if createdForm.Get("space_ref") != "space-id/demo" {
		t.Fatalf("create form: %#v", createdForm)
	}
	if err := RunShareWithOptions(context.Background(), ShareRequest{
		Operation: ShareList, Path: "/demo",
	}, "", options); err != nil {
		t.Fatal(err)
	}
	var machineLinks bytes.Buffer
	machineOptions := options
	machineOptions.Out = &machineLinks
	machineOptions.OutputMode = appoutput.JSON
	if err := RunShareWithOptions(context.Background(), ShareRequest{
		Operation: ShareList, Path: "/demo",
	}, "", machineOptions); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(machineLinks.String(), `"type": "share"`) {
		t.Fatalf("machine links: %s", machineLinks.String())
	}
	if err := RunShareWithOptions(context.Background(), ShareRequest{
		Operation: ShareCreate, Path: "/demo", DryRun: true,
	}, "", options); err != nil {
		t.Fatal(err)
	}
	if err := RunShareWithOptions(context.Background(), ShareRequest{
		Operation: ShareRevoke, ID: "42", DryRun: true,
	}, "", options); err != nil {
		t.Fatal(err)
	}
	if err := RunShareWithOptions(context.Background(), ShareRequest{
		Operation: ShareRevoke, ID: "42",
	}, "", options); err != nil {
		t.Fatal(err)
	}
	if !revoked {
		t.Fatal("public link was not revoked")
	}
	err := RunShareWithOptions(context.Background(), ShareRequest{
		Operation: ShareCreate, Path: "/demo", Expiration: "tomorrow",
	}, "", options)
	if !apperror.IsKind(err, apperror.KindUsage) {
		t.Fatalf("invalid expiration: %v", err)
	}
	for permissions, want := range map[int]string{
		1: "read", 3: "edit", 4: "upload", 5: "upload", 15: "edit", 7: "7",
	} {
		if got := permissionName(permissions); got != want {
			t.Errorf("permissionName(%d): got %q, want %q", permissions, got, want)
		}
	}
}

func writeAppOCS(writer http.ResponseWriter, data string) {
	writer.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(writer, `{"ocs":{"meta":{
		"status":"ok","statuscode":200,"message":"OK"
	},"data":`+data+`}}`)
}

func TestStandardStreamTransfers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodPut:
			body, err := io.ReadAll(request.Body)
			if err != nil || string(body) != "stdin" {
				t.Errorf("upload body: %q, %v", body, err)
			}
			writer.WriteHeader(http.StatusCreated)
		case http.MethodGet:
			writer.Header().Set("Content-Length", "5")
			_, _ = io.WriteString(writer, "hello")
		case "PROPFIND":
			writer.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(writer, appDAVFile)
		default:
			t.Errorf("unexpected method: %s", request.Method)
		}
	}))
	defer server.Close()
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := saveStore(defaultDependencies(), &store{Current: "work", Profiles: map[string]profile{
		"work": {
			Server: server.URL, Insecure: true, Username: "alice",
			AuthType: "basic", Password: "secret",
		},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := RunFilesystemWithOptions(context.Background(), FilesystemRequest{
		Operation: "upload", Source: "-", Destination: "/stdin.txt", Verify: true,
	}, "", RunOptions{
		In: strings.NewReader("stdin"), Out: io.Discard, Err: io.Discard, Quiet: true,
	}); err != nil {
		t.Fatal(err)
	}
	var download bytes.Buffer
	if err := RunFilesystemWithOptions(context.Background(), FilesystemRequest{
		Operation: "download", Source: "/report.txt", Destination: "-", Verify: true,
	}, "", RunOptions{
		Out: &download, Err: io.Discard, Quiet: true,
	}); err != nil {
		t.Fatal(err)
	}
	if download.String() != "hello" {
		t.Fatalf("stdout: %q", download.String())
	}
}

func TestRecursiveFilesystemTransfersUseProtocolPort(t *testing.T) {
	var uploaded bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case "PROPFIND":
			writer.WriteHeader(http.StatusMultiStatus)
			if request.Header.Get("Depth") == "1" {
				_, _ = io.WriteString(writer, appDAVDirectoryList)
			} else if strings.HasSuffix(request.URL.Path, "/demo") {
				_, _ = io.WriteString(writer, appDAVDirectory)
			} else {
				_, _ = io.WriteString(writer, appDAVFile)
			}
		case http.MethodGet:
			writer.Header().Set("Content-Length", "5")
			_, _ = io.WriteString(writer, "hello")
		case "MKCOL":
			writer.WriteHeader(http.StatusCreated)
		case http.MethodPut:
			uploaded = true
			writer.WriteHeader(http.StatusCreated)
		default:
			t.Errorf("unexpected method: %s", request.Method)
		}
	}))
	defer server.Close()
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := saveStore(defaultDependencies(), &store{
		Current: "work",
		Profiles: map[string]profile{"work": {
			Server: server.URL, Insecure: true, Username: "alice",
			AuthType: "basic", Password: "secret",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	options := RunOptions{Out: io.Discard, Err: io.Discard, Quiet: true}
	localSource := filepath.Join(t.TempDir(), "source")
	if err := os.Mkdir(localSource, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localSource, "upload.txt"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RunFilesystemWithOptions(context.Background(), FilesystemRequest{
		Operation: FilesystemUpload, Source: localSource,
		Destination: "/uploaded", Recursive: true,
	}, "", options); err != nil {
		t.Fatal(err)
	}
	if !uploaded {
		t.Fatal("recursive upload did not send PUT")
	}

	parent := t.TempDir()
	if err := RunFilesystemWithOptions(context.Background(), FilesystemRequest{
		Operation: FilesystemDownload, Source: "/demo",
		Destination: parent, Recursive: true,
	}, "", options); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(parent, "demo", "report.txt"))
	if err != nil || string(data) != "hello" {
		t.Fatalf("recursive download: %q, %v", data, err)
	}
}

func TestFilesystemTransferGuards(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != "PROPFIND" {
			t.Fatalf("unexpected method: %s", request.Method)
		}
		writer.WriteHeader(http.StatusMultiStatus)
		_, _ = io.WriteString(writer, appDAVDirectory)
	}))
	defer server.Close()
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := saveStore(defaultDependencies(), &store{
		Current: "work",
		Profiles: map[string]profile{"work": {
			Server: server.URL, Insecure: true, Username: "alice",
			AuthType: "basic", Password: "secret",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	localDirectory := t.TempDir()
	tests := []FilesystemRequest{
		{
			Operation: FilesystemUpload, Source: "-", Destination: "/stdin",
			Recursive: true,
		},
		{
			Operation: FilesystemUpload, Source: localDirectory,
			Destination: "/directory",
		},
		{
			Operation: FilesystemDownload, Source: "/demo",
			Destination: filepath.Join(t.TempDir(), "demo"),
		},
		{
			Operation: FilesystemDownload, Source: "/demo",
			Destination: "-", Recursive: true,
		},
		{Operation: FilesystemOperation("unsupported")},
	}
	for _, request := range tests {
		err := RunFilesystemWithOptions(
			context.Background(), request, "", RunOptions{
				In: strings.NewReader("data"), Out: io.Discard,
				Err: io.Discard, Quiet: true,
			},
		)
		if !apperror.IsKind(err, apperror.KindUsage) {
			t.Errorf("%s: expected usage error, got %v", request.Operation, err)
		}
	}
}

const appDAVFile = `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:">
<d:response><d:href>/remote.php/dav/files/alice/report.txt</d:href><d:propstat>
<d:status>HTTP/1.1 200 OK</d:status><d:prop><d:displayname>report.txt</d:displayname>
<d:getcontentlength>5</d:getcontentlength><d:resourcetype/></d:prop>
</d:propstat></d:response></d:multistatus>`

const appDAVList = `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:">
<d:response><d:href>/remote.php/dav/files/alice/</d:href><d:propstat>
<d:status>HTTP/1.1 200 OK</d:status><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop>
</d:propstat></d:response>
<d:response><d:href>/remote.php/dav/files/alice/report.txt</d:href><d:propstat>
<d:status>HTTP/1.1 200 OK</d:status><d:prop><d:displayname>report.txt</d:displayname>
<d:getcontentlength>5</d:getcontentlength><d:resourcetype/></d:prop>
</d:propstat></d:response></d:multistatus>`

const appDAVDirectory = `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:">
<d:response><d:href>/remote.php/dav/files/alice/demo/</d:href><d:propstat>
<d:status>HTTP/1.1 200 OK</d:status><d:prop><d:displayname>demo</d:displayname>
<d:resourcetype><d:collection/></d:resourcetype></d:prop>
</d:propstat></d:response></d:multistatus>`

const appDAVDirectoryList = `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:">
<d:response><d:href>/remote.php/dav/files/alice/demo/</d:href><d:propstat>
<d:status>HTTP/1.1 200 OK</d:status><d:prop><d:displayname>demo</d:displayname>
<d:resourcetype><d:collection/></d:resourcetype></d:prop>
</d:propstat></d:response>
<d:response><d:href>/remote.php/dav/files/alice/demo/report.txt</d:href><d:propstat>
<d:status>HTTP/1.1 200 OK</d:status><d:prop><d:displayname>report.txt</d:displayname>
<d:getcontentlength>5</d:getcontentlength><d:resourcetype/></d:prop>
</d:propstat></d:response></d:multistatus>`

func TestDiscover(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(auth.Metadata{
			Issuer: server.URL, AuthorizationEndpoint: server.URL + "/authorize",
			TokenEndpoint:    server.URL + "/token",
			UserInfoEndpoint: server.URL + "/userinfo",
		})
	}))
	defer server.Close()
	p := profile{Server: server.URL, Insecure: true}
	meta, err := auth.Discover(context.Background(), httpClientFor(p, time.Minute), p.Server)
	if err != nil {
		t.Fatal(err)
	}
	if meta.AuthorizationEndpoint != server.URL+"/authorize" {
		t.Fatalf("unexpected metadata: %#v", meta)
	}
}

func TestRefreshAndBearerWebDAV(t *testing.T) {
	var accessToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "refresh" {
				t.Fatalf("unexpected refresh form: %#v", r.Form)
			}
			_ = json.NewEncoder(w).Encode(auth.Token{AccessToken: "fresh", RefreshToken: "new-refresh", ExpiresIn: 3600})
		case "/remote.php/dav/files/einstein/":
			accessToken = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"><d:response><d:href>/remote.php/dav/files/einstein/</d:href><d:propstat><d:status>HTTP/1.1 200 OK</d:status><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop></d:propstat></d:response></d:multistatus>`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	configFile := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("OCIS_CONFIG", configFile)
	s := &store{Current: "local", Profiles: map[string]profile{"local": {
		Server: server.URL, Insecure: true, Username: "einstein", ClientID: "cli", TokenURL: server.URL + "/token",
		AccessToken: "expired", RefreshToken: "refresh", ExpiresAt: time.Now().Add(-time.Hour).Unix(),
	}}}
	if err := saveStore(defaultDependencies(), s); err != nil {
		t.Fatal(err)
	}
	c, err := newClientWithOptions(context.Background(), "", RunOptions{}.normalized())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.list("/"); err != nil {
		t.Fatal(err)
	}
	if accessToken != "Bearer fresh" {
		t.Fatalf("got authorization %q", accessToken)
	}
}

func TestServerAddAndUse(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("OCIS_CONFIG", configFile)
	if err := RunServerWithOptions(context.Background(), ServerRequest{Operation: "add", Name: "work", Server: "https://cloud.example", Insecure: true}, RunOptions{Out: io.Discard}); err != nil {
		t.Fatal(err)
	}
	if err := RunServerWithOptions(context.Background(), ServerRequest{Operation: "add", Name: "home", Server: "https://home.example"}, RunOptions{Out: io.Discard}); err != nil {
		t.Fatal(err)
	}
	if err := RunServerWithOptions(context.Background(), ServerRequest{Operation: "use", Name: "home"}, RunOptions{Out: io.Discard}); err != nil {
		t.Fatal(err)
	}
	s, err := loadStore(defaultDependencies())
	if err != nil {
		t.Fatal(err)
	}
	if s.Current != "home" || !s.Profiles["work"].Insecure ||
		s.Profiles["home"].ClientID != defaultClientID {
		t.Fatalf("unexpected store: %#v", s)
	}
	info, err := os.Stat(configFile)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("config mode is %o", info.Mode().Perm())
	}
}

// TestCleartextServerRequiresInsecureOptIn covers both entry points that accept
// a server URL. A cleartext URL would send the Basic password or bearer token
// in the clear on every later request, so it is refused as a usage error and no
// profile is stored; --insecure keeps the local-development case available.
func TestCleartextServerRequiresInsecureOptIn(t *testing.T) {
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	options := RunOptions{Out: io.Discard, Err: io.Discard}

	err := RunServerWithOptions(context.Background(), ServerRequest{
		Operation: "add", Name: "work", Server: "http://cloud.example",
	}, options)
	if err == nil {
		t.Fatal("adding a cleartext server succeeded")
	}
	if !apperror.IsKind(err, apperror.KindUsage) {
		t.Fatalf("got %v, want a usage error", err)
	}
	s, err := loadStore(defaultDependencies())
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := s.Profiles["work"]; exists {
		t.Fatal("a rejected server was still saved")
	}

	if err := RunAuthWithOptions(context.Background(), AuthRequest{
		Operation: "login", Server: "http://cloud.example", Name: "work",
		Mode: "basic", Username: "alice",
	}, "", options); err == nil {
		t.Fatal("logging in to a cleartext server succeeded")
	}

	if err := RunServerWithOptions(context.Background(), ServerRequest{
		Operation: "add", Name: "local", Server: "http://localhost:9200",
		Insecure: true,
	}, options); err != nil {
		t.Fatalf("cleartext server with --insecure: %v", err)
	}
}

// TestHTTPClientRefusesToFollowADowngradeRedirect covers the gap that
// validating only the base URL leaves open. Go decides whether to forward
// Authorization across a redirect by comparing hosts alone, so an https endpoint
// redirecting to http:// on the same host would carry the credential over
// cleartext. Both the Basic and bearer cases run through this one client, so the
// policy is tested below the authentication-specific clients.
func TestHTTPClientRefusesToFollowADowngradeRedirect(t *testing.T) {
	var cleartextRequests atomic.Int64
	cleartext := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		cleartextRequests.Add(1)
		if request.Header.Get("Authorization") != "" {
			t.Error("Authorization was forwarded over cleartext")
		}
		writer.WriteHeader(http.StatusOK)
	}))
	defer cleartext.Close()
	var reachedFinal atomic.Int64
	secure := httptest.NewTLSServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch request.URL.Path {
		case "/final":
			reachedFinal.Add(1)
			writer.WriteHeader(http.StatusOK)
		case "/same-scheme":
			http.Redirect(writer, request, "/final", http.StatusFound)
		default:
			http.Redirect(writer, request, cleartext.URL+"/downgrade", http.StatusFound)
		}
	}))
	defer secure.Close()

	// A normal profile, with only the test server's certificate trusted so the
	// insecure opt-in stays off and the downgrade must be refused.
	client := httpClientFor(profile{}, time.Minute)
	trustTestServer(t, client, secure)
	request, err := http.NewRequestWithContext(
		context.Background(), http.MethodGet, secure.URL+"/start", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer access-token")
	response, err := client.Do(request) //nolint:bodyclose // no response body on the refused redirect
	if err == nil {
		_ = response.Body.Close()
		t.Fatal("the downgrade redirect was followed")
	}
	if !strings.Contains(err.Error(), "http") ||
		strings.Contains(err.Error(), "access-token") {
		t.Fatalf("error must name the downgrade and no credential: %v", err)
	}
	if got := cleartextRequests.Load(); got != 0 {
		t.Fatalf("cleartext requests: got %d, want none", got)
	}

	// An ordinary https-to-https redirect keeps working.
	secureRedirect, err := http.NewRequestWithContext(
		context.Background(), http.MethodGet, secure.URL+"/same-scheme", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	response, err = client.Do(secureRedirect)
	if err != nil {
		t.Fatalf("https redirect: %v", err)
	}
	_ = response.Body.Close()
	if got := reachedFinal.Load(); got != 1 {
		t.Fatalf("followed https redirects: got %d, want 1", got)
	}
}

// TestInsecureProfileMayFollowADowngradeRedirect asserts the explicit
// development opt-in still permits the downgrade it opted into.
func TestInsecureProfileMayFollowADowngradeRedirect(t *testing.T) {
	cleartext := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		writer.WriteHeader(http.StatusOK)
	}))
	defer cleartext.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		http.Redirect(writer, request, cleartext.URL+"/downgrade", http.StatusFound)
	}))
	defer secure.Close()
	client := httpClientFor(profile{Insecure: true}, time.Minute)
	request, err := http.NewRequestWithContext(
		context.Background(), http.MethodGet, secure.URL+"/start", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("insecure profile: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d", response.StatusCode)
	}
}

// trustTestServer adds a test TLS server's certificate to a client built for a
// normal profile, so a redirect policy can be tested without the insecure
// opt-in that would also relax it.
func trustTestServer(t *testing.T, client *http.Client, server *httptest.Server) {
	t.Helper()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport: got %T, want *http.Transport", client.Transport)
	}
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
}

// TestLegacyCleartextProfileSendsNoCredentials covers a profile written by a
// release that accepted cleartext without an opt-in. Rejecting only new entries
// leaves such a profile able to send its saved password over the network, so the
// stored URL is revalidated when the profile is selected. The listener records
// any connection at all: the credential must never leave the process.
func TestLegacyCleartextProfileSendsNoCredentials(t *testing.T) {
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		requests.Add(1)
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	legacy := map[string]profile{
		"basic": {
			Server: server.URL, Username: "alice",
			AuthType: "basic", Password: "secret",
		},
		"oidc": {
			Server: server.URL, AuthType: "oidc", AccessToken: "access",
			RefreshToken: "refresh", TokenURL: server.URL + "/token",
			// Already expired, so a refresh would be attempted.
			ExpiresAt: time.Now().Add(-time.Hour).Unix(),
		},
	}
	if err := saveStore(defaultDependencies(), &store{
		Current: "basic", Profiles: legacy,
	}); err != nil {
		t.Fatal(err)
	}
	options := RunOptions{Out: io.Discard, Err: io.Discard}
	for name := range legacy {
		err := RunFilesystemWithOptions(context.Background(), FilesystemRequest{
			Operation: "list", Source: "/",
		}, name, options)
		if err == nil {
			t.Fatalf("%s: a cleartext profile was used for a remote command", name)
		}
		if !apperror.IsKind(err, apperror.KindUsage) {
			t.Fatalf("%s: got %v, want a usage error", name, err)
		}
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("requests: got %d, want none", got)
	}

	// The same profile must stay inspectable and repairable, or a user cannot
	// recover from the rejection.
	if err := RunServerWithOptions(
		context.Background(), ServerRequest{Operation: "list"}, options,
	); err != nil {
		t.Fatalf("server list: %v", err)
	}
	if err := RunServerWithOptions(context.Background(), ServerRequest{
		Operation: "remove", Name: "basic",
	}, options); err != nil {
		t.Fatalf("server remove: %v", err)
	}
}

// TestLegacyCleartextProfileLoginIsRejectedBeforeThePasswordPrompt asserts that
// a Basic login against a stored cleartext profile stops before any credential
// is read or sent, because passing no new --server previously skipped
// validation entirely.
func TestLegacyCleartextProfileLoginIsRejectedBeforeThePasswordPrompt(t *testing.T) {
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("OCIS_PASSWORD", "secret")
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		requests.Add(1)
		writer.WriteHeader(http.StatusMultiStatus)
		_, _ = io.WriteString(writer, appDAVList)
	}))
	defer server.Close()
	if err := saveStore(defaultDependencies(), &store{
		Current: "legacy",
		Profiles: map[string]profile{
			"legacy": {Server: server.URL, Username: "alice", AuthType: "basic"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	options := RunOptions{Out: io.Discard, Err: io.Discard}
	err := RunAuthWithOptions(context.Background(), AuthRequest{
		Operation: "login", Profile: "legacy", Mode: "basic", Username: "alice",
	}, "", options)
	if err == nil {
		t.Fatal("Basic login against a cleartext profile succeeded")
	}
	if !apperror.IsKind(err, apperror.KindUsage) {
		t.Fatalf("got %v, want a usage error", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("requests: got %d, want none", got)
	}
	s, err := loadStore(defaultDependencies())
	if err != nil {
		t.Fatal(err)
	}
	// The opt-in stays explicit: a rejection must not migrate the profile.
	if s.Profiles["legacy"].Insecure {
		t.Fatal("a rejected profile was silently migrated to insecure")
	}

	// The same login succeeds once the user accepts the risk explicitly.
	if err := RunAuthWithOptions(context.Background(), AuthRequest{
		Operation: "login", Profile: "legacy", Mode: "basic",
		Username: "alice", Insecure: true,
	}, "", options); err != nil {
		t.Fatalf("explicit --insecure login: %v", err)
	}
}

func TestServerListRemoveAndErrors(t *testing.T) {
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	var output bytes.Buffer
	options := RunOptions{Out: &output}
	if err := RunServerWithOptions(context.Background(), ServerRequest{
		Operation: "add", Name: "work", Server: "https://cloud.example",
	}, options); err != nil {
		t.Fatal(err)
	}
	if err := RunServerWithOptions(
		context.Background(), ServerRequest{Operation: "list"}, options,
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "work") {
		t.Fatalf("list output: %q", output.String())
	}
	if err := RunServerWithOptions(context.Background(), ServerRequest{
		Operation: "remove", Name: "work",
	}, options); err != nil {
		t.Fatal(err)
	}
	if err := RunServerWithOptions(context.Background(), ServerRequest{
		Operation: "use", Name: "missing",
	}, options); err == nil {
		t.Fatal("using an unknown profile succeeded")
	}
	if err := RunServerWithOptions(context.Background(), ServerRequest{
		Operation: "unknown",
	}, options); err == nil {
		t.Fatal("unknown operation succeeded")
	}
}

func TestBasicLoginStatusAndLogout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		user, password, ok := request.BasicAuth()
		if !ok || user != "alice" || password != "secret" {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		writer.WriteHeader(http.StatusMultiStatus)
		_, _ = io.WriteString(writer, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"><d:response><d:href>/remote.php/dav/files/alice/</d:href><d:propstat><d:status>HTTP/1.1 200 OK</d:status><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop></d:propstat></d:response></d:multistatus>`)
	}))
	defer server.Close()
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("OCIS_PASSWORD", "secret")
	var output bytes.Buffer
	options := RunOptions{Out: &output, Err: io.Discard, Timeout: time.Second}
	if err := RunAuthWithOptions(context.Background(), AuthRequest{
		Operation: "login", Server: server.URL, Name: "work",
		Mode: "basic", Username: "alice", Insecure: true,
	}, "", options); err != nil {
		t.Fatal(err)
	}
	if err := RunAuthWithOptions(
		context.Background(), AuthRequest{Operation: "status"}, "", options,
	); err != nil {
		t.Fatal(err)
	}
	if err := RunAuthWithOptions(
		context.Background(), AuthRequest{Operation: "logout"}, "", options,
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Authenticated") ||
		!strings.Contains(output.String(), "Logged out") {
		t.Fatalf("output: %q", output.String())
	}
}

func TestBasicLoginProbeUsesCallerContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		writer.WriteHeader(http.StatusMultiStatus)
	}))
	defer server.Close()
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("OCIS_PASSWORD", "secret")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := RunAuthWithOptions(ctx, AuthRequest{
		Operation: "login", Server: server.URL, Name: "work",
		Mode: "basic", Username: "alice", Insecure: true,
	}, "", RunOptions{
		Out: io.Discard, Err: io.Discard, Timeout: time.Second,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context cancellation", err)
	}
}

func TestLoginClearsDefaultSpaceOnlyWhenUserChanges(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		_, password, ok := request.BasicAuth()
		if !ok || password != "secret" {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		writer.WriteHeader(http.StatusMultiStatus)
		_, _ = io.WriteString(
			writer,
			`<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"/>`,
		)
	}))
	defer server.Close()
	t.Setenv("OCIS_PASSWORD", "secret")

	tests := []struct {
		name            string
		username        string
		expectedSpaceID string
	}{
		{name: "same user", username: "alice", expectedSpaceID: "space-id"},
		{name: "different user", username: "bob", expectedSpaceID: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selected := profile{
				Server: server.URL, Insecure: true, Username: "alice", AuthType: "basic",
				DefaultSpace: "space-id",
			}
			selected.DefaultSpaceOwner = profileIdentity(selected)
			configRepository := &memoryConfig{store: &store{
				Version: 1, Current: "work",
				Profiles: map[string]profile{"work": selected},
			}}
			options := RunOptions{
				Out: io.Discard, Err: io.Discard, Timeout: time.Second,
				Dependencies: Dependencies{
					Config: configRepository,
					Credentials: &memoryCredentials{
						secrets: map[string]credentials.Secret{},
					},
				},
			}
			err := RunAuthWithOptions(context.Background(), AuthRequest{
				Operation: AuthLogin, Profile: "work", Mode: "basic",
				Username: test.username,
			}, "", options)
			if err != nil {
				t.Fatal(err)
			}
			got := configRepository.store.Profiles["work"].DefaultSpace
			if got != test.expectedSpaceID {
				t.Fatalf("default Space = %q, want %q", got, test.expectedSpaceID)
			}
		})
	}
}

func TestLogoutClearsDefaultSpace(t *testing.T) {
	configRepository := &memoryConfig{store: &store{
		Version: 1, Current: "work",
		Profiles: map[string]profile{"work": {
			Server: "https://cloud.example", Username: "alice", AuthType: "oidc",
			DefaultSpace: "space-id",
		}},
	}}
	err := RunAuthWithOptions(context.Background(), AuthRequest{
		Operation: AuthLogout, Profile: "work",
	}, "", RunOptions{
		Out: io.Discard,
		Dependencies: Dependencies{
			Config: configRepository,
			Credentials: &memoryCredentials{
				secrets: map[string]credentials.Secret{},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := configRepository.store.Profiles["work"].DefaultSpace; got != "" {
		t.Fatalf("default Space = %q, want empty", got)
	}
}

func TestAuthValidationErrors(t *testing.T) {
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	if err := saveStore(defaultDependencies(), &store{Current: "work", Profiles: map[string]profile{
		"work": {Server: "https://cloud.example", ClientID: "client"},
	}}); err != nil {
		t.Fatal(err)
	}
	options := RunOptions{Out: io.Discard, Err: io.Discard}
	tests := []AuthRequest{
		{Operation: "login", Profile: "work", Mode: "basic"},
		{Operation: "login", Profile: "work", Mode: "unsupported"},
		{Operation: "unknown"},
	}
	for _, request := range tests {
		if err := RunAuthWithOptions(context.Background(), request, "", options); err == nil {
			t.Errorf("%s succeeded", request.Operation)
		}
	}
}

func TestOIDCLoginFlow(t *testing.T) {
	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(writer).Encode(auth.Metadata{
				Issuer: provider.URL, AuthorizationEndpoint: provider.URL + "/authorize",
				TokenEndpoint: provider.URL + "/token", UserInfoEndpoint: provider.URL + "/userinfo",
			})
		case "/token":
			_ = json.NewEncoder(writer).Encode(auth.Token{
				AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 0,
			})
		case "/userinfo":
			_ = json.NewEncoder(writer).Encode(auth.UserInfo{
				PreferredUsername: "alice", Subject: "alice-subject",
			})
		default:
			t.Fatalf("unexpected provider path: %s", request.URL.Path)
		}
	}))
	defer provider.Close()
	writer := &authorizationWriter{}
	p := profile{Server: provider.URL, ClientID: "client", Insecure: true}
	err := oidcLogin(
		context.Background(), &p, true, "urn:ocis:mfa", RunOptions{
			Out: writer, Err: io.Discard, Timeout: time.Second,
		}.normalized(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if p.Username != "alice" || p.Subject != "alice-subject" ||
		p.AccessToken != "access" || p.RefreshToken != "refresh" {
		t.Fatalf("profile: %#v", p)
	}
	if !strings.Contains(writer.String(), "acr_values=urn%3Aocis%3Amfa") {
		t.Fatalf("authorization URL did not request MFA ACR: %q", writer.String())
	}
	if remaining := time.Until(time.Unix(p.ExpiresAt, 0)); remaining < 59*time.Minute ||
		remaining > 61*time.Minute {
		t.Fatalf("fallback expiry is %s away", remaining)
	}
}

type authorizationWriter struct {
	bytes.Buffer
	once sync.Once
}

func (writer *authorizationWriter) Write(data []byte) (int, error) {
	count, err := writer.Buffer.Write(data)
	target := strings.TrimSpace(string(data))
	if parsed, parseErr := url.Parse(target); parseErr == nil &&
		parsed.Scheme != "" && strings.HasSuffix(parsed.Path, "/authorize") {
		writer.once.Do(func() {
			go func() {
				query := parsed.Query()
				callback, callbackErr := url.Parse(query.Get("redirect_uri"))
				if callbackErr != nil {
					return
				}
				values := callback.Query()
				values.Set("state", query.Get("state"))
				values.Set("code", "authorization-code")
				callback.RawQuery = values.Encode()
				response, requestErr := http.Get(callback.String()) //nolint:gosec // loopback URL generated by the test flow
				if requestErr == nil {
					_ = response.Body.Close()
				}
			}()
		})
	}
	return count, err
}

func TestBasicBearerSelection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "fixture-user" || password != "fixture-password" {
			t.Fatalf("unexpected authentication: %q %q %t", user, password, ok)
		}
		w.WriteHeader(http.StatusMultiStatus)
		_, _ = io.WriteString(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"><d:response><d:href>/remote.php/dav/files/fixture-user/</d:href><d:propstat><d:status>HTTP/1.1 200 OK</d:status><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop></d:propstat></d:response></d:multistatus>`)
	}))
	defer server.Close()
	c := client{profile: profile{Server: server.URL, Insecure: true, Username: "fixture-user", AuthType: "basic", Password: "fixture-password"}, http: server.Client()}
	if _, err := c.list("/"); err != nil {
		t.Fatal(err)
	}
}

func TestUploadSendsContentLength(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength != 5 {
			t.Fatalf("content length: got %d, want 5", r.ContentLength)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != "alpha" {
			t.Fatalf("body: got %q", body)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	local := filepath.Join(t.TempDir(), "alpha.txt")
	if err := os.WriteFile(local, []byte("alpha"), 0600); err != nil {
		t.Fatal(err)
	}
	c := client{profile: profile{Server: server.URL, Insecure: true, Username: "alice", AuthType: "basic", Password: "secret"}, http: server.Client()}
	if err := c.davClient().Upload(context.Background(), local, "/alpha.txt"); err != nil {
		t.Fatal(err)
	}
}

func TestTopLevelFileAlias(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("OCIS_CONFIG", configFile)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMultiStatus)
		_, _ = io.WriteString(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"><d:response><d:href>/remote.php/dav/files/alice/</d:href><d:propstat><d:status>HTTP/1.1 200 OK</d:status><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop></d:propstat></d:response></d:multistatus>`)
	}))
	defer server.Close()
	if err := saveStore(defaultDependencies(), &store{Current: "work", Profiles: map[string]profile{"work": {
		Server: server.URL, Insecure: true, Username: "alice", AuthType: "basic", Password: "secret",
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := RunFilesystemWithOptions(context.Background(), FilesystemRequest{Operation: "list", Source: "/"}, "", RunOptions{Out: io.Discard}); err != nil {
		t.Fatal(err)
	}
}

func TestMoveDoesNotOverwriteByDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "MOVE" {
			t.Fatalf("method: got %s", r.Method)
		}
		if got, want := r.Header.Get("Overwrite"), "F"; got != want {
			t.Fatalf("overwrite: got %q, want %q", got, want)
		}
		if got, want := r.Header.Get("Destination"), serverURL(r)+"/remote.php/dav/files/alice/archive/report.txt"; got != want {
			t.Fatalf("destination: got %q, want %q", got, want)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	c := client{profile: profile{Server: server.URL, Insecure: true, Username: "alice", AuthType: "basic", Password: "secret"}, http: server.Client()}
	if err := c.move("/report.txt", "/archive/report.txt", false); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveDirectoryRequiresRecursive(t *testing.T) {
	deleteCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PROPFIND":
			w.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"><d:response><d:href>/remote.php/dav/files/alice/docs/</d:href><d:propstat><d:status>HTTP/1.1 200 OK</d:status><d:prop><d:displayname>docs</d:displayname><d:resourcetype><d:collection/></d:resourcetype></d:prop></d:propstat></d:response></d:multistatus>`)
		case http.MethodDelete:
			deleteCalled = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected method: %s", r.Method)
		}
	}))
	defer server.Close()
	c := client{profile: profile{Server: server.URL, Insecure: true, Username: "alice", AuthType: "basic", Password: "secret"}, http: server.Client()}
	if err := c.remove("/docs", false); !errors.Is(err, errRemoteIsDirectory) {
		t.Fatalf("expected directory safety error, got %v", err)
	}
	if deleteCalled {
		t.Fatal("DELETE was sent without recursive opt-in")
	}
	if err := c.remove("/docs", true); err != nil {
		t.Fatal(err)
	}
	if !deleteCalled {
		t.Fatal("recursive removal did not send DELETE")
	}
}

func serverURL(r *http.Request) string {
	return "http://" + r.Host
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "", "value"); got != "value" {
		t.Fatalf("got %q", got)
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestProfileSelectionErrorsAndAccessTokenOverride(t *testing.T) {
	empty := &store{Profiles: map[string]profile{}}
	if _, _, err := selectProfile(empty, ""); !apperror.IsKind(err, apperror.KindUsage) {
		t.Fatalf("missing current profile: %v", err)
	}
	if _, _, err := selectProfile(empty, "missing"); !apperror.IsKind(err, apperror.KindUsage) {
		t.Fatalf("unknown profile: %v", err)
	}

	t.Setenv("OCIS_ACCESS_TOKEN", "environment-token")
	dependencies := Dependencies{
		Config: &memoryConfig{store: &store{
			Version: 1, Current: "work",
			Profiles: map[string]profile{"work": {
				Server: "https://cloud.example", AuthType: "oidc",
			}},
		}},
		Credentials: &memoryCredentials{secrets: map[string]credentials.Secret{}},
	}
	value, err := newClientWithOptions(
		context.Background(), "", RunOptions{Dependencies: dependencies}.normalized(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if value.profile.AccessToken != "environment-token" {
		t.Fatalf("access token: %q", value.profile.AccessToken)
	}
}

func TestStreamHelperErrors(t *testing.T) {
	t.Setenv("OCIS_PASSWORD", "")
	if _, err := obtainPassword(RunOptions{Err: io.Discard}.normalized()); err == nil {
		t.Fatal("non-interactive password acquisition succeeded")
	}
}
