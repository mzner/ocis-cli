package transfer

// ReplaceFile atomically replaces destination with temporary where supported,
// preserving the previous destination if the final rename fails.
func ReplaceFile(temporary, destination string) error {
	return replaceFile(temporary, destination)
}
