package archiver

import (
	"archive/tar"
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
)

const (
	defaultMaxEntries = int64(10_000)
	defaultMaxBytes   = int64(1 << 30)
)

// ValidationLimits bound archive decoding. Non-positive values use
// conservative oCIS-compatible defaults instead of permitting unbounded
// decompression when a server omitted capability limits.
type ValidationLimits struct {
	MaxEntries int64
	MaxBytes   int64
}

// ValidateFile reads the complete archive before it is committed to the user
// selected destination. This detects truncated streams and ZIP checksum
// failures, including server errors written after response streaming began.
func ValidateFile(name, format string, limits ValidationLimits) error {
	limits = normalizedLimits(limits)
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "zip":
		return validateZIP(name, limits)
	case "tar":
		return validateTAR(name, limits)
	default:
		return fmt.Errorf("unsupported archive format %q", format)
	}
}

func validateZIP(name string, limits ValidationLimits) error {
	archive, err := zip.OpenReader(name)
	if err != nil {
		return fmt.Errorf("validate ZIP archive: %w", err)
	}
	defer func() { _ = archive.Close() }()
	if int64(len(archive.File)) > limits.MaxEntries {
		return fmt.Errorf(
			"validate ZIP archive: archive contains more than %d entries",
			limits.MaxEntries,
		)
	}
	var total int64
	for _, entry := range archive.File {
		if entry.UncompressedSize64 > math.MaxInt64 ||
			int64(entry.UncompressedSize64) > limits.MaxBytes-total {
			return fmt.Errorf(
				"validate ZIP archive: uncompressed content exceeds %d bytes",
				limits.MaxBytes,
			)
		}
		total += int64(entry.UncompressedSize64)
		reader, err := entry.Open()
		if err != nil {
			return fmt.Errorf("validate ZIP entry %q: %w", entry.Name, err)
		}
		_, copyErr := io.CopyN(
			io.Discard, reader, int64(entry.UncompressedSize64),
		)
		if copyErr == nil {
			var extra [1]byte
			_, copyErr = reader.Read(extra[:])
			if errors.Is(copyErr, io.EOF) {
				copyErr = nil
			} else if copyErr == nil {
				copyErr = errors.New("entry exceeds its declared size")
			}
		}
		closeErr := reader.Close()
		if copyErr != nil {
			return fmt.Errorf("validate ZIP entry %q: %w", entry.Name, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close ZIP entry %q: %w", entry.Name, closeErr)
		}
	}
	return nil
}

func validateTAR(name string, limits ValidationLimits) error {
	file, err := os.Open(name) //nolint:gosec // user-selected archive temporary file
	if err != nil {
		return fmt.Errorf("open TAR archive: %w", err)
	}
	defer func() { _ = file.Close() }()
	reader := tar.NewReader(file)
	var entries, total int64
	for {
		entry, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("validate TAR archive: %w", err)
		}
		entries++
		if entries > limits.MaxEntries {
			return fmt.Errorf(
				"validate TAR archive: archive contains more than %d entries",
				limits.MaxEntries,
			)
		}
		if entry.Size < 0 || entry.Size > limits.MaxBytes-total {
			return fmt.Errorf(
				"validate TAR archive: content exceeds %d bytes",
				limits.MaxBytes,
			)
		}
		total += entry.Size
		if _, err := io.CopyN(io.Discard, reader, entry.Size); err != nil {
			return fmt.Errorf("validate TAR entry %q: %w", entry.Name, err)
		}
	}
}

func normalizedLimits(limits ValidationLimits) ValidationLimits {
	if limits.MaxEntries <= 0 {
		limits.MaxEntries = defaultMaxEntries
	}
	if limits.MaxBytes <= 0 {
		limits.MaxBytes = defaultMaxBytes
	}
	return limits
}
