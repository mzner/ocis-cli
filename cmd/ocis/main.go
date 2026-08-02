// Command ocis provides command-line access to oCIS-compatible servers.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/mzner/ocis-cli/internal/apperror"
	"github.com/mzner/ocis-cli/internal/command"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), terminationSignals()...)
	defer stop()
	os.Exit(runContext(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return runContext(context.Background(), args, stdout, stderr)
}

func runContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	root := command.NewRootCommand()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	if err := root.ExecuteContext(ctx); err != nil {
		code := apperror.ExitCode(err)
		if renderErr := command.RenderError(root, err, code); renderErr != nil {
			_, _ = fmt.Fprintln(stderr, "error:", err)
		}
		return code
	}
	return 0
}
