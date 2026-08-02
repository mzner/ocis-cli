package harness

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config contains the externally supplied integration environment.
type Config struct {
	Binary             string
	Server             string
	Insecure           bool
	AdminUsername      string
	AdminPassword      string
	RestrictedUsername string
	RestrictedPassword string
	CommandTimeout     time.Duration
}

// LoadConfig reads and validates the integration environment.
func LoadConfig() (Config, error) {
	if os.Getenv("OCIS_INTEGRATION") != "1" {
		return Config{}, errors.New(
			"integration tests are disabled; set OCIS_INTEGRATION=1",
		)
	}
	insecure, err := strconv.ParseBool(environment(
		"OCIS_INTEGRATION_INSECURE", "true",
	))
	if err != nil {
		return Config{}, fmt.Errorf("parse OCIS_INTEGRATION_INSECURE: %w", err)
	}
	timeout, err := time.ParseDuration(environment(
		"OCIS_INTEGRATION_COMMAND_TIMEOUT", "2m",
	))
	if err != nil || timeout <= 0 {
		return Config{}, fmt.Errorf(
			"parse OCIS_INTEGRATION_COMMAND_TIMEOUT: expected positive duration",
		)
	}
	config := Config{
		Binary:             environment("OCIS_INTEGRATION_BINARY", "bin/ocis"),
		Server:             environment("OCIS_INTEGRATION_SERVER", "https://localhost:9200"),
		Insecure:           insecure,
		AdminUsername:      environment("OCIS_INTEGRATION_ADMIN_USERNAME", "admin"),
		AdminPassword:      environment("OCIS_INTEGRATION_ADMIN_PASSWORD", "admin"),
		RestrictedUsername: environment("OCIS_INTEGRATION_RESTRICTED_USERNAME", "einstein"),
		RestrictedPassword: environment("OCIS_INTEGRATION_RESTRICTED_PASSWORD", "relativity"),
		CommandTimeout:     timeout,
	}
	if _, err := os.Stat(config.Binary); err != nil {
		return Config{}, fmt.Errorf("integration binary %q: %w", config.Binary, err)
	}
	return config, nil
}

func environment(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
