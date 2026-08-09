package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/mzner/ocis-cli/internal/apperror"
	"github.com/mzner/ocis-cli/internal/auth"
	appconfig "github.com/mzner/ocis-cli/internal/config"
	"github.com/mzner/ocis-cli/internal/credentials"
	"github.com/mzner/ocis-cli/internal/graph"
	"github.com/mzner/ocis-cli/internal/httpapi"
	"github.com/mzner/ocis-cli/internal/logging"
	appoutput "github.com/mzner/ocis-cli/internal/output"
	"github.com/mzner/ocis-cli/internal/search"
	"github.com/mzner/ocis-cli/internal/sharing"
	"github.com/mzner/ocis-cli/internal/trash"
	"github.com/mzner/ocis-cli/internal/versions"
	"github.com/mzner/ocis-cli/internal/webdav"
)

const defaultClientID = "github.com/mzner/ocis-cli"

type store = appconfig.Store
type profile = appconfig.Profile
type item = webdav.Item

type client struct {
	name         string
	profile      profile
	http         *http.Client
	store        *store
	ctx          context.Context
	dav          *webdav.Client
	graph        *graph.Client
	search       *search.Client
	sharing      *sharing.Client
	recycle      *trash.Client
	versions     *versions.Client
	space        *graph.Drive
	retries      int
	logger       logging.Logger
	dependencies Dependencies
}

func (client *client) apiConfig() httpapi.Config {
	return httpapi.Config{
		Server: client.profile.Server, Username: client.profile.Username,
		AuthType: client.profile.AuthType, Password: client.profile.Password,
		AccessToken: client.profile.AccessToken, UserAgent: "ocis-cli/" + Version,
		Retries: client.retries, Logger: client.logger,
	}
}

func (client *client) graphClient() *graph.Client {
	if client.graph == nil {
		client.graph = graph.NewClient(client.apiConfig(), client.http)
	}
	return client.graph
}

func (client *client) sharingClient() *sharing.Client {
	if client.sharing == nil {
		client.sharing = sharing.NewClient(client.apiConfig(), client.http)
	}
	return client.sharing
}

func (client *client) searchClient() *search.Client {
	if client.search == nil {
		client.search = search.NewClient(client.apiConfig(), client.http)
	}
	return client.search
}

func newClientWithOptions(
	ctx context.Context, selected string, options RunOptions,
) (*client, error) {
	s, err := loadStore(options.Dependencies)
	if err != nil {
		return nil, err
	}
	name, selectedProfile, err := selectProfile(s, selected)
	if err != nil {
		return nil, err
	}
	// Validated here, not only where a profile is created: a release before the
	// https requirement stored cleartext profiles without an opt-in, and such a
	// profile would otherwise still receive its saved password or token and send
	// it over the network. Checked before any credential is applied or refreshed,
	// and never during config.Load, so server list, status, logout, and
	// server remove stay usable for repairing the profile.
	if err := validateProfileServerURL(name, selectedProfile); err != nil {
		return nil, err
	}
	if token := os.Getenv("OCIS_ACCESS_TOKEN"); token != "" {
		selectedProfile.AuthType = "oidc"
		selectedProfile.AccessToken = token
		selectedProfile.ExpiresAt = time.Now().Add(time.Hour).Unix()
	}
	if selectedProfile.AuthType == "basic" && selectedProfile.Password == "" {
		return nil, fmt.Errorf(
			"%s has no saved password; run ocis auth login %s --auth basic --username USER",
			name, name,
		)
	}
	if selectedProfile.AuthType != "basic" &&
		selectedProfile.AccessToken == "" && selectedProfile.RefreshToken == "" {
		return nil, fmt.Errorf(
			"%s is not authenticated; run ocis auth login %s", name, name,
		)
	}
	client := &client{
		name: name, profile: selectedProfile,
		http:  httpClientFor(selectedProfile, options.Timeout),
		store: s, ctx: ctx, retries: options.Retries, logger: options.Logger,
		dependencies: options.Dependencies,
	}
	if selectedProfile.AuthType != "basic" &&
		selectedProfile.RefreshToken != "" &&
		time.Now().Add(30*time.Second).Unix() >= selectedProfile.ExpiresAt {
		token, err := auth.Refresh(
			ctx, client.http, selectedProfile.TokenURL, selectedProfile.ClientID,
			selectedProfile.ClientSecret, selectedProfile.RefreshToken,
		)
		if err != nil {
			return nil, fmt.Errorf("refresh login: %w", err)
		}
		client.profile.AccessToken = token.AccessToken
		if token.RefreshToken != "" {
			client.profile.RefreshToken = token.RefreshToken
		}
		client.profile.ExpiresAt = time.Now().
			Add(time.Duration(token.ExpiresIn) * time.Second).Unix()
		s.Profiles[name] = client.profile
		if err := saveStore(options.Dependencies, s); err != nil {
			return nil, err
		}
	}
	return client, nil
}

// maxRedirects matches the limit Go's default redirect policy applies. It is
// restated here because replacing that policy also replaces its limit.
const maxRedirects = 10

func httpClientFor(selectedProfile profile, timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if selectedProfile.Insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit per-profile development option
	}
	return &http.Client{
		Transport:     transport,
		Timeout:       timeout,
		CheckRedirect: refuseCleartextRedirect(selectedProfile.Insecure),
	}
}

// refuseCleartextRedirect returns a redirect policy that refuses to downgrade a
// request to cleartext. Validating the base URL is not enough on its own: Go
// decides whether to forward Authorization across a redirect by comparing hosts
// alone, so an https endpoint redirecting to http:// on the same host would
// carry the password or bearer token over the network in the clear. Only the
// explicit insecure opt-in permits it.
func refuseCleartextRedirect(insecure bool) func(*http.Request, []*http.Request) error {
	return func(request *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		if insecure || request.URL.Scheme == "https" {
			return nil
		}
		// Only the scheme and host are named: a redirect target can carry
		// credentials or other sensitive values in its path and query.
		return fmt.Errorf(
			"refusing to follow a redirect from https to %s://%s, "+
				"which would send credentials over an unencrypted connection; "+
				"pass --insecure to allow it for an explicitly trusted "+
				"development server",
			request.URL.Scheme, request.URL.Host,
		)
	}
}

func selectProfile(s *store, selected string) (string, profile, error) {
	if selected == "" {
		selected = s.Current
	}
	if selected == "" {
		return "", profile{}, apperror.Wrap(
			apperror.KindUsage, "profile",
			errors.New("no server configured; run ocis auth login --server URL"),
		)
	}
	selectedProfile, ok := s.Profiles[selected]
	if !ok {
		return "", profile{}, apperror.Wrap(
			apperror.KindUsage, "profile",
			fmt.Errorf("unknown server profile %q", selected),
		)
	}
	return selected, selectedProfile, nil
}

func loadStore(dependencies Dependencies) (*store, error) {
	s, err := dependencies.Config.Load()
	if err != nil {
		return nil, err
	}
	for name, selectedProfile := range s.Profiles {
		secret, err := dependencies.Credentials.Get(name)
		switch {
		case err == nil:
			applyProfileSecret(&selectedProfile, secret)
		case errors.Is(err, credentials.ErrNotFound):
			// A configured profile may not be authenticated yet.
		case err != nil:
			return nil, err
		}
		s.Profiles[name] = selectedProfile
	}
	return s, nil
}

func saveStore(dependencies Dependencies, s *store) error {
	for name, selectedProfile := range s.Profiles {
		if err := dependencies.Credentials.Set(
			name, profileSecret(selectedProfile),
		); err != nil {
			return err
		}
	}
	return dependencies.Config.Save(s)
}

func profileSecret(selectedProfile profile) credentials.Secret {
	return credentials.Secret{
		Password:     selectedProfile.Password,
		ClientSecret: selectedProfile.ClientSecret,
		AccessToken:  selectedProfile.AccessToken,
		RefreshToken: selectedProfile.RefreshToken,
	}
}

func applyProfileSecret(selectedProfile *profile, secret credentials.Secret) {
	selectedProfile.Password = secret.Password
	selectedProfile.ClientSecret = secret.ClientSecret
	selectedProfile.AccessToken = secret.AccessToken
	selectedProfile.RefreshToken = secret.RefreshToken
}

// validateServerURL rejects a cleartext server URL unless the caller passed
// --insecure, which is the same opt-in that already permits cleartext OIDC
// endpoints and unverified certificates for a trusted development server.
func validateServerURL(server string, insecure bool) error {
	if insecure {
		return appconfig.ValidateInsecureServerURL(server)
	}
	return appconfig.ValidateServerURL(server)
}

// validateProfileServerURL rejects a persisted profile whose stored URL would
// carry credentials in the clear. The error names the profile and how to repair
// it, because unlike a rejected command-line URL the user did not just type it.
func validateProfileServerURL(name string, selectedProfile profile) error {
	if err := validateServerURL(
		selectedProfile.Server, selectedProfile.Insecure,
	); err != nil {
		return apperror.Wrap(
			apperror.KindUsage, "profile "+name,
			fmt.Errorf(
				"%w; update it with ocis server add %s https://... or, for an "+
					"explicitly trusted development server, re-add it with --insecure",
				err, name,
			),
		)
	}
	return nil
}

func output(
	options RunOptions, kind string, value any, format string, args ...any,
) error {
	return (appoutput.Renderer{
		Writer: options.Out, Mode: options.OutputMode, Type: kind,
	}).Write(value, format, args...)
}

func writeOutput(options RunOptions, kind string, value any) error {
	return (appoutput.Renderer{
		Writer: options.Out, Mode: options.OutputMode, Type: kind,
	}).Write(value, "")
}
