package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestExpireProfile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(
		`{"version":3,"current":"admin","profiles":{"admin":{"expiresAt":42}}}`,
	), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ExpireProfile(configPath, "admin"); err != nil {
		t.Fatal(err)
	}
	expiry, err := ProfileExpiry(configPath, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if expiry != 1 {
		t.Fatalf("expiry = %d, want 1", expiry)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var document profileDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if document.Version != 3 || document.Current != "admin" {
		t.Fatalf("config metadata was not preserved: %#v", document)
	}
}
