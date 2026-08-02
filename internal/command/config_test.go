package command

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigCommandsAreDiscoverable(t *testing.T) {
	root := NewRootCommand()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"config", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"path", "paths", "show"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf(
				"config help does not contain %q:\n%s",
				expected, output.String(),
			)
		}
	}
}

func TestConfigPathCommandPrintsEffectivePath(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "custom.json")
	t.Setenv("OCIS_CONFIG", configPath)
	root := NewRootCommand()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"config", "path"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != configPath+"\n" {
		t.Fatalf("output=%q want=%q", got, configPath+"\n")
	}
}
