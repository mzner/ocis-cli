package command

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mzner/ocis-cli/internal/app"
	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func runServer(command *cobra.Command, options *globalOptions, request app.ServerRequest) error {
	return app.RunServerWithOptions(command.Context(), request, options.runOptions(command))
}

func runConfig(
	command *cobra.Command,
	options *globalOptions,
	request app.ConfigRequest,
) error {
	return app.RunConfigWithOptions(
		command.Context(), request, options.runOptions(command),
	)
}

func usageError(operation, message string) error {
	return apperror.Wrap(apperror.KindUsage, operation, fmt.Errorf("%s", message))
}

func confirmAction(command *cobra.Command, prompt string) (bool, error) {
	if _, err := fmt.Fprintf(command.ErrOrStderr(), "%s [y/N] ", prompt); err != nil {
		return false, err
	}
	line, err := bufio.NewReader(command.InOrStdin()).ReadString('\n')
	if err != nil {
		return false, err
	}
	confirmed := strings.EqualFold(strings.TrimSpace(line), "y") ||
		strings.EqualFold(strings.TrimSpace(line), "yes")
	if !confirmed {
		_, _ = fmt.Fprintln(command.ErrOrStderr(), "Cancelled.")
	}
	return confirmed, nil
}

func runAuth(command *cobra.Command, options *globalOptions, request app.AuthRequest) error {
	return app.RunAuthWithOptions(command.Context(), request, options.profile, options.runOptions(command))
}

func runFilesystem(command *cobra.Command, options *globalOptions, request app.FilesystemRequest) error {
	return app.RunFilesystemWithOptions(command.Context(), request, options.profile, options.runOptions(command))
}

func runDoctor(command *cobra.Command, options *globalOptions, profile string) error {
	return app.RunDoctorWithOptions(command.Context(), profile, options.runOptions(command))
}

func runSpace(command *cobra.Command, options *globalOptions, request app.SpaceRequest) error {
	return app.RunSpaceWithOptions(
		command.Context(), request, options.profile, options.runOptions(command),
	)
}

func runSpaceCreate(
	command *cobra.Command, options *globalOptions, request app.SpaceCreateRequest,
) error {
	return app.RunSpaceCreateWithOptions(
		command.Context(), request, options.profile, options.runOptions(command),
	)
}

func runSpaceUpdate(
	command *cobra.Command, options *globalOptions, request app.SpaceUpdateRequest,
) error {
	return app.RunSpaceUpdateWithOptions(
		command.Context(), request, options.profile, options.runOptions(command),
	)
}

func runSpaceLifecycle(
	command *cobra.Command,
	options *globalOptions,
	request app.SpaceLifecycleRequest,
) error {
	return app.RunSpaceLifecycleWithOptions(
		command.Context(), request, options.profile, options.runOptions(command),
	)
}

func runSpaceMember(
	command *cobra.Command,
	options *globalOptions,
	request app.SpaceMemberRequest,
) error {
	return app.RunSpaceMemberWithOptions(
		command.Context(), request, options.profile, options.runOptions(command),
	)
}

func runShare(command *cobra.Command, options *globalOptions, request app.ShareRequest) error {
	return app.RunShareWithOptions(
		command.Context(), request, options.profile, options.runOptions(command),
	)
}

func runTrash(
	command *cobra.Command, options *globalOptions, request app.TrashRequest,
) error {
	return app.RunTrashWithOptions(
		command.Context(), request, options.profile, options.runOptions(command),
	)
}

func runVersion(
	command *cobra.Command, options *globalOptions, request app.VersionRequest,
) error {
	return app.RunVersionWithOptions(
		command.Context(), request, options.profile, options.runOptions(command),
	)
}

func runSearch(
	command *cobra.Command, options *globalOptions, request app.SearchRequest,
) error {
	return app.RunSearchWithOptions(
		command.Context(), request, options.profile, options.runOptions(command),
	)
}

func readSecret(command *cobra.Command, environment, prompt string) (string, error) {
	if value := os.Getenv(environment); value != "" {
		return value, nil
	}
	input, ok := command.InOrStdin().(interface{ Fd() uintptr })
	if !ok || !term.IsTerminal(int(input.Fd())) {
		return "", fmt.Errorf("%s is required when no interactive terminal is available", environment)
	}
	if _, err := fmt.Fprint(command.ErrOrStderr(), prompt); err != nil {
		return "", err
	}
	value, err := term.ReadPassword(int(input.Fd()))
	_, _ = fmt.Fprintln(command.ErrOrStderr())
	if err != nil {
		return "", err
	}
	result := strings.TrimRight(string(value), "\r\n")
	if result == "" {
		return "", errors.New("password cannot be empty")
	}
	return result, nil
}

// RenderError writes an error according to the root command's output mode.
func RenderError(command *cobra.Command, err error, code int) error {
	mode := OutputMode(command)
	if mode == appoutput.Human {
		_, writeErr := fmt.Fprintln(command.ErrOrStderr(), "error:", err)
		return writeErr
	}
	kind, operation := apperror.Details(err)
	return (appoutput.Renderer{
		Writer: command.ErrOrStderr(), Mode: mode, Type: "error",
	}).Write(appoutput.ErrorData{
		Code: code, Kind: string(kind), Message: err.Error(), Operation: operation,
	}, "")
}
