package credentials

import (
	"errors"
	"testing"
)

func TestUploadSessionRoundTrip(t *testing.T) {
	want := UploadSession{
		UploadURL:         "https://cloud.test/data/transfer-token",
		SourceFingerprint: "source-fingerprint",
		ExpiresAt:         1234567890,
	}
	if err := SetUploadSession("work", "destination-key", want); err != nil {
		t.Fatal(err)
	}
	got, err := GetUploadSession("work", "destination-key")
	if err != nil {
		t.Fatal(err)
	}
	want.Version = uploadSessionVersion
	if got != want {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	if err := DeleteUploadSession("work", "destination-key"); err != nil {
		t.Fatal(err)
	}
	if _, err := GetUploadSession(
		"work", "destination-key",
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestUploadSessionRejectsIncompleteState(t *testing.T) {
	err := SetUploadSession("work", "destination-key", UploadSession{
		SourceFingerprint: "source-fingerprint",
	})
	if err == nil {
		t.Fatal("expected incomplete session error")
	}
}
