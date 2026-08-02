package harness

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
)

type helloRequest struct {
	State       string `json:"state"`
	Flow        string `json:"flow"`
	Scope       string `json:"scope"`
	Prompt      string `json:"prompt"`
	ClientID    string `json:"client_id"`
	RedirectURI string `json:"redirect_uri"`
	IDTokenHint string `json:"id_token_hint"`
	MaxAge      string `json:"max_age"`
}

type helloResponse struct {
	Success     bool   `json:"success"`
	Next        string `json:"next"`
	ContinueURI string `json:"continue_uri"`
}

type logonResponse struct {
	Success bool          `json:"success"`
	Hello   helloResponse `json:"hello"`
}

type consentRequest struct {
	State       string `json:"state"`
	Allow       bool   `json:"allow"`
	Scope       string `json:"scope"`
	ClientID    string `json:"client_id"`
	RedirectURI string `json:"redirect_uri"`
	Ref         string `json:"ref"`
	Nonce       string `json:"flow_nonce"`
}

// OIDCBrowser drives the embedded IDP login and consent JSON endpoints.
type OIDCBrowser struct {
	client *http.Client
}

// NewOIDCBrowser constructs a cookie-aware headless OIDC flow driver.
func NewOIDCBrowser(insecure bool) (*OIDCBrowser, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if insecure {
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // explicit disposable integration server setting
		}
	}
	return &OIDCBrowser{client: &http.Client{
		Transport: transport, Jar: jar,
	}}, nil
}

// Authenticate signs in through the embedded IDP and follows the authorization
// redirect into the CLI's loopback callback.
func (browser *OIDCBrowser) Authenticate(
	ctx context.Context, authorizationURL, username, password string,
) error {
	loginURL, err := browser.open(ctx, authorizationURL)
	if err != nil {
		return fmt.Errorf("open OIDC authorization URL: %w", err)
	}
	hello := helloFromURL(loginURL)
	logonEndpoint := endpoint(loginURL, "/signin/v1/identifier/_/logon")
	var logon logonResponse
	if err := browser.postJSON(ctx, loginURL, logonEndpoint, map[string]any{
		"state": hello.State, "params": []string{username, password, "1"},
		"hello": hello,
	}, &logon); err != nil {
		return fmt.Errorf("OIDC logon: %w", err)
	}
	if !logon.Success {
		return errors.New("OIDC logon rejected the supplied account")
	}
	if logon.Hello.Next == "consent" {
		return browser.consent(ctx, loginURL)
	}
	if logon.Hello.ContinueURI == "" {
		return errors.New("OIDC logon returned no continuation URI")
	}
	return browser.continueAuthorization(
		ctx, loginURL, logon.Hello.ContinueURI, "", true,
	)
}

func (browser *OIDCBrowser) consent(
	ctx context.Context, loginURL *url.URL,
) error {
	consentURL := cloneURL(loginURL)
	consentURL.Path = "/signin/v1/consent"
	query := consentURL.Query()
	query.Set("prompt", "consent")
	consentURL.RawQuery = query.Encode()
	opened, err := browser.open(ctx, consentURL.String())
	if err != nil {
		return fmt.Errorf("open OIDC consent page: %w", err)
	}
	consentHello := helloFromURL(opened)
	helloEndpoint := endpoint(opened, "/signin/v1/identifier/_/hello")
	var current helloResponse
	if err := browser.postJSON(
		ctx, opened, helloEndpoint, consentHello, &current,
	); err != nil {
		return fmt.Errorf("load OIDC consent: %w", err)
	}
	if !current.Success || current.ContinueURI == "" {
		return errors.New("OIDC consent returned no continuation URI")
	}
	konnectState, err := randomHex(16)
	if err != nil {
		return err
	}
	scope := consentHello.Scope
	if scope == "" {
		scope = "openid"
	}
	consentEndpoint := endpoint(opened, "/signin/v1/identifier/_/consent")
	var accepted struct {
		Success bool `json:"success"`
	}
	if err := browser.postJSON(ctx, opened, consentEndpoint, consentRequest{
		State: konnectState, Allow: true, Scope: scope,
		ClientID: consentHello.ClientID, RedirectURI: consentHello.RedirectURI,
		Ref: consentHello.State, Nonce: opened.Query().Get("nonce"),
	}, &accepted); err != nil {
		return fmt.Errorf("approve OIDC consent: %w", err)
	}
	if !accepted.Success {
		return errors.New("OIDC consent was not accepted")
	}
	return browser.continueAuthorization(
		ctx, opened, current.ContinueURI, konnectState, true,
	)
}

func (browser *OIDCBrowser) continueAuthorization(
	ctx context.Context,
	source *url.URL,
	continuation string,
	konnectState string,
	stripConsent bool,
) error {
	target, err := url.Parse(continuation)
	if err != nil {
		return fmt.Errorf("parse OIDC continuation URI: %w", err)
	}
	query := target.Query()
	for name, values := range source.Query() {
		if query.Has(name) {
			continue
		}
		for _, value := range values {
			query.Add(name, value)
		}
	}
	if stripConsent {
		var prompts []string
		for _, prompt := range strings.Fields(query.Get("prompt")) {
			if prompt != "select_account" && prompt != "consent" {
				prompts = append(prompts, prompt)
			}
		}
		if len(prompts) == 0 {
			query.Del("prompt")
		} else {
			query.Set("prompt", strings.Join(prompts, " "))
		}
	}
	if konnectState != "" {
		query.Set("konnect", konnectState)
	}
	target.RawQuery = query.Encode()
	_, err = browser.open(ctx, target.String())
	if err != nil {
		return fmt.Errorf("complete OIDC authorization: %w", err)
	}
	return nil
}

func (browser *OIDCBrowser) open(
	ctx context.Context, target string,
) (*url.URL, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	response, err := browser.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 400 {
		return nil, fmt.Errorf("unexpected HTTP status %s", response.Status)
	}
	return cloneURL(response.Request.URL), nil
}

func (browser *OIDCBrowser) postJSON(
	ctx context.Context,
	referer *url.URL,
	target string,
	value any,
	result any,
) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, target, bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Kopano-Konnect-XSRF", "1")
	request.Header.Set("Origin", referer.Scheme+"://"+referer.Host)
	request.Header.Set("Referer", referer.String())
	response, err := browser.client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf(
			"unexpected HTTP status %s: %s",
			response.Status, strings.TrimSpace(string(data)),
		)
	}
	if response.StatusCode == http.StatusNoContent || result == nil {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func helloFromURL(value *url.URL) helloRequest {
	query := value.Query()
	flow := query.Get("flow")
	if flow == "" {
		flow = "oidc"
	}
	return helloRequest{
		State: query.Get("state"), Flow: flow, Scope: query.Get("scope"),
		Prompt: query.Get("prompt"), ClientID: query.Get("client_id"),
		RedirectURI: query.Get("redirect_uri"),
		IDTokenHint: query.Get("id_token_hint"), MaxAge: query.Get("max_age"),
	}
}

func endpoint(base *url.URL, path string) string {
	value := &url.URL{Scheme: base.Scheme, Host: base.Host, Path: path}
	return value.String()
}

func cloneURL(value *url.URL) *url.URL {
	cloned := *value
	return &cloned
}

func randomHex(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
