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
var totalCoveragePattern = regexp.MustCompile(
	`total:\s+\(statements\)\s+([0-9]+(?:\.[0-9]+)?)%`,
)

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
		if packageName == "app" {
			value, err := applicationCoverage()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				failed = true
				continue
			}
			fmt.Printf("%s/...: merged coverage: %.1f%% of statements\n", target, value)
			if value < *minimum {
				fmt.Fprintf(
					os.Stderr, "%s/...: coverage %.1f%% is below %.1f%%\n",
					target, value, *minimum,
				)
				failed = true
			}
			continue
		}
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

func applicationCoverage() (float64, error) {
	profile, err := os.CreateTemp("", "ocis-cli-app-coverage-*.out")
	if err != nil {
		return 0, fmt.Errorf("create application coverage profile: %w", err)
	}
	name := profile.Name()
	if err := profile.Close(); err != nil {
		_ = os.Remove(name)
		return 0, fmt.Errorf("close application coverage profile: %w", err)
	}
	defer func() { _ = os.Remove(name) }()
	command := exec.Command( //nolint:gosec // fixed Go executable and repository-owned package patterns
		"go", "test", "-coverpkg=./internal/app/...",
		"-coverprofile="+name, "./internal/app/...",
	)
	var testOutput bytes.Buffer
	command.Stdout, command.Stderr = &testOutput, &testOutput
	if err := command.Run(); err != nil {
		fmt.Print(testOutput.String())
		return 0, fmt.Errorf("application coverage tests failed: %w", err)
	}
	command = exec.Command("go", "tool", "cover", "-func="+name) //nolint:gosec // fixed Go executable and generated temporary profile
	var coverageOutput bytes.Buffer
	command.Stdout, command.Stderr = &coverageOutput, &coverageOutput
	if err := command.Run(); err != nil {
		return 0, fmt.Errorf(
			"summarize application coverage: %w: %s",
			err, coverageOutput.String(),
		)
	}
	match := totalCoveragePattern.FindStringSubmatch(coverageOutput.String())
	if len(match) != 2 {
		return 0, fmt.Errorf("application coverage total not found")
	}
	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0, fmt.Errorf("parse application coverage: %w", err)
	}
	return value, nil
}
