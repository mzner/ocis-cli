// Command releasesmoke validates the contents of a GoReleaser dist directory.
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const maximumBinarySize = 256 << 20

type releaseTarget struct {
	goos, goarch, extension, binary string
}

type archiveArtifact struct {
	path, name, version string
	target              releaseTarget
}

func main() {
	dist := flag.String("dist", "dist", "GoReleaser output directory")
	version := flag.String("version", "", "expected version without a leading v")
	flag.Parse()
	if err := run(*dist, *version, os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "release smoke test:", err)
		os.Exit(1)
	}
}

func run(dist, expectedVersion string, output io.Writer) error {
	archives, version, err := discoverArchives(dist, expectedVersion)
	if err != nil {
		return err
	}
	checksums, err := readChecksums(filepath.Join(dist, "checksums.txt"))
	if err != nil {
		return err
	}
	if err := verifyChecksums(dist, checksums); err != nil {
		return err
	}

	nativeVerified := false
	for _, archive := range archives {
		if _, ok := checksums[archive.name]; !ok {
			return fmt.Errorf("checksum is missing for %s", archive.name)
		}
		binary, err := inspectArchive(archive)
		if err != nil {
			return err
		}
		if err := verifySBOM(archive.path + ".sbom.json"); err != nil {
			return err
		}
		if archive.target.goos == runtime.GOOS && archive.target.goarch == runtime.GOARCH {
			if err := verifyVersion(binary, version); err != nil {
				return err
			}
			nativeVerified = true
		}
	}
	if !nativeVerified {
		return fmt.Errorf("no archive matches smoke-test host %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if err := verifyHomebrewFormula(
		filepath.Join(dist, "homebrew", "Formula", "ocis-cli.rb"), version,
	); err != nil {
		return err
	}
	if err := verifyScoopManifest(
		filepath.Join(dist, "scoop", "ocis-cli.json"), version,
	); err != nil {
		return err
	}
	_, err = fmt.Fprintf(
		output,
		"verified %d archives, %d SBOMs, checksums, Homebrew formula, Scoop manifest, and ocis version %s\n",
		len(archives), len(archives), version,
	)
	return err
}

func releaseTargets() []releaseTarget {
	return []releaseTarget{
		{goos: "darwin", goarch: "amd64", extension: ".tar.gz", binary: "ocis"},
		{goos: "darwin", goarch: "arm64", extension: ".tar.gz", binary: "ocis"},
		{goos: "linux", goarch: "amd64", extension: ".tar.gz", binary: "ocis"},
		{goos: "linux", goarch: "arm64", extension: ".tar.gz", binary: "ocis"},
		{goos: "windows", goarch: "amd64", extension: ".zip", binary: "ocis.exe"},
		{goos: "windows", goarch: "arm64", extension: ".zip", binary: "ocis.exe"},
	}
}

func discoverArchives(dist, expectedVersion string) ([]archiveArtifact, string, error) {
	entries, err := os.ReadDir(dist)
	if err != nil {
		return nil, "", fmt.Errorf("read dist directory: %w", err)
	}
	targets := releaseTargets()
	found := make(map[string]archiveArtifact, len(targets))
	version := strings.TrimPrefix(expectedVersion, "v")
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		artifact, ok := parseArchiveName(dist, entry.Name(), targets)
		if !ok {
			continue
		}
		key := artifact.target.goos + "/" + artifact.target.goarch
		if _, exists := found[key]; exists {
			return nil, "", fmt.Errorf("duplicate archive for %s", key)
		}
		if version == "" {
			version = artifact.version
		}
		if artifact.version != version {
			return nil, "", fmt.Errorf(
				"archive %s has version %s, expected %s",
				artifact.name, artifact.version, version,
			)
		}
		found[key] = artifact
	}
	if version == "" {
		return nil, "", errors.New("no release archives found")
	}
	archives := make([]archiveArtifact, 0, len(targets))
	for _, target := range targets {
		key := target.goos + "/" + target.goarch
		artifact, ok := found[key]
		if !ok {
			return nil, "", fmt.Errorf("archive is missing for %s", key)
		}
		archives = append(archives, artifact)
	}
	return archives, version, nil
}

func parseArchiveName(
	dist, name string, targets []releaseTarget,
) (archiveArtifact, bool) {
	const prefix = "ocis-cli_"
	if !strings.HasPrefix(name, prefix) {
		return archiveArtifact{}, false
	}
	for _, target := range targets {
		suffix := "_" + target.goos + "_" + target.goarch + target.extension
		if !strings.HasSuffix(name, suffix) {
			continue
		}
		version := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
		if version == "" {
			return archiveArtifact{}, false
		}
		return archiveArtifact{
			path: filepath.Join(dist, name), name: name,
			version: version, target: target,
		}, true
	}
	return archiveArtifact{}, false
}

func inspectArchive(artifact archiveArtifact) ([]byte, error) {
	var (
		files  map[string]bool
		binary []byte
		err    error
	)
	if artifact.target.extension == ".zip" {
		files, binary, err = inspectZip(artifact)
	} else {
		files, binary, err = inspectTarGzip(artifact)
	}
	if err != nil {
		return nil, err
	}
	for _, required := range []string{
		artifact.target.binary, "README.md", "LICENSE",
	} {
		if !files[required] {
			return nil, fmt.Errorf("archive %s is missing %s", artifact.name, required)
		}
	}
	return binary, nil
}

func inspectTarGzip(artifact archiveArtifact) (map[string]bool, []byte, error) {
	file, err := os.Open(artifact.path)
	if err != nil {
		return nil, nil, fmt.Errorf("open %s: %w", artifact.name, err)
	}
	defer func() { _ = file.Close() }()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		return nil, nil, fmt.Errorf("open gzip %s: %w", artifact.name, err)
	}
	defer func() { _ = compressed.Close() }()
	files := make(map[string]bool)
	var binary []byte
	reader := tar.NewReader(compressed)
	for {
		header, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return nil, nil, fmt.Errorf("read %s: %w", artifact.name, nextErr)
		}
		name := filepath.Base(header.Name)
		files[name] = true
		if name == artifact.target.binary {
			binary, err = readLimited(reader)
			if err != nil {
				return nil, nil, fmt.Errorf("read binary from %s: %w", artifact.name, err)
			}
		}
	}
	return files, binary, nil
}

func inspectZip(artifact archiveArtifact) (map[string]bool, []byte, error) {
	reader, err := zip.OpenReader(artifact.path)
	if err != nil {
		return nil, nil, fmt.Errorf("open %s: %w", artifact.name, err)
	}
	defer func() { _ = reader.Close() }()
	files := make(map[string]bool)
	var binary []byte
	for _, entry := range reader.File {
		name := filepath.Base(entry.Name)
		files[name] = true
		if name != artifact.target.binary {
			continue
		}
		opened, openErr := entry.Open()
		if openErr != nil {
			return nil, nil, fmt.Errorf("open binary from %s: %w", artifact.name, openErr)
		}
		binary, err = readLimited(opened)
		_ = opened.Close()
		if err != nil {
			return nil, nil, fmt.Errorf("read binary from %s: %w", artifact.name, err)
		}
	}
	return files, binary, nil
}

func readLimited(reader io.Reader) ([]byte, error) {
	value, err := io.ReadAll(io.LimitReader(reader, maximumBinarySize+1))
	if err != nil {
		return nil, err
	}
	if len(value) > maximumBinarySize {
		return nil, errors.New("binary exceeds size limit")
	}
	return value, nil
}

func readChecksums(path string) (map[string]string, error) {
	value, err := os.ReadFile(path) //nolint:gosec // path is the selected dist directory.
	if err != nil {
		return nil, fmt.Errorf("read checksums: %w", err)
	}
	checksums := make(map[string]string)
	for number, line := range strings.Split(strings.TrimSpace(string(value)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("invalid checksum line %d", number+1)
		}
		if _, err := hex.DecodeString(fields[0]); err != nil || len(fields[0]) != sha256.Size*2 {
			return nil, fmt.Errorf("invalid SHA-256 on checksum line %d", number+1)
		}
		if _, exists := checksums[fields[1]]; exists {
			return nil, fmt.Errorf("duplicate checksum for %s", fields[1])
		}
		checksums[fields[1]] = strings.ToLower(fields[0])
	}
	if len(checksums) == 0 {
		return nil, errors.New("checksums.txt is empty")
	}
	return checksums, nil
}

func verifyChecksums(dist string, checksums map[string]string) error {
	names := make([]string, 0, len(checksums))
	for name := range checksums {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(dist, filepath.Base(name))
		file, err := os.Open(path) //nolint:gosec // path is restricted to a dist basename.
		if err != nil {
			return fmt.Errorf("open checksummed artifact %s: %w", name, err)
		}
		digest := sha256.New()
		_, copyErr := io.Copy(digest, file)
		closeErr := file.Close()
		if copyErr != nil {
			return fmt.Errorf("checksum %s: %w", name, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close %s: %w", name, closeErr)
		}
		actual := hex.EncodeToString(digest.Sum(nil))
		if actual != checksums[name] {
			return fmt.Errorf("checksum mismatch for %s", name)
		}
	}
	return nil
}

func verifySBOM(path string) error {
	file, err := os.Open(path) //nolint:gosec // path is derived from a release archive.
	if err != nil {
		return fmt.Errorf("open SBOM %s: %w", filepath.Base(path), err)
	}
	defer func() { _ = file.Close() }()
	var document map[string]any
	if err := json.NewDecoder(file).Decode(&document); err != nil {
		return fmt.Errorf("decode SBOM %s: %w", filepath.Base(path), err)
	}
	if len(document) == 0 {
		return fmt.Errorf("SBOM %s is empty", filepath.Base(path))
	}
	return nil
}

func verifyHomebrewFormula(path, version string) error {
	value, err := os.ReadFile(path) //nolint:gosec // path is the generated manifest in dist.
	if err != nil {
		return fmt.Errorf("read Homebrew formula: %w", err)
	}
	content := string(value)
	for _, required := range []string{
		`class OcisCli < Formula`,
		`releases/download/v` + version + `/`,
		`bin.install "ocis"`,
	} {
		if !strings.Contains(content, required) {
			return fmt.Errorf("homebrew formula is missing %q", required)
		}
	}
	return nil
}

func verifyScoopManifest(path, version string) error {
	value, err := os.ReadFile(path) //nolint:gosec // path is the generated manifest in dist.
	if err != nil {
		return fmt.Errorf("read Scoop manifest: %w", err)
	}
	var manifest struct {
		Version      string `json:"version"`
		License      string `json:"license"`
		Architecture map[string]struct {
			Bin []string `json:"bin"`
		} `json:"architecture"`
	}
	if err := json.Unmarshal(value, &manifest); err != nil {
		return fmt.Errorf("decode Scoop manifest: %w", err)
	}
	if manifest.Version != version {
		return fmt.Errorf("scoop manifest has version %q, expected %q", manifest.Version, version)
	}
	if manifest.License != "Apache-2.0" {
		return fmt.Errorf("scoop manifest has license %q, expected Apache-2.0", manifest.License)
	}
	for _, architecture := range []string{"64bit", "arm64"} {
		entry, ok := manifest.Architecture[architecture]
		if !ok {
			return fmt.Errorf("scoop manifest is missing %s architecture", architecture)
		}
		if len(entry.Bin) != 1 || entry.Bin[0] != "ocis.exe" {
			return fmt.Errorf("scoop manifest has invalid %s bin entry %q", architecture, entry.Bin)
		}
	}
	return nil
}

func verifyVersion(binary []byte, version string) error {
	if len(binary) == 0 {
		return errors.New("native archive has no binary payload")
	}
	directory, err := os.MkdirTemp("", "ocis-release-smoke-")
	if err != nil {
		return fmt.Errorf("create temporary directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(directory) }()
	path := filepath.Join(directory, "ocis")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	if err := os.WriteFile(path, binary, 0o700); err != nil { //nolint:gosec // executable is a generated release binary in a private directory.
		return fmt.Errorf("write temporary binary: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, path, "--version") //nolint:gosec // path is the extracted release binary selected above.
	value, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("run packaged binary: %w: %s", err, strings.TrimSpace(string(value)))
	}
	want := "ocis version " + version
	if strings.TrimSpace(string(value)) != want {
		return fmt.Errorf("packaged binary reports %q, expected %q", strings.TrimSpace(string(value)), want)
	}
	return nil
}
