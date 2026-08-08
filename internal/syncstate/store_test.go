package syncstate

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	syncmodel "github.com/mzner/ocis-cli/internal/sync"
)

func TestStateRoundTripAndPermissions(t *testing.T) {
	directory := t.TempDir()
	t.Setenv(stateDirectoryEnvironment, directory)
	binding := syncmodel.Binding{
		Profile: "work", AccountID: "account", SpaceID: "space",
		Direction: syncmodel.Push,
		LocalRoot: "/local", RemoteRoot: "/remote",
	}
	key := binding.Key()
	state := syncmodel.NewState(
		binding,
		syncmodel.Snapshot{
			"": {Path: "", Type: "directory"},
		},
		syncmodel.Snapshot{
			"": {Path: "", Type: "directory"},
		},
	)
	if err := Save(key, state); err != nil {
		t.Fatal(err)
	}
	loaded, found, err := Load(key)
	if err != nil || !found || !reflect.DeepEqual(loaded, state) {
		t.Fatalf("loaded=%#v found=%t err=%v", loaded, found, err)
	}
	info, err := os.Stat(filepath.Join(directory, key+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("mode: %o", info.Mode().Perm())
	}
}

func TestStateValidationAndMissing(t *testing.T) {
	t.Setenv(stateDirectoryEnvironment, t.TempDir())
	if _, _, err := Load("../escape"); err == nil {
		t.Fatal("unsafe key accepted")
	}
	key := string(make([]byte, 64))
	if _, _, err := Load(key); err == nil {
		t.Fatal("non-hex key accepted")
	}
	binding := syncmodel.Binding{Direction: syncmodel.Pull}
	valid := binding.Key()
	if _, found, err := Load(valid); err != nil || found {
		t.Fatalf("found=%t err=%v", found, err)
	}
	if err := Save(valid, syncmodel.State{Version: 99}); err == nil {
		t.Fatal("unsupported state version saved")
	}
}

func TestKeysAndDelete(t *testing.T) {
	directory := t.TempDir()
	t.Setenv(stateDirectoryEnvironment, directory)
	first := syncmodel.Binding{
		Profile: "first", Direction: syncmodel.Push,
	}.Key()
	second := syncmodel.Binding{
		Profile: "second", Direction: syncmodel.Pull,
	}.Key()
	for _, key := range []string{second, first} {
		if err := os.WriteFile(
			filepath.Join(directory, key+".json"), []byte("{broken"), 0600,
		); err != nil {
			t.Fatal(err)
		}
	}
	for name, contents := range map[string]string{
		".sync-temporary": "temporary",
		"notes.txt":       "unrelated",
		"short.json":      "{}",
	} {
		if err := os.WriteFile(
			filepath.Join(directory, name), []byte(contents), 0600,
		); err != nil {
			t.Fatal(err)
		}
	}
	keys, err := Keys()
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{first, second}
	if first > second {
		expected[0], expected[1] = second, first
	}
	if !reflect.DeepEqual(keys, expected) {
		t.Fatalf("keys=%#v expected=%#v", keys, expected)
	}
	removed, err := Delete(first)
	if err != nil || !removed {
		t.Fatalf("removed=%t err=%v", removed, err)
	}
	removed, err = Delete(first)
	if err != nil || removed {
		t.Fatalf("second removal=%t err=%v", removed, err)
	}
	if _, err := Delete("../escape"); err == nil {
		t.Fatal("unsafe delete key accepted")
	}
}

func TestKeysMissingDirectory(t *testing.T) {
	t.Setenv(
		stateDirectoryEnvironment,
		filepath.Join(t.TempDir(), "does-not-exist"),
	)
	keys, err := Keys()
	if err != nil || len(keys) != 0 {
		t.Fatalf("keys=%#v err=%v", keys, err)
	}
}
