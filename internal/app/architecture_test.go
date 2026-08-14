package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestDomainPackagesDoNotReachOuterLayers(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, domain := range []string{
		"admin", "archive", "filesystem", "share", "spaces", "sync",
	} {
		directory := filepath.Join(root, domain)
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") ||
				strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			name := filepath.Join(directory, entry.Name())
			parsed, err := parser.ParseFile(
				token.NewFileSet(), name, nil, parser.ImportsOnly,
			)
			if err != nil {
				t.Fatal(err)
			}
			for _, imported := range parsed.Imports {
				assertAllowedDomainImport(t, domain, name, imported)
			}
		}
	}
}

func assertAllowedDomainImport(
	t *testing.T, domain, name string, imported *ast.ImportSpec,
) {
	t.Helper()
	path, err := strconv.Unquote(imported.Path.Value)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := path == "github.com/mzner/ocis-cli/internal/app" ||
		strings.HasPrefix(path, "github.com/mzner/ocis-cli/internal/command") ||
		strings.HasPrefix(path, "github.com/spf13/cobra")
	if forbidden {
		t.Errorf(
			"application domain %s imports outer layer %s in %s",
			domain, path, name,
		)
	}
}
