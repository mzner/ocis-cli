package credentials

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}

func TestRoundTrip(t *testing.T) {
	want := Secret{
		Password: "password", ClientSecret: "client",
		AccessToken: "access", RefreshToken: "refresh",
	}
	if err := Set("work", want); err != nil {
		t.Fatal(err)
	}
	got, err := Get("work")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	if _, err := keyring.Get(serviceName, "work"); !errors.Is(
		err, keyring.ErrNotFound,
	) {
		t.Fatalf("combined credential entry exists: %v", err)
	}
	for _, field := range secretFields() {
		if _, err := keyring.Get(field.service, "work"); err != nil {
			t.Fatalf("split credential entry %s missing: %v", field.service, err)
		}
	}
	if format, err := keyring.Get(formatServiceName, "work"); err != nil ||
		format != splitFormat {
		t.Fatalf("format marker = %q, %v", format, err)
	}
}

func TestDeleteAndMissing(t *testing.T) {
	if err := Set("temporary", Secret{AccessToken: "token"}); err != nil {
		t.Fatal(err)
	}
	if err := Delete("temporary"); err != nil {
		t.Fatal(err)
	}
	if _, err := Get("temporary"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestSetClearsIndividualSecret(t *testing.T) {
	if err := Set("clear-one", Secret{
		Password: "password", RefreshToken: "refresh",
	}); err != nil {
		t.Fatal(err)
	}
	if err := Set("clear-one", Secret{RefreshToken: "refresh"}); err != nil {
		t.Fatal(err)
	}
	got, err := Get("clear-one")
	if err != nil {
		t.Fatal(err)
	}
	if got.Password != "" || got.RefreshToken != "refresh" {
		t.Fatalf("credentials = %#v", got)
	}
}

func TestGetRejectsIncompleteSplitCredentials(t *testing.T) {
	if err := keyring.Set(
		serviceName+".access-token", "incomplete", "access",
	); err != nil {
		t.Fatal(err)
	}
	if _, err := Get("incomplete"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestUnavailableCredentialServiceHasActionableSentinel(t *testing.T) {
	keyring.MockInitWithError(errors.New("service locked"))
	t.Cleanup(keyring.MockInit)
	_, err := Get("locked")
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "unavailable or locked") {
		t.Fatalf("unexpected error: %v", err)
	}
}
