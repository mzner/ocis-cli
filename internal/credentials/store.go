// Package credentials stores profile secrets in the operating system's
// credential service.
package credentials

import (
	"errors"
	"fmt"
	"runtime"

	"github.com/zalando/go-keyring"
)

// BackendName returns the operating-system credential service used by the
// production keyring adapter. It does not access or unlock the service.
func BackendName() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS Keychain"
	case "windows":
		return "Windows Credential Manager"
	default:
		return "Secret Service"
	}
}

const (
	serviceName       = "io.github.mzner.ocis-cli"
	formatServiceName = serviceName + ".format"
	splitFormat       = "2"
)

type secretField struct {
	service string
	value   func(Secret) string
	assign  func(*Secret, string)
}

var (
	// ErrNotFound indicates that a profile has no saved credentials.
	ErrNotFound = errors.New("credentials not found")
	// ErrUnavailable indicates that the OS credential service is unavailable,
	// locked, or inaccessible to the current session.
	ErrUnavailable = errors.New("OS credential service is unavailable or locked")
)

// Secret contains authentication material that must never be written to the
// regular configuration file.
type Secret struct {
	Password     string `json:"password,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"`
	AccessToken  string `json:"accessToken,omitempty"`
	RefreshToken string `json:"refreshToken,omitempty"`
}

// Empty reports whether the value contains authentication material.
func (secret Secret) Empty() bool {
	return secret.Password == "" &&
		secret.ClientSecret == "" &&
		secret.AccessToken == "" &&
		secret.RefreshToken == ""
}

// Get retrieves a profile's secrets from the OS credential service.
func Get(profileName string) (Secret, error) {
	format, err := keyring.Get(formatServiceName, profileName)
	if err != nil && err != keyring.ErrNotFound &&
		!errors.Is(err, keyring.ErrNotFound) {
		return Secret{}, serviceError("read", profileName, err)
	}
	if err == nil {
		if format != splitFormat {
			return Secret{}, fmt.Errorf(
				"credentials for %q use unsupported format %q",
				profileName, format,
			)
		}
		secret, found, splitErr := getSplit(profileName)
		if splitErr != nil {
			return Secret{}, splitErr
		}
		if !found {
			return Secret{}, fmt.Errorf(
				"credentials for %q are incomplete", profileName,
			)
		}
		return secret, nil
	}
	return Secret{}, ErrNotFound
}

func getSplit(profileName string) (Secret, bool, error) {
	var secret Secret
	found := false
	for _, field := range secretFields() {
		value, err := keyring.Get(field.service, profileName)
		if err == keyring.ErrNotFound || errors.Is(err, keyring.ErrNotFound) {
			continue
		}
		if err != nil {
			return Secret{}, false, serviceError(
				"read", profileName, err,
			)
		}
		field.assign(&secret, value)
		found = true
	}
	return secret, found, nil
}

// Set saves a profile's secrets in the OS credential service. Empty values
// remove their existing entries. Each value uses a separate keychain item so
// large OAuth tokens do not exceed an operating system's per-item size limit.
func Set(profileName string, secret Secret) error {
	if secret.Empty() {
		return Delete(profileName)
	}
	if err := deleteFromService(formatServiceName, profileName); err != nil {
		return fmt.Errorf(
			"prepare credentials for %q: %w", profileName, err,
		)
	}
	for _, field := range secretFields() {
		value := field.value(secret)
		if value == "" {
			if err := deleteFromService(field.service, profileName); err != nil {
				return fmt.Errorf(
					"clear credentials for %q: %w", profileName, err,
				)
			}
			continue
		}
		if err := keyring.Set(field.service, profileName, value); err != nil {
			return serviceError("save", profileName, err)
		}
	}
	if err := keyring.Set(
		formatServiceName, profileName, splitFormat,
	); err != nil {
		return serviceError("save format for", profileName, err)
	}
	return nil
}

// Delete removes a profile's secrets from the OS credential service.
func Delete(profileName string) error {
	if err := deleteFromService(formatServiceName, profileName); err != nil {
		return fmt.Errorf("delete credentials for %q: %w", profileName, err)
	}
	for _, field := range secretFields() {
		if err := deleteFromService(field.service, profileName); err != nil {
			return fmt.Errorf("delete credentials for %q: %w", profileName, err)
		}
	}
	return nil
}

func secretFields() []secretField {
	return []secretField{
		{
			service: serviceName + ".password",
			value:   func(secret Secret) string { return secret.Password },
			assign: func(secret *Secret, value string) {
				secret.Password = value
			},
		},
		{
			service: serviceName + ".client-secret",
			value:   func(secret Secret) string { return secret.ClientSecret },
			assign: func(secret *Secret, value string) {
				secret.ClientSecret = value
			},
		},
		{
			service: serviceName + ".access-token",
			value:   func(secret Secret) string { return secret.AccessToken },
			assign: func(secret *Secret, value string) {
				secret.AccessToken = value
			},
		},
		{
			service: serviceName + ".refresh-token",
			value:   func(secret Secret) string { return secret.RefreshToken },
			assign: func(secret *Secret, value string) {
				secret.RefreshToken = value
			},
		},
	}
}

func deleteFromService(service, profileName string) error {
	err := keyring.Delete(service, profileName)
	if err == keyring.ErrNotFound || errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return nil
}

func serviceError(operation, profileName string, err error) error {
	return fmt.Errorf("%s credentials for %q: %w: %v", operation, profileName, ErrUnavailable, err)
}
