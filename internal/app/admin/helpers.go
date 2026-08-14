package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/mzner/ocis-cli/internal/apperror"
	"github.com/mzner/ocis-cli/internal/graph"
	appoutput "github.com/mzner/ocis-cli/internal/output"
	"github.com/mzner/ocis-cli/internal/sharing"
)

func output(options Options, kind string, value any, format string, args ...any) error {
	return (appoutput.Renderer{Writer: options.Out, Mode: options.OutputMode, Type: kind}).Write(value, format, args...)
}

func writeOutput(options Options, kind string, value any) error {
	return output(options, kind, value, "")
}

func fallback(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func protocolStatus(err error) int {
	var statusErr interface{ HTTPStatusCode() int }
	if errors.As(err, &statusErr) {
		return statusErr.HTTPStatusCode()
	}
	return 0
}

func adminCapabilities(ctx context.Context, selected Client, options Options) (sharing.Capabilities, bool) {
	capabilities, err := selected.Sharing().Capabilities(ctx)
	if err != nil {
		options.Logger.Debug("could not read optional administration capabilities", "error", err)
		return sharing.Capabilities{}, false
	}
	return capabilities, true
}

func RequireWritableUserFields(capabilities sharing.Capabilities, fields ...string) error {
	for _, field := range fields {
		for _, readOnly := range capabilities.Graph.Users.ReadOnlyAttributes {
			if strings.EqualFold(strings.TrimSpace(readOnly), field) {
				return fmt.Errorf("server advertises %s as read-only", field)
			}
		}
	}
	return nil
}

func requireWritableUserFields(capabilities sharing.Capabilities, fields ...string) error {
	return RequireWritableUserFields(capabilities, fields...)
}

func resolveMutationUser(ctx context.Context, selected Client, identifier string) (graph.DirectoryUser, error) {
	user, err := selected.Graph().GetUser(ctx, identifier)
	if err != nil {
		return graph.DirectoryUser{}, err
	}
	if strings.TrimSpace(user.ID) == "" {
		return graph.DirectoryUser{}, fmt.Errorf("server returned user %q without a stable ID", user.DisplayName)
	}
	return user, nil
}

func resolveMutationGroup(ctx context.Context, selected Client, identifier string) (graph.DirectoryGroup, error) {
	group, err := selected.Graph().GetGroup(ctx, identifier)
	if err != nil {
		return graph.DirectoryGroup{}, err
	}
	if strings.TrimSpace(group.ID) == "" {
		return graph.DirectoryGroup{}, fmt.Errorf("server returned group %q without a stable ID", group.DisplayName)
	}
	return group, nil
}

func RejectReadOnlyGroup(group graph.DirectoryGroup) error {
	if AdminGroupAccess(group) == "read-only" {
		return fmt.Errorf("group %q is managed by a read-only identity backend", group.DisplayName)
	}
	return nil
}

func rejectReadOnlyGroup(group graph.DirectoryGroup) error { return RejectReadOnlyGroup(group) }

func rejectSelfTarget(ctx context.Context, selected Client, target graph.DirectoryUser, operation string) error {
	current, err := selected.Graph().GetMe(ctx)
	if err != nil {
		return fmt.Errorf("verify current account before %s: %w", operation, err)
	}
	if current.ID == target.ID {
		return apperror.Wrap(apperror.KindConflict, operation, fmt.Errorf("refusing to %s the currently authenticated account %q", operation, target.Username))
	}
	return nil
}

func AdminMutationError(resource string, err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "read-only") || strings.Contains(message, "configured read only") {
		return fmt.Errorf("%s is managed by a read-only identity backend", resource)
	}
	switch protocolStatus(err) {
	case http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return fmt.Errorf("server does not expose the requested %s mutation", resource)
	default:
		return err
	}
}

func adminMutationError(resource string, err error) error { return AdminMutationError(resource, err) }
