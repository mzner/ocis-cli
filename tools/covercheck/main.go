// Command covercheck enforces per-package coverage for the CLI's core layers.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
)

var coveragePattern = regexp.MustCompile(`coverage:\s+([0-9]+(?:\.[0-9]+)?)%`)

func main() {
	minimum := flag.Float64("min", 70, "minimum statement coverage percentage")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: covercheck [-min PERCENT] PACKAGE...")
		os.Exit(2)
	}
	failed := false
	for _, packageName := range flag.Args() {
		target := "./internal/" + packageName
		command := exec.Command("go", "test", "-cover", target) //nolint:gosec // fixed executable, no shell, and package arguments are developer input
		var output bytes.Buffer
		command.Stdout, command.Stderr = &output, &output
		err := command.Run()
		fmt.Print(output.String())
		if err != nil {
			failed = true
			continue
		}
		match := coveragePattern.FindStringSubmatch(output.String())
		if len(match) != 2 {
			fmt.Fprintf(os.Stderr, "%s: coverage result not found\n", target)
			failed = true
			continue
		}
		value, err := strconv.ParseFloat(match[1], 64)
		if err != nil || value < *minimum {
			fmt.Fprintf(os.Stderr, "%s: coverage %.1f%% is below %.1f%%\n", target, value, *minimum)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}
