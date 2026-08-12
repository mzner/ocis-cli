package transfer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// ErrDestinationExists reports a no-clobber commit collision.
var ErrDestinationExists = errors.New("destination already exists")

// ReplaceFile atomically replaces destination with temporary where supported,
// preserving the previous destination if the final rename fails.
func ReplaceFile(temporary, destination string) error {
	return replaceFile(temporary, destination)
}

// CommitFile installs a completed temporary file. The no-overwrite path uses
// an atomic hard-link creation, so a destination created after preflight is not
// silently replaced. temporary and destination must be on the same filesystem.
func CommitFile(temporary, destination string, overwrite bool) error {
	if overwrite {
		return ReplaceFile(temporary, destination)
	}
	if err := os.Link(temporary, destination); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%w: %s", ErrDestinationExists, destination)
		}
		return fmt.Errorf("commit download without overwrite: %w", err)
	}
	if err := os.Remove(temporary); err != nil {
		return fmt.Errorf(
			"archive was installed at %s but temporary-file cleanup failed: %w",
			destination, err,
		)
	}
	return nil
}
