package harness

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

var sensitiveEnvironment = map[string]struct{}{ //nolint:gochecknoglobals // immutable process-boundary denylist
	"OCIS_ACCESS_TOKEN":   {},
	"OCIS_CLIENT_SECRET":  {},
	"OCIS_CONFIG":         {},
	"OCIS_PASSWORD":       {},
	"OCIS_SHARE_PASSWORD": {},
	"OCIS_STATE_DIR":      {},
	"OCIS_SYNC_JOBS":      {},
	"OCIS_USER_PASSWORD":  {},
}

const defaultCommandTimeout = 2 * time.Minute

// Result contains the observable process result for one CLI invocation.
type Result struct {
	Args     []string
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
}

// Runner executes the compiled CLI with an isolated config file.
type Runner struct {
	Binary     string
	ConfigPath string
	StateDir   string
	Timeout    time.Duration
}

// Run executes one CLI command. Additional environment values are applied
// after removing inherited authentication overrides.
func (runner Runner) Run(
	parent context.Context, environment map[string]string, args ...string,
) Result {
	timeout := runner.Timeout
	if timeout <= 0 {
		timeout = defaultCommandTimeout
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	command := exec.CommandContext(ctx, runner.Binary, args...) //nolint:gosec // test executes the explicitly configured CLI binary
	command.Stdout = &stdout
	command.Stderr = &stderr
	command.Env = runner.commandEnvironment(environment)
	err := command.Run()
	result := Result{
		Args: append([]string(nil), args...), Stdout: stdout.String(),
		Stderr: stderr.String(), ExitCode: processExitCode(err), Err: err,
	}
	if ctx.Err() != nil {
		result.Err = ctx.Err()
	}
	return result
}

// Success executes a command that must succeed.
func (runner Runner) Success(
	ctx context.Context, environment map[string]string, args ...string,
) (Result, error) {
	result := runner.Run(ctx, environment, args...)
	if result.ExitCode != 0 || result.Err != nil {
		return result, fmt.Errorf(
			"ocis %s failed with exit code %d: %s",
			strings.Join(args, " "), result.ExitCode,
			SanitizeText(strings.TrimSpace(result.Stderr)),
		)
	}
	return result, nil
}

func (runner Runner) commandEnvironment(extra map[string]string) []string {
	values := make([]string, 0, len(os.Environ())+len(extra)+1)
	for _, value := range os.Environ() {
		name, _, ok := strings.Cut(value, "=")
		if !ok {
			continue
		}
		if _, sensitive := sensitiveEnvironment[name]; sensitive {
			continue
		}
		if _, replaced := extra[name]; replaced {
			continue
		}
		values = append(values, value)
	}
	values = append(values, "OCIS_CONFIG="+runner.ConfigPath)
	stateDirectory := runner.StateDir
	if stateDirectory == "" {
		stateDirectory = runner.ConfigPath + ".state"
	}
	values = append(values, "OCIS_STATE_DIR="+stateDirectory)
	for name, value := range extra {
		values = append(values, name+"="+value)
	}
	return values
}

func processExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode()
	}
	return -1
}

// DescribeResult returns a sanitized diagnostic suitable for test logs.
func DescribeResult(result Result) string {
	return fmt.Sprintf(
		"command: ocis %s\nexit code: %s\nstdout:\n%s\nstderr:\n%s",
		strings.Join(result.Args, " "), strconv.Itoa(result.ExitCode),
		SanitizeText(result.Stdout), SanitizeText(result.Stderr),
	)
}
