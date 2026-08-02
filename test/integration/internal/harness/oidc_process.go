package harness

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// OIDCLogin runs the CLI's loopback login while browser completes the embedded
// IDP flow.
func (runner Runner) OIDCLogin(
	parent context.Context,
	browser *OIDCBrowser,
	profile string,
	username string,
	password string,
) (Result, error) {
	timeout := runner.Timeout
	if timeout <= 0 {
		timeout = defaultCommandTimeout
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	commandArgs := []string{"auth", "login", profile, "--no-browser"}
	command := exec.CommandContext(ctx, runner.Binary, commandArgs...) //nolint:gosec // test executes the explicitly configured CLI binary
	command.Env = runner.commandEnvironment(nil)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return Result{}, err
	}
	type outputState struct {
		value string
		err   error
	}
	authorizationURL := make(chan string, 1)
	outputDone := make(chan outputState, 1)
	go func() {
		var output strings.Builder
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			output.WriteString(line)
			output.WriteByte('\n')
			if strings.HasPrefix(line, "http://") ||
				strings.HasPrefix(line, "https://") {
				select {
				case authorizationURL <- strings.TrimSpace(line):
				default:
				}
			}
		}
		outputDone <- outputState{value: output.String(), err: scanner.Err()}
	}()
	waitDone := make(chan error, 1)
	go func() { waitDone <- command.Wait() }()

	var target string
	select {
	case target = <-authorizationURL:
	case waitErr := <-waitDone:
		output := <-outputDone
		result := processResult(commandArgs, output.value, stderr.String(), waitErr)
		return result, fmt.Errorf(
			"OIDC login exited before emitting an authorization URL: %s",
			DescribeResult(result),
		)
	case <-ctx.Done():
		return Result{Args: commandArgs, ExitCode: -1, Err: ctx.Err()}, ctx.Err()
	}
	if err := browser.Authenticate(ctx, target, username, password); err != nil {
		cancel()
		<-waitDone
		output := <-outputDone
		result := processResult(commandArgs, output.value, stderr.String(), err)
		return result, err
	}
	waitErr := <-waitDone
	output := <-outputDone
	result := processResult(commandArgs, output.value, stderr.String(), waitErr)
	if output.err != nil {
		return result, output.err
	}
	if waitErr != nil {
		return result, fmt.Errorf("OIDC login failed: %s", DescribeResult(result))
	}
	return result, nil
}

func processResult(
	args []string, stdout, stderr string, err error,
) Result {
	return Result{
		Args: append([]string(nil), args...), Stdout: stdout, Stderr: stderr,
		ExitCode: processExitCode(err), Err: err,
	}
}
