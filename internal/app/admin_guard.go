package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/mzner/ocis-cli/internal/apperror"
	"github.com/mzner/ocis-cli/internal/graph"
	"github.com/mzner/ocis-cli/internal/sharing"
)

func newAdminMutationClient(
	ctx context.Context,
	selectedProfile string,
	options RunOptions,
) (*client, error) {
	if options.Space != "" {
		return nil, apperror.Wrap(
			apperror.KindUsage, "admin",
			errors.New(
				"--space cannot be used with administrative account operations",
			),
		)
	}
	selected, err := newClientWithOptions(ctx, selectedProfile, options)
	if err != nil {
		return nil, err
	}
	if err := requireAccountAdminMFA(ctx, selected); err != nil {
		return nil, err
	}
	return selected, nil
}

func requireAccountAdminMFA(ctx context.Context, selected *client) error {
	if err := selected.graphClient().CheckAdminMFA(ctx); err != nil {
		if requiresMFA(err) {
			return apperror.Wrap(
				apperror.KindAuthentication, "admin MFA",
				fmt.Errorf(
					"%w; run ocis auth login %s --mfa and complete the identity provider's MFA challenge",
					err, selected.name,
				),
			)
		}
		return unavailableAdminList("administrative account management", err)
	}
	return nil
}

func checkAdminSpaceMFA(
	ctx context.Context,
	selectedProfile string,
	options RunOptions,
) error {
	if options.Space != "" {
		return apperror.Wrap(
			apperror.KindUsage, "admin space",
			errors.New("--space cannot be used with Space administration"),
		)
	}
	selected, err := newClientWithOptions(ctx, selectedProfile, options)
	if err != nil {
		return err
	}
	if err := selected.graphClient().CheckMFA(ctx); err != nil {
		if requiresMFA(err) {
			return apperror.Wrap(
				apperror.KindAuthentication, "admin space MFA",
				fmt.Errorf(
					"%w; run ocis auth login %s --mfa and complete the identity provider's MFA challenge",
					err, selected.name,
				),
			)
		}
		return unavailableAdminList("MFA-protected Space inventory", err)
	}
	return nil
}

func requiresMFA(err error) bool {
	var required interface{ RequiresMFA() bool }
	return errors.As(err, &required) && required.RequiresMFA()
}

func adminCapabilities(
	ctx context.Context, selected *client,
) (sharing.Capabilities, bool) {
	capabilities, err := selected.sharingClient().Capabilities(ctx)
	if err != nil {
		selected.logger.Debug(
			"could not read optional administration capabilities",
			"error", err,
		)
		return sharing.Capabilities{}, false
	}
	return capabilities, true
}

func requireWritableUserFields(
	capabilities sharing.Capabilities,
	fields ...string,
) error {
	for _, field := range fields {
		for _, readOnly := range capabilities.Graph.Users.ReadOnlyAttributes {
			if strings.EqualFold(strings.TrimSpace(readOnly), field) {
				return fmt.Errorf(
					"server advertises %s as read-only", field,
				)
			}
		}
	}
	return nil
}

func resolveMutationUser(
	ctx context.Context, selected *client, identifier string,
) (graph.DirectoryUser, error) {
	user, err := selected.graphClient().GetUser(ctx, identifier)
	if err != nil {
		return graph.DirectoryUser{}, err
	}
	if strings.TrimSpace(user.ID) == "" {
		return graph.DirectoryUser{}, fmt.Errorf(
			"server returned user %q without a stable ID",
			user.DisplayName,
		)
	}
	return user, nil
}

func resolveMutationGroup(
	ctx context.Context, selected *client, identifier string,
) (graph.DirectoryGroup, error) {
	group, err := selected.graphClient().GetGroup(ctx, identifier)
	if err != nil {
		return graph.DirectoryGroup{}, err
	}
	if strings.TrimSpace(group.ID) == "" {
		return graph.DirectoryGroup{}, fmt.Errorf(
			"server returned group %q without a stable ID",
			group.DisplayName,
		)
	}
	return group, nil
}

func rejectReadOnlyGroup(group graph.DirectoryGroup) error {
	if adminGroupAccess(group) == "read-only" {
		return fmt.Errorf(
			"group %q is managed by a read-only identity backend",
			group.DisplayName,
		)
	}
	return nil
}

func rejectSelfTarget(
	ctx context.Context,
	selected *client,
	target graph.DirectoryUser,
	operation string,
) error {
	current, err := selected.graphClient().GetMe(ctx)
	if err != nil {
		return fmt.Errorf(
			"verify current account before %s: %w", operation, err,
		)
	}
	if current.ID == target.ID {
		return apperror.Wrap(
			apperror.KindConflict, operation,
			fmt.Errorf(
				"refusing to %s the currently authenticated account %q",
				operation, target.Username,
			),
		)
	}
	return nil
}

func adminMutationError(resource string, err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "read-only") ||
		strings.Contains(message, "configured read only") {
		return fmt.Errorf(
			"%s is managed by a read-only identity backend", resource,
		)
	}
	switch protocolStatus(err) {
	case http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return fmt.Errorf(
			"server does not expose the requested %s mutation", resource,
		)
	default:
		return err
	}
}
