package command

import (
	"bytes"
	"strings"
	"testing"
)

func TestAdminListSearchHelp(t *testing.T) {
	for _, args := range [][]string{
		{"admin", "user", "list", "--help"},
		{"admin", "group", "list", "--help"},
	} {
		root := NewRootCommand()
		var output bytes.Buffer
		root.SetOut(&output)
		root.SetErr(&output)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		for _, expected := range []string{
			"--search", "--search-raw", "literal text",
			"exact server-side LibreGraph",
		} {
			if !strings.Contains(output.String(), expected) {
				t.Fatalf(
					"%v help missing %q:\n%s",
					args, expected, output.String(),
				)
			}
		}
	}
}

func TestAdminListSearchFlagsAreMutuallyExclusive(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{
		"admin", "user", "list",
		"--search", "Alice Example",
		"--search-raw", `"Alice Example"`,
	})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "were all set") {
		t.Fatalf("error: %v", err)
	}
}
