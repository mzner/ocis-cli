// Command releaseformula creates the Homebrew formula for release archives.
package main

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/template"
)

var versionPattern = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+-]*$`)

type target struct {
	OS, Arch, Archive, SHA256 string
}

type formulaData struct {
	Version string
	Targets map[string]target
}

const formulaTemplate = `class OcisCli < Formula
  desc "Script-friendly CLI for oCIS-compatible servers"
  homepage "https://github.com/mzner/ocis-cli"
  license "Apache-2.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/mzner/ocis-cli/releases/download/v{{ .Version }}/{{ (index .Targets "darwin/arm64").Archive }}"
      sha256 "{{ (index .Targets "darwin/arm64").SHA256 }}"
    else
      url "https://github.com/mzner/ocis-cli/releases/download/v{{ .Version }}/{{ (index .Targets "darwin/amd64").Archive }}"
      sha256 "{{ (index .Targets "darwin/amd64").SHA256 }}"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/mzner/ocis-cli/releases/download/v{{ .Version }}/{{ (index .Targets "linux/arm64").Archive }}"
      sha256 "{{ (index .Targets "linux/arm64").SHA256 }}"
    else
      url "https://github.com/mzner/ocis-cli/releases/download/v{{ .Version }}/{{ (index .Targets "linux/amd64").Archive }}"
      sha256 "{{ (index .Targets "linux/amd64").SHA256 }}"
    end
  end

  def install
    bin.install "ocis"
    generate_completions_from_executable(bin/"ocis", "completion")
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/ocis --version")
  end
end
`

func main() {
	dist := flag.String("dist", "dist", "GoReleaser output directory")
	version := flag.String("version", "", "release version without a leading v")
	output := flag.String("output", "", "formula output path")
	flag.Parse()
	if *output == "" {
		*output = filepath.Join(*dist, "homebrew", "Formula", "ocis-cli.rb")
	}
	if err := run(*dist, *output, *version); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "generate Homebrew formula:", err)
		os.Exit(1)
	}
}

func run(dist, output, requestedVersion string) error {
	checksums, err := loadChecksums(filepath.Join(dist, "checksums.txt"))
	if err != nil {
		return err
	}
	version := strings.TrimPrefix(requestedVersion, "v")
	if version == "" {
		version, err = inferVersion(checksums)
		if err != nil {
			return err
		}
	}
	if !versionPattern.MatchString(version) {
		return fmt.Errorf("invalid version %q", version)
	}
	targets := make(map[string]target, 4)
	for _, value := range []target{
		{OS: "darwin", Arch: "amd64"},
		{OS: "darwin", Arch: "arm64"},
		{OS: "linux", Arch: "amd64"},
		{OS: "linux", Arch: "arm64"},
	} {
		value.Archive = fmt.Sprintf("ocis-cli_%s_%s_%s.tar.gz", version, value.OS, value.Arch)
		value.SHA256 = checksums[value.Archive]
		if value.SHA256 == "" {
			return fmt.Errorf("checksum is missing for %s", value.Archive)
		}
		targets[value.OS+"/"+value.Arch] = value
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o750); err != nil {
		return fmt.Errorf("create formula directory: %w", err)
	}
	var rendered bytes.Buffer
	tmpl, err := template.New("formula").Parse(formulaTemplate)
	if err == nil {
		err = tmpl.Execute(&rendered, formulaData{Version: version, Targets: targets})
	}
	if err != nil {
		return fmt.Errorf("render formula: %w", err)
	}
	if err := os.WriteFile(output, rendered.Bytes(), 0o644); err != nil { //nolint:gosec // a Homebrew formula is public metadata.
		return fmt.Errorf("write formula: %w", err)
	}
	return nil
}

func loadChecksums(path string) (map[string]string, error) {
	file, err := os.Open(path) //nolint:gosec // path is derived from the selected dist directory.
	if err != nil {
		return nil, fmt.Errorf("open checksums: %w", err)
	}
	defer func() { _ = file.Close() }()
	checksums := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 || len(fields[0]) != 64 {
			return nil, fmt.Errorf("invalid checksum line %q", scanner.Text())
		}
		if _, err := hex.DecodeString(fields[0]); err != nil {
			return nil, fmt.Errorf("invalid checksum line %q", scanner.Text())
		}
		checksums[fields[1]] = fields[0]
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read checksums: %w", err)
	}
	return checksums, nil
}

func inferVersion(checksums map[string]string) (string, error) {
	const prefix = "ocis-cli_"
	const suffix = "_darwin_arm64.tar.gz"
	versions := make([]string, 0, 1)
	for name := range checksums {
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, suffix) {
			versions = append(versions, strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix))
		}
	}
	sort.Strings(versions)
	if len(versions) == 0 {
		return "", errors.New("cannot infer version from release checksums")
	}
	if len(versions) != 1 {
		return "", fmt.Errorf("found multiple release versions: %s", strings.Join(versions, ", "))
	}
	return versions[0], nil
}
