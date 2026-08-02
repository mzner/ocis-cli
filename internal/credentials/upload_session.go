package credentials

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

const (
	uploadSessionServiceName = serviceName + ".upload-session"
	uploadSessionVersion     = 1
)

// UploadSession contains the protected state required to resume one TUS
// upload. UploadURL may contain a transfer token and must remain in the
// operating-system credential service.
type UploadSession struct {
	Version           int    `json:"version"`
	UploadURL         string `json:"uploadUrl"`
	SourceFingerprint string `json:"sourceFingerprint"`
	ExpiresAt         int64  `json:"expiresAt,omitempty"`
}

// GetUploadSession retrieves a protected resumable-upload session.
func GetUploadSession(profileName, key string) (UploadSession, error) {
	value, err := keyring.Get(uploadSessionServiceName, uploadSessionAccount(profileName, key))
	if err == keyring.ErrNotFound || errors.Is(err, keyring.ErrNotFound) {
		return UploadSession{}, ErrNotFound
	}
	if err != nil {
		return UploadSession{}, serviceError("read upload session for", profileName, err)
	}
	var session UploadSession
	if err := json.Unmarshal([]byte(value), &session); err != nil {
		return UploadSession{}, fmt.Errorf(
			"decode upload session for %q: %w", profileName, err,
		)
	}
	if session.Version != uploadSessionVersion {
		return UploadSession{}, fmt.Errorf(
			"upload session for %q uses unsupported format %d",
			profileName, session.Version,
		)
	}
	if session.UploadURL == "" || session.SourceFingerprint == "" {
		return UploadSession{}, fmt.Errorf(
			"upload session for %q is incomplete", profileName,
		)
	}
	return session, nil
}

// SetUploadSession saves a protected resumable-upload session.
func SetUploadSession(profileName, key string, session UploadSession) error {
	session.Version = uploadSessionVersion
	if session.UploadURL == "" || session.SourceFingerprint == "" {
		return fmt.Errorf("upload session for %q is incomplete", profileName)
	}
	value, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("encode upload session for %q: %w", profileName, err)
	}
	if err := keyring.Set(
		uploadSessionServiceName, uploadSessionAccount(profileName, key),
		string(value),
	); err != nil {
		return serviceError("save upload session for", profileName, err)
	}
	return nil
}

// DeleteUploadSession removes a protected resumable-upload session.
func DeleteUploadSession(profileName, key string) error {
	err := keyring.Delete(uploadSessionServiceName, uploadSessionAccount(profileName, key))
	if err == keyring.ErrNotFound || errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	if err != nil {
		return serviceError("delete upload session for", profileName, err)
	}
	return nil
}

func uploadSessionAccount(profileName, key string) string {
	return profileName + ":" + key
}
