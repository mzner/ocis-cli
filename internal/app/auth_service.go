package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/mzner/ocis-cli/internal/apperror"
	"github.com/mzner/ocis-cli/internal/auth"
	"github.com/mzner/ocis-cli/internal/httpapi"
	"github.com/mzner/ocis-cli/internal/sharing"
	"golang.org/x/term"
)

func runAuth(ctx context.Context, request AuthRequest, selected string, options RunOptions) error {
	options.Logger.Debug("run authentication operation", "operation", request.Operation)
	s, err := loadStore(options.Dependencies)
	if err != nil {
		return err
	}
	switch request.Operation {
	case AuthSetup:
		profileName := request.Profile
		if profileName == "" {
			profileName = selected
		}
		return setupOIDCClient(ctx, s, profileName, options)
	case AuthLogin:
		server, name, clientID := request.Server, request.Name, request.ClientID
		if request.Profile != "" {
			selected = request.Profile
		}
		if server != "" {
			if err := validateServerURL(server); err != nil {
				return apperror.Wrap(apperror.KindUsage, "login", err)
			}
			if name == "" {
				u, _ := url.Parse(server)
				name = strings.ReplaceAll(u.Hostname(), ".", "-")
				if name == "" {
					name = "ocis"
				}
			}
			if clientID == "" {
				clientID = defaultClientID
			}
			secret := os.Getenv("OCIS_CLIENT_SECRET")
			s.Profiles[name] = profile{
				Server: strings.TrimRight(server, "/"), Insecure: request.Insecure,
				ClientID: clientID, ClientSecret: secret,
			}
			s.Current, selected = name, name
		}
		name, p, err := selectProfile(s, selected)
		if err != nil {
			return err
		}
		if clientID != "" {
			p.ClientID = clientID
			p.ClientSecret = os.Getenv("OCIS_CLIENT_SECRET")
		}
		if request.Insecure {
			p.Insecure = true
		}
		authType := request.Mode
		if authType == "" {
			authType = "oidc"
		}
		switch authType {
		case "oidc":
			if request.ACR != "" && !request.MFA {
				return apperror.Wrap(
					apperror.KindUsage, "login",
					errors.New("--acr requires --mfa"),
				)
			}
			acr := ""
			if request.MFA {
				acr, err = resolveMFAACR(
					ctx, p, request.ACR, options,
				)
				if err != nil {
					return err
				}
			}
			if err := oidcLogin(
				ctx, &p, request.NoBrowser, acr, options,
			); err != nil {
				return explainOIDCLoginError(name, p.ClientID, err)
			}
			p.AuthType, p.Password = "oidc", ""
		case "basic":
			if request.MFA || request.ACR != "" {
				return apperror.Wrap(
					apperror.KindUsage, "login",
					errors.New(
						"MFA step-up requires OIDC authentication",
					),
				)
			}
			if request.Username == "" {
				return apperror.Wrap(
					apperror.KindUsage, "login",
					errors.New("--username is required with --auth basic"),
				)
			}
			password, err := obtainPassword(options)
			if err != nil {
				return err
			}
			p.AuthType, p.Username, p.Password = "basic", request.Username, password
			p.Subject = ""
			p.AccessToken, p.RefreshToken, p.ExpiresAt = "", "", 0
			probe := &client{
				name: name, profile: p, http: httpClientFor(p, options.Timeout),
				store: s, ctx: ctx, retries: options.Retries, logger: options.Logger,
			}
			if _, err := probe.list("/"); err != nil {
				return fmt.Errorf("basic authentication failed: %w", err)
			}
		default:
			return apperror.Wrap(
				apperror.KindUsage, "login",
				fmt.Errorf("unsupported auth mode %q; use oidc or basic", authType),
			)
		}
		clearDefaultSpaceAfterIdentityChange(&p)
		s.Profiles[name], s.Current = p, name
		if err := saveStore(options.Dependencies, s); err != nil {
			return err
		}
		return output(
			options, "authentication",
			map[string]any{
				"authenticated": true, "profile": name, "server": p.Server,
				"username": p.Username, "authType": p.AuthType,
			},
			"Authenticated with %s as %s using %s\n",
			p.Server, p.Username, p.AuthType,
		)
	case AuthStatus:
		profileName := request.Profile
		if profileName == "" {
			profileName = selected
		}
		name, p, err := selectProfile(s, profileName)
		if err != nil {
			return err
		}
		authenticated := p.Password != "" || p.AccessToken != "" || p.RefreshToken != ""
		return output(
			options, "authentication",
			map[string]any{
				"profile": name, "server": p.Server, "username": p.Username,
				"authType": p.AuthType, "authenticated": authenticated,
				"expiresAt": p.ExpiresAt,
			},
			"%s: %s as %s using %s (authenticated: %t)\n",
			name, p.Server, p.Username, p.AuthType, authenticated,
		)
	case AuthLogout:
		profileName := request.Profile
		if profileName == "" {
			profileName = selected
		}
		name, p, err := selectProfile(s, profileName)
		if err != nil {
			return err
		}
		clearAuthenticatedAccount(&p)
		s.Profiles[name] = p
		if err := saveStore(options.Dependencies, s); err != nil {
			return err
		}
		return output(
			options, "authentication",
			map[string]any{"profile": name, "authenticated": false},
			"Logged out from %s\n", name,
		)
	default:
		return apperror.Wrap(
			apperror.KindUsage, "authentication",
			fmt.Errorf("unknown auth command %q", request.Operation),
		)
	}
}

func oidcLogin(
	ctx context.Context,
	p *profile,
	noBrowser bool,
	acr string,
	options RunOptions,
) error {
	httpClient := httpClientFor(*p, options.Timeout)
	meta, err := auth.Discover(ctx, httpClient, p.Server)
	if err != nil {
		return err
	}
	if err := auth.ValidateTransportSecurity(meta, p.Insecure); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("start callback listener: %w", err)
	}
	defer func() { _ = listener.Close() }()
	redirectURI := "http://" + listener.Addr().String() + "/callback"
	state, err := randomURLSafe(32)
	if err != nil {
		return err
	}
	verifier, err := randomURLSafe(64)
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	authURL, err := url.Parse(meta.AuthorizationEndpoint)
	if err != nil {
		return err
	}
	query := authURL.Query()
	query.Set("response_type", "code")
	query.Set("client_id", p.ClientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("scope", "openid offline_access profile email")
	query.Set("state", state)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	if acr != "" {
		query.Set("acr_values", acr)
	}
	authURL.RawQuery = query.Encode()

	type callbackResult struct {
		code string
		err  error
	}
	result := make(chan callbackResult, 1)
	mux := http.NewServeMux()
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	mux.HandleFunc("/callback", func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("state") != state {
			http.Error(writer, "Invalid OAuth state", http.StatusBadRequest)
			result <- callbackResult{err: errors.New("OIDC callback state mismatch")}
			return
		}
		if oauthErr := request.URL.Query().Get("error"); oauthErr != "" {
			http.Error(
				writer, "Authentication failed; return to the terminal.",
				http.StatusBadRequest,
			)
			result <- callbackResult{err: &auth.OAuthError{
				Code: oauthErr, Description: request.URL.Query().Get("error_description"),
				StatusCode: http.StatusBadRequest,
			}}
			return
		}
		code := request.URL.Query().Get("code")
		if code == "" {
			http.Error(writer, "Missing authorization code", http.StatusBadRequest)
			result <- callbackResult{err: errors.New("OIDC callback did not contain a code")}
			return
		}
		_, _ = io.WriteString(
			writer,
			"<!doctype html><title>oCIS CLI</title><h1>Signed in</h1>"+
				"<p>You can close this window and return to the terminal.</p>",
		)
		result <- callbackResult{code: code}
	})
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			select {
			case result <- callbackResult{err: err}:
			default:
			}
		}
	}()
	_, _ = fmt.Fprintln(options.Out, "Open this URL to authenticate:")
	_, _ = fmt.Fprintln(options.Out, authURL.String())
	if !noBrowser {
		if err := openBrowser(authURL.String()); err != nil {
			_, _ = fmt.Fprintln(options.Err, "Could not open browser automatically:", err)
		}
	}
	var callback callbackResult
	select {
	case callback = <-result:
	case <-ctx.Done():
		callback.err = ctx.Err()
	case <-time.After(5 * time.Minute):
		callback.err = errors.New("timed out waiting for browser authentication")
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	if callback.err != nil {
		return callback.err
	}
	token, err := auth.ExchangeCode(
		ctx, httpClient, meta.TokenEndpoint, p.ClientID, p.ClientSecret,
		callback.code, verifier, redirectURI,
	)
	if err != nil {
		return err
	}
	p.Issuer, p.TokenURL, p.UserInfoURL = meta.Issuer, meta.TokenEndpoint, meta.UserInfoEndpoint
	p.AccessToken, p.RefreshToken = token.AccessToken, token.RefreshToken
	expiresIn := time.Duration(token.ExpiresIn) * time.Second
	if token.ExpiresIn <= 0 {
		expiresIn = time.Hour
	}
	p.ExpiresAt = time.Now().Add(expiresIn).Unix()
	info, err := auth.FetchUserInfo(ctx, httpClient, meta.UserInfoEndpoint, token.AccessToken)
	if err != nil {
		return err
	}
	p.Username = firstNonEmpty(info.PreferredUsername, info.Email, info.Subject)
	p.Subject = info.Subject
	if p.Username == "" {
		return errors.New("OIDC userinfo did not return a usable username")
	}
	if p.Subject == "" {
		return errors.New("OIDC userinfo did not return a subject")
	}
	return nil
}

func resolveMFAACR(
	ctx context.Context,
	p profile,
	explicit string,
	options RunOptions,
) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		return strings.TrimSpace(explicit), nil
	}
	httpClient := httpClientFor(p, options.Timeout)
	capabilityClient := sharing.NewClient(httpapi.Config{
		Server: p.Server, Username: p.Username,
		AuthType: p.AuthType, Password: p.Password,
		AccessToken: p.AccessToken, UserAgent: "ocis-cli/" + Version,
		Retries: options.Retries, Logger: options.Logger,
	}, httpClient)
	capabilities, err := capabilityClient.Capabilities(ctx)
	if err != nil {
		return "", fmt.Errorf(
			"discover server MFA authentication level: %w", err,
		)
	}
	if !capabilities.Auth.MFA.Enabled {
		return "", apperror.Wrap(
			apperror.KindUsage, "login",
			errors.New(
				"server does not advertise MFA; omit --mfa or provide --acr explicitly",
			),
		)
	}
	if len(capabilities.Auth.MFA.LevelNames) == 0 ||
		strings.TrimSpace(capabilities.Auth.MFA.LevelNames[0]) == "" {
		return "", fmt.Errorf(
			"server advertises MFA without an authentication level name",
		)
	}
	return strings.TrimSpace(capabilities.Auth.MFA.LevelNames[0]), nil
}

func openBrowser(target string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", target) //nolint:gosec // no shell involved; target is our generated authorization URL
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", target) //nolint:gosec // no shell involved; target is our generated authorization URL
	default:
		command = exec.Command("xdg-open", target) //nolint:gosec // no shell involved; target is our generated authorization URL
	}
	return command.Start()
}

func randomURLSafe(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func obtainPassword(options RunOptions) (string, error) {
	if password := os.Getenv("OCIS_PASSWORD"); password != "" {
		return password, nil
	}
	input := int(os.Stdin.Fd())
	if !term.IsTerminal(input) {
		return "", errors.New("OCIS_PASSWORD is required when no interactive terminal is available")
	}
	_, _ = fmt.Fprint(options.Err, "Password: ")
	value, err := term.ReadPassword(input)
	_, _ = fmt.Fprintln(options.Err)
	if err != nil {
		return "", err
	}
	password := strings.TrimRight(string(value), "\r\n")
	if password == "" {
		return "", errors.New("password cannot be empty")
	}
	return password, nil
}
