package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	t.Parallel()
	dist := t.TempDir()
	version := "1.0.0"
	lines := make([]string, 0, 4)
	for _, platform := range []string{
		"darwin_amd64", "darwin_arm64", "linux_amd64", "linux_arm64",
	} {
		lines = append(lines, strings.Repeat("a", 64)+"  ocis-cli_"+version+"_"+platform+".tar.gz")
	}
	if err := os.WriteFile(filepath.Join(dist, "checksums.txt"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dist, "homebrew", "Formula", "ocis-cli.rb")
	if err := run(dist, output, version); err != nil {
		t.Fatal(err)
	}
	value, err := os.ReadFile(output) //nolint:gosec // test-owned path.
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`class OcisCli < Formula`,
		`releases/download/v1.0.0/`,
		`ocis-cli_1.0.0_darwin_arm64.tar.gz`,
		`ocis-cli_1.0.0_linux_amd64.tar.gz`,
		`bin.install "ocis"`,
	} {
		if !strings.Contains(string(value), expected) {
			t.Fatalf("formula is missing %q", expected)
		}
	}
}

func TestInferVersion(t *testing.T) {
	t.Parallel()
	version, err := inferVersion(map[string]string{
		"ocis-cli_2.3.4_darwin_arm64.tar.gz": strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	if version != "2.3.4" {
		t.Fatalf("version: got %q, want 2.3.4", version)
	}
}

func TestLoadChecksumsRejectsInvalidDigest(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "checksums.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("z", 64)+"  archive.tar.gz\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadChecksums(path); err == nil {
		t.Fatal("expected an invalid digest to fail")
	}
}
