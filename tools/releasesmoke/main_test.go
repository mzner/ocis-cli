package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseArchiveName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, version, goos, goarch string
		ok                          bool
	}{
		{name: "ocis-cli_0.1.0_linux_amd64.tar.gz", version: "0.1.0", goos: "linux", goarch: "amd64", ok: true},
		{name: "ocis-cli_0.2.0-rc.1_windows_arm64.zip", version: "0.2.0-rc.1", goos: "windows", goarch: "arm64", ok: true},
		{name: "ocis-cli__darwin_arm64.tar.gz"},
		{name: "other_0.1.0_linux_amd64.tar.gz"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			artifact, ok := parseArchiveName("dist", test.name, releaseTargets())
			if ok != test.ok {
				t.Fatalf("matched: got %t, want %t", ok, test.ok)
			}
			if !ok {
				return
			}
			if artifact.version != test.version || artifact.target.goos != test.goos ||
				artifact.target.goarch != test.goarch {
				t.Fatalf("artifact: %#v", artifact)
			}
		})
	}
}

func TestReadChecksums(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, "checksums.txt")
	value := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  archive.tar.gz\n"
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
	checksums, err := readChecksums(path)
	if err != nil {
		t.Fatal(err)
	}
	if checksums["archive.tar.gz"] != value[:64] {
		t.Fatalf("checksums: %#v", checksums)
	}
}

func TestReadChecksumsRejectsMalformedInput(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "checksums.txt")
	if err := os.WriteFile(path, []byte("not-a-checksum\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readChecksums(path); err == nil {
		t.Fatal("expected malformed checksums to fail")
	}
}

func TestVerifyHomebrewFormula(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "ocis-cli.rb")
	value := `class OcisCli < Formula
  url "https://example.invalid/releases/download/v1.0.0/archive.tar.gz"
  def install
    bin.install "ocis"
  end
end
`
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyHomebrewFormula(path, "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := verifyHomebrewFormula(path, "1.0.1"); err == nil {
		t.Fatal("expected a version mismatch to fail")
	}
}

func TestVerifyScoopManifest(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "ocis-cli.json")
	value := `{
  "version": "1.0.0",
  "license": "Apache-2.0",
  "architecture": {
    "64bit": {"url": "https://example.invalid/amd64.zip", "bin": ["ocis.exe"]},
    "arm64": {"url": "https://example.invalid/arm64.zip", "bin": ["ocis.exe"]}
  }
}`
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyScoopManifest(path, "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := verifyScoopManifest(path, "1.0.1"); err == nil {
		t.Fatal("expected a version mismatch to fail")
	}
}
