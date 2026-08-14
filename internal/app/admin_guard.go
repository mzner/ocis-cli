package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/mzner/ocis-cli/internal/apperror"
)

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

func unavailableAdminList(capability string, err error) error {
	switch protocolStatus(err) {
	case http.StatusNotFound, http.StatusMethodNotAllowed,
		http.StatusNotImplemented:
		return fmt.Errorf(
			"server does not expose %s through LibreGraph: %w",
			capability, err,
		)
	default:
		return err
	}
}
