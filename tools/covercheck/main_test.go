package main

import "testing"

func TestCoveragePatterns(t *testing.T) {
	packageMatch := coveragePattern.FindStringSubmatch(
		"coverage: 81.2% of statements",
	)
	if len(packageMatch) != 2 || packageMatch[1] != "81.2" {
		t.Fatalf("package match: %v", packageMatch)
	}
	totalMatch := totalCoveragePattern.FindStringSubmatch(
		"total:\t(statements)\t\t76.4%",
	)
	if len(totalMatch) != 2 || totalMatch[1] != "76.4" {
		t.Fatalf("total match: %v", totalMatch)
	}
}
