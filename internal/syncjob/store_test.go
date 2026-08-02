package syncjob

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	syncmodel "github.com/mzner/ocis-cli/internal/sync"
)

func TestRoundTripPermissionsAndCustomPath(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "jobs.json")
	t.Setenv(pathEnvironment, storePath)
	job := validJob()
	store := Empty()
	store.Jobs[job.Name] = job
	if err := Save(store); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, store) {
		t.Fatalf("loaded=%#v expected=%#v", loaded, store)
	}
	info, err := os.Stat(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
}

func TestMissingAndValidation(t *testing.T) {
	t.Setenv(pathEnvironment, filepath.Join(t.TempDir(), "missing.json"))
	store, err := Load()
	if err != nil || store.Version != CurrentVersion ||
		len(store.Jobs) != 0 {
		t.Fatalf("store=%#v err=%v", store, err)
	}
	for _, job := range []Job{
		{},
		func() Job { value := validJob(); value.Name = "../bad"; return value }(),
		func() Job { value := validJob(); value.Profile = ""; return value }(),
		func() Job { value := validJob(); value.AccountID = ""; return value }(),
		func() Job { value := validJob(); value.Direction = "sideways"; return value }(),
		func() Job { value := validJob(); value.LocalRoot = "relative"; return value }(),
		func() Job { value := validJob(); value.RemoteRoot = ""; return value }(),
		func() Job { value := validJob(); value.RemoteRoot = "relative"; return value }(),
		func() Job { value := validJob(); value.RemoteRoot = "/not/../clean"; return value }(),
		func() Job { value := validJob(); value.Excludes = []string{"["}; return value }(),
		func() Job { value := validJob(); value.MaxEntries = 0; return value }(),
	} {
		if err := Validate(job); err == nil {
			t.Fatalf("accepted job %#v", job)
		}
	}
	store = Empty()
	store.Jobs["wrong-map-key"] = validJob()
	if err := Save(store); err == nil {
		t.Fatal("mismatched map key accepted")
	}
	store.Version = 99
	if err := Save(store); err == nil {
		t.Fatal("unsupported version accepted")
	}
}

func TestPathFollowsConfigOverride(t *testing.T) {
	t.Setenv(pathEnvironment, "")
	configPath := filepath.Join(t.TempDir(), "custom-config.json")
	t.Setenv("OCIS_CONFIG", configPath)
	got, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(filepath.Dir(configPath), "sync-jobs.json")
	if got != want {
		t.Fatalf("path=%q want=%q", got, want)
	}
}

func TestRejectsMalformedAndFutureFiles(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "jobs.json")
	t.Setenv(pathEnvironment, storePath)
	for _, contents := range []string{
		"{broken",
		`{"version":99,"jobs":{}}`,
		`{"version":1,"jobs":{"wrong":{"name":"other"}}}`,
		`{"version":1,"jobs":{},"password":"secret"}`,
		`{"version":1,"jobs":{}} {"version":1,"jobs":{}}`,
	} {
		if err := os.WriteFile(storePath, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(); err == nil {
			t.Fatalf("accepted %q", contents)
		}
	}
}

func validJob() Job {
	return Job{
		Name: "website", Profile: "work", AccountID: "v1:account",
		SpaceID: "space", Direction: syncmodel.Push,
		LocalRoot: "/local", RemoteRoot: "/remote",
		Includes: []string{"*.html"}, Excludes: []string{"*.tmp"},
		Delete: true, MaxEntries: 100,
	}
}
