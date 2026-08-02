package main

import (
	"bufio"
	"fmt"
	"os"

	"github.com/mzner/ocis-cli/test/integration/internal/harness"
)

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	sanitizer := harness.NewSanitizer()
	for scanner.Scan() {
		fmt.Println(sanitizer.Sanitize(scanner.Text()))
	}
	if err := scanner.Err(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "sanitize integration log: %v\n", err)
		os.Exit(1)
	}
}
