package transfer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceFilePreservesNewContent(t *testing.T) {
	root := t.TempDir()
	temporary := filepath.Join(root, "download.part")
	destination := filepath.Join(root, "download.txt")
	if err := os.WriteFile(temporary, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceFile(temporary, destination); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "new" {
		t.Fatalf("destination: %q, %v", data, err)
	}
}
