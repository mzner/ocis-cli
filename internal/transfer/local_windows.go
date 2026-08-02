//go:build windows

package transfer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func replaceFile(temporary, destination string) error {
	if err := os.Rename(temporary, destination); err == nil {
		return nil
	}
	if _, err := os.Stat(destination); err != nil {
		return fmt.Errorf("replace %s: %w", destination, err)
	}
	backup, err := os.CreateTemp(filepath.Dir(destination), ".ocis-backup-*")
	if err != nil {
		return err
	}
	backupPath := backup.Name()
	if err := backup.Close(); err != nil {
		return err
	}
	if err := os.Remove(backupPath); err != nil {
		return err
	}
	if err := os.Rename(destination, backupPath); err != nil {
		return err
	}
	if err := os.Rename(temporary, destination); err != nil {
		if restoreErr := os.Rename(backupPath, destination); restoreErr != nil {
			return errors.Join(
				err, fmt.Errorf("restore previous destination: %w", restoreErr),
			)
		}
		return err
	}
	if err := os.Remove(backupPath); err != nil {
		return fmt.Errorf("remove download backup: %w", err)
	}
	return nil
}
