package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mzner/ocis-cli/internal/apperror"
	"github.com/mzner/ocis-cli/internal/auth"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

const staticClientRegistration = `- id: github.com/mzner/ocis-cli
  name: oCIS CLI
  trusted: false
  secret: ""
  redirect_uris:
  - http://127.0.0.1
  origins: []
  application_type: native`

func setupOIDCClient(
	ctx context.Context,
	selectedStore *store,
	selectedProfile string,
	options RunOptions,
) error {
	name, current, err := selectProfile(selectedStore, selectedProfile)
	if err != nil {
		return err
	}
	httpClient := httpClientFor(current, options.Timeout)
	metadata, err := auth.Discover(ctx, httpClient, current.Server)
	if err != nil {
		return err
	}
	if err := auth.ValidateTransportSecurity(metadata, current.Insecure); err != nil {
		return err
	}
	if metadata.RegistrationEndpoint == "" {
		current.ClientID = defaultClientID
		current.ClientSecret = ""
		current.Issuer = metadata.Issuer
		current.TokenURL = metadata.TokenEndpoint
		current.UserInfoURL = metadata.UserInfoEndpoint
		clearAuthenticatedAccount(&current)
		selectedStore.Profiles[name] = current
		if err := saveStore(options.Dependencies, selectedStore); err != nil {
			return fmt.Errorf("save static OIDC client configuration: %w", err)
		}
		value := map[string]any{
			"profile": name, "server": current.Server, "dynamic": false,
			"clientId": defaultClientID, "registration": staticClientRegistration,
			"nextCommand": "ocis auth login " + name,
		}
		if options.OutputMode != appoutput.Human {
			return writeOutput(options, "oidc-client-setup", value)
		}
		return output(
			options, "oidc-client-setup", value,
			"Server %s does not advertise dynamic OIDC client registration.\n"+
				"Ask its administrator to add this entry to the embedded IDP configuration:\n\n"+
				"%s\n\n"+
				"Then run: ocis auth login %s\n",
			current.Server, staticClientRegistration, name,
		)
	}
	registered, err := auth.RegisterClient(
		ctx, httpClient, metadata.RegistrationEndpoint,
	)
	if err != nil {
		return fmt.Errorf("register OIDC client: %w", err)
	}
	current.ClientID = registered.ClientID
	current.ClientSecret = registered.ClientSecret
	current.Issuer = metadata.Issuer
	current.TokenURL = metadata.TokenEndpoint
	current.UserInfoURL = metadata.UserInfoEndpoint
	clearAuthenticatedAccount(&current)
	selectedStore.Profiles[name] = current
	if err := saveStore(options.Dependencies, selectedStore); err != nil {
		return fmt.Errorf("save registered OIDC client: %w", err)
	}
	return output(
		options, "oidc-client-setup",
		map[string]any{
			"profile": name, "server": current.Server, "dynamic": true,
			"clientId": registered.ClientID, "clientSecretStored": registered.ClientSecret != "",
			"nextCommand": "ocis auth login " + name,
		},
		"Registered an OIDC client for profile %s.\n"+
			"The client secret, when provided, was saved in the operating-system credential service.\n"+
			"Next run: ocis auth login %s\n",
		name, name,
	)
}

func clearAuthenticatedAccount(selected *profile) {
	selected.AccessToken = ""
	selected.RefreshToken = ""
	selected.Password = ""
	selected.ExpiresAt = 0
	selected.Username = ""
	selected.Subject = ""
	selected.AuthType = ""
	selected.DefaultSpace = ""
	selected.DefaultSpaceOwner = ""
}

func explainOIDCLoginError(profileName, clientID string, err error) error {
	var oauthErr *auth.OAuthError
	if !errors.As(err, &oauthErr) {
		return err
	}
	var actionable error
	switch {
	case oauthErr.Code == "invalid_client" ||
		containsTextFold(oauthErr.Description, "client_secret") ||
		containsTextFold(oauthErr.Description, "unknown client"):
		actionable = fmt.Errorf(
			"OIDC client %q is not accepted by this server; run `ocis auth setup %s`, "+
				"or use --client-id with OCIS_CLIENT_SECRET for an administrator-provisioned client: %w",
			clientID, profileName, err,
		)
	case containsTextFold(oauthErr.Description, "redirect"):
		actionable = fmt.Errorf(
			"the OIDC client does not allow the CLI loopback redirect; run `ocis auth setup %s` "+
				"or update its registered redirect URI to http://127.0.0.1: %w",
			profileName, err,
		)
	case oauthErr.Code == "access_denied":
		actionable = fmt.Errorf(
			"OIDC access was denied; approve access in the browser and retry: %w",
			err,
		)
	default:
		return err
	}
	return apperror.Wrap(apperror.KindAuthentication, "oidc login", actionable)
}

func containsTextFold(value, search string) bool {
	return strings.Contains(strings.ToLower(value), strings.ToLower(search))
}
