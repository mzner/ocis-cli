package archive

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	archiveclient "github.com/mzner/ocis-cli/internal/archiver"
	appoutput "github.com/mzner/ocis-cli/internal/output"
	"github.com/mzner/ocis-cli/internal/sharing"
	"github.com/mzner/ocis-cli/internal/webdav"
)

type fakeClient struct{ capabilities sharing.Capabilities }

func (client fakeClient) SelectSpace(string) error { return nil }

func (client fakeClient) Capabilities(context.Context) (sharing.Capabilities, error) {
	return client.capabilities, nil
}

func (fakeClient) Stat(string) (webdav.Item, error) {
	return webdav.Item{}, errors.New("unexpected stat")
}

func (fakeClient) List(string) ([]webdav.Item, error) {
	return nil, errors.New("unexpected list")
}

func (fakeClient) Archiver(string) (*archiveclient.Client, error) {
	return nil, errors.New("unexpected archive client")
}

func TestRunFormatsUsesPreferredEnabledCapability(t *testing.T) {
	capabilities := sharing.Capabilities{}
	capabilities.Files.Archivers = []sharing.ArchiverCapabilities{
		{Enabled: true, Version: "1.0.0", Formats: []string{"zip"}, URL: "/old"},
		{Enabled: true, Version: "2.0.0", Formats: []string{"tar", "zip"}, URL: "/archiver", MaxNumFiles: 10, MaxSize: 100},
	}
	var output bytes.Buffer
	options := Options{
		OutputMode: appoutput.JSON, Out: &output, Err: io.Discard,
		NewClient: func(context.Context, string) (Client, error) {
			return fakeClient{capabilities: capabilities}, nil
		},
	}
	if err := RunFormats(context.Background(), "work", options); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"version": "2.0.0"`) ||
		!strings.Contains(output.String(), `"format": "tar"`) ||
		strings.Contains(output.String(), `"version": "1.0.0"`) {
		t.Fatalf("output: %s", output.String())
	}
}

func TestArchiveValidationHelpers(t *testing.T) {
	if _, err := normalizeArchivePaths([]string{"/reports", "/reports/file"}); err == nil {
		t.Fatal("nested selection accepted")
	}
	if _, err := normalizeArchivePaths([]string{"/reports", "reports"}); err == nil {
		t.Fatal("duplicate selection accepted")
	}
	paths, err := normalizeArchivePaths([]string{" reports ", "/photos"})
	if err != nil || strings.Join(paths, ",") != "/reports,/photos" {
		t.Fatalf("paths=%v error=%v", paths, err)
	}
	for _, test := range []struct {
		requested   string
		destination string
		want        string
	}{
		{"", "backup", "zip"}, {"", "backup.tar", "tar"},
		{"zip", "backup.zip", "zip"}, {"tar", "backup", "tar"},
	} {
		got, err := resolveArchiveFormat(test.requested, test.destination)
		if err != nil || got != test.want {
			t.Fatalf("format %#v = %q, %v", test, got, err)
		}
	}
	for _, invalid := range [][2]string{{"rar", "a.rar"}, {"tar", "a.zip"}} {
		if _, err := resolveArchiveFormat(invalid[0], invalid[1]); err == nil {
			t.Fatalf("invalid format accepted: %v", invalid)
		}
	}
	destination := filepath.Join(t.TempDir(), "archive.zip")
	if err := validateArchiveDestination(destination, false); err != nil {
		t.Fatal(err)
	}
	if err := writeTestFile(destination); err != nil {
		t.Fatal(err)
	}
	if err := validateArchiveDestination(destination, false); err == nil {
		t.Fatal("existing destination accepted without overwrite")
	}
	if err := validateArchiveDestination(destination, true); err != nil {
		t.Fatal(err)
	}
}

func TestCapabilitySelectionAndDetail(t *testing.T) {
	values := []sharing.ArchiverCapabilities{
		{Enabled: false, Version: "9.0.0", Formats: []string{"zip"}, URL: "/disabled"},
		{Enabled: true, Version: "v2.1.0", Formats: []string{"ZIP", "tar", "rar"}, URL: "/archiver", MaxNumFiles: 12, MaxSize: 34},
	}
	selected, err := SelectCapabilities(values)
	if err != nil || selected.Version != "v2.1.0" ||
		strings.Join(selected.Formats, ",") != "tar,zip" {
		t.Fatalf("selected=%#v error=%v", selected, err)
	}
	capabilities := sharing.Capabilities{}
	capabilities.Files.Archivers = values
	detail := CapabilityDetail(capabilities)
	for _, expected := range []string{
		"version v2.1.0", "formats tar, zip", "maximum 12 entries",
		"maximum 34 source bytes",
	} {
		if !strings.Contains(detail, expected) {
			t.Fatalf("detail %q missing %q", detail, expected)
		}
	}
	if _, err := SelectCapabilities(nil); err == nil ||
		CapabilityDetail(sharing.Capabilities{}) != "not advertised" {
		t.Fatal("missing capability accepted")
	}
}
