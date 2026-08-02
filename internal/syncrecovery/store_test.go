package syncrecovery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	syncmodel "github.com/mzner/ocis-cli/internal/sync"
)

func TestRecoveryJournalRoundTripAndPermissions(t *testing.T) {
	directory := t.TempDir()
	t.Setenv(pathEnvironment, directory)
	binding := syncmodel.Binding{
		Profile: "work", AccountID: "account", Direction: syncmodel.Bidirectional,
		LocalRoot: "/local", RemoteRoot: "/remote",
	}
	journal := New(
		binding, 100,
		syncmodel.Plan{Direction: syncmodel.Bidirectional},
		time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
	)
	if err := Save(journal); err != nil {
		t.Fatal(err)
	}
	loaded, found, err := Load(journal.ID)
	if err != nil || !found || loaded.ID != journal.ID || loaded.Status != Running {
		t.Fatalf("loaded=%#v found=%t err=%v", loaded, found, err)
	}
	keys, err := Keys()
	if err != nil || len(keys) != 1 || keys[0] != journal.ID {
		t.Fatalf("keys=%v err=%v", keys, err)
	}
	info, err := os.Stat(filepath.Join(directory, journal.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("journal permissions=%o", info.Mode().Perm())
	}
	removed, err := Delete(journal.ID)
	if err != nil || !removed {
		t.Fatalf("removed=%t err=%v", removed, err)
	}
}

func TestRecoveryJournalValidation(t *testing.T) {
	binding := syncmodel.Binding{
		Direction: syncmodel.Bidirectional, LocalRoot: "/local", RemoteRoot: "/remote",
	}
	valid := New(
		binding, 1, syncmodel.Plan{Direction: syncmodel.Bidirectional}, time.Now(),
	)
	for _, journal := range []Journal{
		{},
		func() Journal { value := valid; value.ID = "bad"; return value }(),
		func() Journal { value := valid; value.MaxEntries = 0; return value }(),
		func() Journal { value := valid; value.Status = "unknown"; return value }(),
		func() Journal {
			value := valid
			value.Binding.Direction = syncmodel.Push
			return value
		}(),
	} {
		if err := Validate(journal); err == nil {
			t.Fatalf("accepted invalid journal %#v", journal)
		}
	}
}

func TestRecoveryJournalMissingAndMalformedFiles(t *testing.T) {
	directory := t.TempDir()
	t.Setenv(pathEnvironment, directory)
	key := strings.Repeat("a", 64)
	if keys, err := Keys(); err != nil || len(keys) != 0 {
		t.Fatalf("keys=%v err=%v", keys, err)
	}
	if _, found, err := Load(key); err != nil || found {
		t.Fatalf("missing load found=%t err=%v", found, err)
	}
	if removed, err := Delete(key); err != nil || removed {
		t.Fatalf("missing delete removed=%t err=%v", removed, err)
	}

	name := filepath.Join(directory, key+".json")
	for _, contents := range []string{
		"{broken",
		`{"version":99,"id":"` + key + `"}`,
		`{"version":1,"id":"` + key +
			`","binding":{"direction":"bidirectional"},"maxEntries":1,"status":"running"}`,
	} {
		if err := os.WriteFile(name, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Load(key); err == nil {
			t.Fatalf("accepted malformed journal %s", contents)
		}
	}
	if err := Save(Journal{}); err == nil {
		t.Fatal("saved invalid journal")
	}
}

func TestRecoveryJournalDefaultDirectory(t *testing.T) {
	t.Setenv(pathEnvironment, "")
	directory, err := Directory()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(directory) != "sync-recovery" {
		t.Fatalf("default directory=%q", directory)
	}
}
