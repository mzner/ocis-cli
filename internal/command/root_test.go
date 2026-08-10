package command

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mzner/ocis-cli/internal/apperror"
)

func TestRootCommandMetadata(t *testing.T) {
	root := NewRootCommand()
	if root.Use != "ocis" {
		t.Fatalf("use: got %q", root.Use)
	}
	if root.Version == "" {
		t.Fatal("version is empty")
	}
	if root.SilenceErrors != true || root.SilenceUsage != true {
		t.Fatal("root must leave error rendering to the executable boundary")
	}
}

func TestAliasesResolve(t *testing.T) {
	root := NewRootCommand()
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"list"}, "ls"},
		{[]string{"move"}, "mv"},
		{[]string{"copy"}, "cp"},
		{[]string{"remove"}, "rm"},
		{[]string{"fs", "list"}, "ls"},
	} {
		command, _, err := root.Find(test.args)
		if err != nil {
			t.Fatalf("%v: %v", test.args, err)
		}
		if command.Name() != test.want {
			t.Fatalf("%v: got %q, want %q", test.args, command.Name(), test.want)
		}
	}
}

func TestArgumentValidation(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{"cp", "/only-source"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "accepts 2 arg") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGeneratedHelpIncludesGlobalFlags(t *testing.T) {
	root := NewRootCommand()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	help := output.String()
	for _, expected := range []string{
		"--profile", "--space", "--json", "--jsonl", "--verbose", "--timeout", "--retries", "--concurrency",
		"Manage files on an oCIS-compatible server", "ls, list", "mv, move",
		"cp, copy", "rm, remove", "space", "trash", "version, versions", "share",
		"search, find", "tag", "favorite", "property", "admin",
		"sync", "config", "cat", "tree", "du", "batch", "touch",
		"federation, federated, ocm",
	} {
		if !strings.Contains(help, expected) {
			t.Fatalf("help does not contain %q:\n%s", expected, help)
		}
	}
}

func TestFederationCommandsAndAliasesAreDiscoverable(t *testing.T) {
	for _, test := range []struct {
		command  []string
		expected []string
	}{
		{[]string{"federation"}, []string{"invite, invitation", "connection, connections"}},
		{[]string{"federation", "invite"}, []string{"create", "list, ls", "accept"}},
		{[]string{"federation", "connection"}, []string{"list, ls", "remove, rm"}},
		{[]string{"share", "federated"}, []string{"add", "roles"}},
	} {
		root := NewRootCommand()
		var output bytes.Buffer
		root.SetOut(&output)
		root.SetErr(&output)
		root.SetArgs(append(test.command, "--help"))
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		for _, expected := range test.expected {
			if !strings.Contains(output.String(), expected) {
				t.Fatalf("%v help missing %q:\n%s", test.command, expected, output.String())
			}
		}
	}
	for _, args := range [][]string{
		{"ocm", "invitation", "ls"},
		{"federated", "connections", "ls"},
		{"share", "ocm", "roles", "/report.txt"},
	} {
		root := NewRootCommand()
		if _, _, err := root.Find(args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
}

func TestAdminCommandsAndAliasesAreDiscoverable(t *testing.T) {
	for _, test := range []struct {
		command  []string
		expected []string
	}{
		{[]string{"admin"}, []string{"user, users", "group, groups", "space, spaces"}},
		{[]string{"admin", "user"}, []string{
			"list, ls", "info, stat", "create", "update", "enable",
			"disable", "delete", "role, roles",
		}},
		{[]string{"admin", "user", "role"}, []string{
			"available", "list, ls", "grant", "revoke",
		}},
		{[]string{"admin", "group"}, []string{
			"list, ls", "info, stat", "create", "update", "delete",
			"member, members",
		}},
		{[]string{"admin", "group", "member"}, []string{
			"list, ls", "add", "remove, rm",
		}},
		{[]string{"admin", "space"}, []string{
			"list, ls", "info, stat", "create", "update",
			"member, members", "disable", "restore", "delete",
		}},
	} {
		root := NewRootCommand()
		var output bytes.Buffer
		root.SetOut(&output)
		root.SetErr(&output)
		root.SetArgs(append(test.command, "--help"))
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		for _, expected := range test.expected {
			if !strings.Contains(output.String(), expected) {
				t.Fatalf(
					"%v help does not contain %q:\n%s",
					test.command, expected, output.String(),
				)
			}
		}
	}
	for _, test := range []struct {
		args []string
		name string
	}{
		{[]string{"admin", "users", "ls"}, "list"},
		{[]string{"admin", "group", "stat", "team"}, "info"},
		{[]string{"admin", "groups", "members", "ls", "team"}, "list"},
		{[]string{"admin", "spaces", "stat", "project"}, "info"},
	} {
		root := NewRootCommand()
		command, _, err := root.Find(test.args)
		if err != nil || command.Name() != test.name {
			t.Fatalf(
				"%v: command=%v name=%q err=%v",
				test.args, command, test.name, err,
			)
		}
	}
}

func TestMetadataCommandsAreDiscoverable(t *testing.T) {
	for _, test := range []struct {
		command  string
		expected []string
	}{
		{"tag", []string{"list, ls", "add", "remove, rm"}},
		{"favorite", []string{"set", "unset"}},
		{"property", []string{"get", "set", "remove, rm"}},
	} {
		root := NewRootCommand()
		var output bytes.Buffer
		root.SetOut(&output)
		root.SetErr(&output)
		root.SetArgs([]string{test.command, "--help"})
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		for _, expected := range test.expected {
			if !strings.Contains(output.String(), expected) {
				t.Fatalf(
					"%s help does not contain %q:\n%s",
					test.command, expected, output.String(),
				)
			}
		}
	}
}

func TestTagAddRequiresAPathAndTag(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{"tag", "add", "/report.txt"})
	err := root.Execute()
	if apperror.ExitCode(err) != 2 ||
		!strings.Contains(err.Error(), "at least 2 arg") {
		t.Fatalf("error: %v", err)
	}
}

func TestAuthSetupIsDiscoverable(t *testing.T) {
	root := NewRootCommand()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"auth", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"setup", "login", "status", "logout"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("auth help does not contain %q:\n%s", expected, output.String())
		}
	}
}

func TestVersionCommandsAndAliasesAreDiscoverable(t *testing.T) {
	root := NewRootCommand()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"version", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"list, ls", "info, stat", "download, get", "restore",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("version help does not contain %q:\n%s", expected, output.String())
		}
	}
	for _, test := range []struct {
		args []string
		name string
	}{
		{[]string{"versions", "ls", "/report.txt"}, "list"},
		{[]string{"version", "stat", "/report.txt", "v1"}, "info"},
		{[]string{"version", "get", "/report.txt", "v1", "./old"}, "download"},
	} {
		command, _, err := root.Find(test.args)
		if err != nil || command.Name() != test.name {
			t.Fatalf(
				"%v: command=%v name=%q err=%v",
				test.args, command, test.name, err,
			)
		}
	}
}

func TestVersionRestoreCancellationDoesNotLoadProfile(t *testing.T) {
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "missing", "config.json"))
	root := NewRootCommand()
	var output bytes.Buffer
	root.SetIn(strings.NewReader("no\n"))
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{
		"version", "restore", "/report.txt", "version-1",
	})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Cancelled.") {
		t.Fatalf("output: %q", output.String())
	}
}

func TestVersionDownloadRejectsMachineOutputToStdout(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{
		"--json", "version", "download", "/report.txt", "version-1", "-",
	})
	err := root.Execute()
	if apperror.ExitCode(err) != 2 ||
		!strings.Contains(err.Error(), "cannot be used") {
		t.Fatalf("error: %v", err)
	}
}

func TestTrashCommandsAndAliasesAreDiscoverable(t *testing.T) {
	root := NewRootCommand()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"trash", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"list, ls", "restore", "remove, rm, delete", "empty",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("trash help does not contain %q:\n%s", expected, output.String())
		}
	}
	for _, test := range []struct {
		args []string
		name string
	}{
		{[]string{"trash", "ls"}, "list"},
		{[]string{"trash", "rm", "item-id"}, "remove"},
		{[]string{"trash", "delete", "item-id"}, "remove"},
	} {
		command, _, err := root.Find(test.args)
		if err != nil {
			t.Fatalf("%v: %v", test.args, err)
		}
		if command.Name() != test.name {
			t.Fatalf("%v: command=%s", test.args, command.Name())
		}
	}
}

func TestTrashDestructiveCancellationDoesNotLoadProfile(t *testing.T) {
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "missing", "config.json"))
	for _, args := range [][]string{
		{"trash", "remove", "item-id"},
		{"trash", "empty"},
	} {
		root := NewRootCommand()
		var output bytes.Buffer
		root.SetIn(strings.NewReader("no\n"))
		root.SetOut(&output)
		root.SetErr(&output)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if !strings.Contains(output.String(), "Cancelled.") {
			t.Fatalf("%v output: %q", args, output.String())
		}
	}
}

func TestPublicLinkPermissionValidation(t *testing.T) {
	for value, want := range map[string]int{"read": 1, "upload": 5, "edit": 15} {
		got, err := parsePublicLinkPermissions(value)
		if err != nil || got != want {
			t.Errorf("%s: got %d, %v", value, got, err)
		}
	}
	if _, err := parsePublicLinkPermissions("owner"); err == nil {
		t.Fatal("invalid public-link permission was accepted")
	}
}

func TestPublicLinkCommandsAreDiscoverable(t *testing.T) {
	root := NewRootCommand()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"share", "link", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"create", "list, ls", "info, stat", "update", "revoke",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf(
				"share link help does not contain %q:\n%s",
				expected, output.String(),
			)
		}
	}
}

func TestDirectShareCommandsAndAliasesAreDiscoverable(t *testing.T) {
	root := NewRootCommand()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"share", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"user", "group", "roles", "update", "remove, rm", "received",
		"accept", "decline", "overview", "link, links", "list, ls",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf(
				"share help does not contain %q:\n%s",
				expected, output.String(),
			)
		}
	}
	for _, test := range []struct {
		args []string
		name string
	}{
		{[]string{"share", "rm", "share-id"}, "remove"},
		{[]string{"share", "links", "ls"}, "list"},
		{[]string{"share", "user", "add", "/report", "alice"}, "add"},
		{[]string{"share", "group", "add", "/report", "team"}, "add"},
	} {
		command, _, err := root.Find(test.args)
		if err != nil || command.Name() != test.name {
			t.Fatalf(
				"%v: command=%v name=%q err=%v",
				test.args, command, test.name, err,
			)
		}
	}
}

func TestShareOverviewFlagsAreDiscoverable(t *testing.T) {
	root := NewRootCommand()
	command, _, err := root.Find([]string{"share", "overview"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"direction", "state"} {
		if command.Flags().Lookup(name) == nil {
			t.Fatalf("share overview is missing --%s", name)
		}
	}
}

func TestShareRemoveCancellationDoesNotLoadProfile(t *testing.T) {
	t.Setenv(
		"OCIS_CONFIG", filepath.Join(t.TempDir(), "missing", "config.json"),
	)
	root := NewRootCommand()
	var output bytes.Buffer
	root.SetIn(strings.NewReader("no\n"))
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"share", "remove", "share-id"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Cancelled.") {
		t.Fatalf("output: %q", output.String())
	}
}

func TestSpaceCreateDryRunThroughCobra(t *testing.T) {
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "missing", "config.json"))
	root := NewRootCommand()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{
		"space", "create", "Engineering", "--description", "Shared work",
		"--quota", "2GB", "--dry-run", "--json",
	})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"operation": "create"`, `"name": "Engineering"`,
		`"description": "Shared work"`, `"quota": 2000000000`,
		`"dryRun": true`,
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("output does not contain %q: %s", expected, output.String())
		}
	}
}

func TestSpaceCreateRejectsInvalidQuota(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{"space", "create", "Engineering", "--quota", "-1GB"})
	err := root.Execute()
	if apperror.ExitCode(err) != 2 || !strings.Contains(err.Error(), "invalid quota") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSpaceAdministrationCommandsAreDiscoverable(t *testing.T) {
	root := NewRootCommand()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"space", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"create", "update", "member", "disable", "restore", "delete",
		"info, stat", "current", "unset, clear",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("Space help does not contain %q:\n%s", expected, output.String())
		}
	}
	command, _, err := root.Find([]string{"space", "members", "ls"})
	if err != nil || command.Name() != "list" {
		t.Fatalf("member aliases: command=%v err=%v", command, err)
	}
	command, _, err = root.Find([]string{"space", "stat", "Engineering"})
	if err != nil || command.Name() != "info" {
		t.Fatalf("info alias: command=%v err=%v", command, err)
	}
}

func TestPermanentSpaceDeletionRequiresExplicitFlag(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{"space", "delete", "space-id", "--dry-run"})
	err := root.Execute()
	if apperror.ExitCode(err) != 2 ||
		!strings.Contains(err.Error(), "--permanent is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStableIDLifecycleDryRunsThroughCobra(t *testing.T) {
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "missing", "config.json"))
	for _, args := range [][]string{
		{"space", "restore", "space-id", "--dry-run"},
		{"space", "delete", "space-id", "--permanent", "--dry-run"},
	} {
		root := NewRootCommand()
		var output bytes.Buffer
		root.SetOut(&output)
		root.SetErr(&output)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if !strings.Contains(output.String(), "Would") {
			t.Fatalf("%v output: %q", args, output.String())
		}
	}
}

func TestSpaceDisableCancellationDoesNotLoadProfile(t *testing.T) {
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "missing", "config.json"))
	root := NewRootCommand()
	var output bytes.Buffer
	root.SetIn(strings.NewReader("no\n"))
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"space", "disable", "Engineering"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Cancelled.") {
		t.Fatalf("output: %q", output.String())
	}
}

func TestSpaceUpdateRequiresAChange(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{"space", "update", "Engineering"})
	err := root.Execute()
	if apperror.ExitCode(err) != 2 ||
		!strings.Contains(err.Error(), "at least one") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestArgumentErrorsUseUsageExitCode(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{"cp", "/only-source"})
	err := root.Execute()
	if apperror.ExitCode(err) != 2 {
		t.Fatalf("exit code: got %d from %v", apperror.ExitCode(err), err)
	}
}

func TestMachineOutputFlagsAreMutuallyExclusive(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{"server", "list", "--json", "--jsonl"})
	err := root.Execute()
	if apperror.ExitCode(err) != 2 || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestServerListUsesCommandWriter(t *testing.T) {
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	root := NewRootCommand()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"server", "list", "--jsonl"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("empty JSONL collection should produce no records, got %q", output.String())
	}
}

func TestInteractiveCancellationDoesNotExecute(t *testing.T) {
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	root := NewRootCommand()
	var output bytes.Buffer
	root.SetIn(strings.NewReader("no\n"))
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"rm", "/report.txt", "--interactive"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Cancelled.") {
		t.Fatalf("output: %q", output.String())
	}
}

func TestStdoutDownloadRejectsMachineOutput(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{"download", "/report.txt", "-", "--json"})
	err := root.Execute()
	if apperror.ExitCode(err) != 2 ||
		!strings.Contains(err.Error(), "downloading to stdout") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCatRejectsMachineOutput(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{"cat", "/report.txt", "--jsonl"})
	err := root.Execute()
	if apperror.ExitCode(err) != 2 ||
		!strings.Contains(err.Error(), "cat writes raw file bytes") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTreeRejectsInvalidLimitsBeforeLoadingAProfile(t *testing.T) {
	for _, args := range [][]string{
		{"tree", "--max-depth", "-1"},
		{"tree", "--max-entries", "0"},
	} {
		root := NewRootCommand()
		root.SetArgs(args)
		err := root.Execute()
		if apperror.ExitCode(err) != 2 {
			t.Fatalf("%v: unexpected error: %v", args, err)
		}
	}
}

func TestFilesystemHelpIncludesReadCommands(t *testing.T) {
	root := NewRootCommand()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"filesystem", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"cat", "tree", "du", "batch"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("help does not contain %q:\n%s", expected, output.String())
		}
	}
}

func TestDUAndBatchRejectInvalidFlagsBeforeLoadingAProfile(t *testing.T) {
	for _, test := range []struct {
		args    []string
		message string
	}{
		{[]string{"du", "--max-depth", "-1"}, "--max-depth"},
		{[]string{"du", "--max-entries", "0"}, "--max-entries"},
		{[]string{"batch", "--max-operations", "0", "--dry-run"}, "--max-operations"},
		{[]string{"batch"}, "requires --yes"},
	} {
		root := NewRootCommand()
		root.SetArgs(test.args)
		err := root.Execute()
		if apperror.ExitCode(err) != 2 ||
			!strings.Contains(err.Error(), test.message) {
			t.Fatalf("%v: unexpected error: %v", test.args, err)
		}
	}
}

func TestMkdirParentsFlagIsDiscoverable(t *testing.T) {
	root := NewRootCommand()
	command, _, err := root.Find([]string{"mkdir"})
	if err != nil {
		t.Fatal(err)
	}
	flag := command.Flags().Lookup("parents")
	if flag == nil || flag.Shorthand != "p" {
		t.Fatalf("parents flag: %#v", flag)
	}
}

func TestPersistentFlagsReachNestedCommands(t *testing.T) {
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	root := NewRootCommand()
	root.SetArgs([]string{"server", "list", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestVerboseWritesDiagnosticsToStderr(t *testing.T) {
	t.Setenv("OCIS_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	root := NewRootCommand()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--verbose", "server", "list"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "debug: run server operation operation=list") {
		t.Fatalf("stderr: %q", stderr.String())
	}
}
