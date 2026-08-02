package command

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/mzner/ocis-cli/internal/apperror"
)

func TestSearchCommandIsDiscoverable(t *testing.T) {
	root := NewRootCommand()
	command, _, err := root.Find([]string{"find", "report"})
	if err != nil || command.Name() != "search" {
		t.Fatalf("command=%v err=%v", command, err)
	}
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"search", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"--all-spaces", "--path", "--type", "--media-type", "--content",
		"--raw", "--min-size", "--max-size", "--modified-after", "--limit",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("help missing %q:\n%s", expected, output.String())
		}
	}
}

func TestSearchFlagParsingRejectsInvalidValuesBeforeProfileLoad(t *testing.T) {
	for _, args := range [][]string{
		{"search", "report", "--min-size", "many"},
		{"search", "report", "--min-size", "unlimited"},
		{"search", "report", "--modified-after", "tomorrow-ish"},
		{"search", "report", "--limit", "0"},
		{"search", "report", "--limit", "1001"},
	} {
		root := NewRootCommand()
		root.SetArgs(args)
		err := root.Execute()
		if !apperror.IsKind(err, apperror.KindUsage) {
			t.Fatalf("%v: %v", args, err)
		}
	}
}

func TestParseOptionalSearchTime(t *testing.T) {
	date, err := parseOptionalSearchTime("2026-07-27")
	if err != nil || date.Format(time.RFC3339) != "2026-07-27T00:00:00Z" {
		t.Fatalf("date=%v err=%v", date, err)
	}
	instant, err := parseOptionalSearchTime("2026-07-27T12:30:00+02:00")
	if err != nil || instant.UTC().Format(time.RFC3339) != "2026-07-27T10:30:00Z" {
		t.Fatalf("instant=%v err=%v", instant, err)
	}
	empty, err := parseOptionalSearchTime("")
	if err != nil || empty != nil {
		t.Fatalf("empty=%v err=%v", empty, err)
	}
}
