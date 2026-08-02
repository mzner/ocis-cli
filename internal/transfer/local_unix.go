//go:build !windows

package transfer

import "os"

func replaceFile(temporary, destination string) error {
	return os.Rename(temporary, destination)
}
