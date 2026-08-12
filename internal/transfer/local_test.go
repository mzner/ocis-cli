package transfer

import (
	"errors"
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

func TestCommitFileNoClobber(t *testing.T) {
	root := t.TempDir()
	temporary := filepath.Join(root, "archive.part")
	destination := filepath.Join(root, "archive.zip")
	if err := os.WriteFile(temporary, []byte("archive"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := CommitFile(temporary, destination, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(temporary); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary still exists: %v", err)
	}
	if data, err := os.ReadFile(destination); err != nil || string(data) != "archive" {
		t.Fatalf("destination=%q error=%v", data, err)
	}

	second := filepath.Join(root, "second.part")
	if err := os.WriteFile(second, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := CommitFile(second, destination, false); !errors.Is(err, ErrDestinationExists) {
		t.Fatalf("error: %v", err)
	}
	if data, err := os.ReadFile(destination); err != nil || string(data) != "archive" {
		t.Fatalf("destination changed=%q error=%v", data, err)
	}
	if _, err := os.Stat(second); err != nil {
		t.Fatalf("failed commit must retain temporary: %v", err)
	}
}

func TestCommitFileOverwrite(t *testing.T) {
	root := t.TempDir()
	temporary := filepath.Join(root, "archive.part")
	destination := filepath.Join(root, "archive.zip")
	if err := os.WriteFile(temporary, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := CommitFile(temporary, destination, true); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(destination); err != nil || string(data) != "new" {
		t.Fatalf("destination=%q error=%v", data, err)
	}
}
