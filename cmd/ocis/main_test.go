package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	appoutput "github.com/mzner/ocis-cli/internal/output"
)

func TestRunMapsUsageErrorToExitCode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"cp", "/only-source"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code: got %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "accepts 2 arg") {
		t.Fatalf("stderr: %q", stderr.String())
	}
}

func TestRunWritesStructuredJSONError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", "cp", "/only-source"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code: got %d, want 2", code)
	}
	var envelope struct {
		SchemaVersion string              `json:"schemaVersion"`
		Type          string              `json:"type"`
		Data          appoutput.ErrorData `json:"data"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error output: %v\n%s", err, stderr.String())
	}
	if envelope.Type != "error" || envelope.Data.Code != 2 ||
		envelope.Data.Kind != "usage" || envelope.Data.Operation != "ocis cp" {
		t.Fatalf("envelope: %#v", envelope)
	}
}

func TestRunHelpSucceeds(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code: got %d, stderr %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Available Commands") {
		t.Fatalf("stdout: %q", stdout.String())
	}
}

func TestRunContextMapsCancellationToExitCode130(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stdout, stderr bytes.Buffer
	code := runContext(
		ctx, []string{"--json", "server", "list"}, &stdout, &stderr,
	)
	if code != 130 {
		t.Fatalf("exit code: got %d, want 130; stderr %q", code, stderr.String())
	}
	var envelope struct {
		Type string              `json:"type"`
		Data appoutput.ErrorData `json:"data"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &envelope); err != nil {
		t.Fatalf("decode cancellation output: %v\n%s", err, stderr.String())
	}
	if envelope.Type != "error" || envelope.Data.Code != 130 ||
		envelope.Data.Kind != "canceled" {
		t.Fatalf("envelope: %#v", envelope)
	}
}
