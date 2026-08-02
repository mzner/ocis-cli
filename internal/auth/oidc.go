// Package auth implements authentication protocols without depending on Cobra
// or application orchestration.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Metadata contains the OIDC endpoints used by the CLI.
type Metadata struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserInfoEndpoint      string `json:"userinfo_endpoint"`
	RegistrationEndpoint  string `json:"registration_endpoint,omitempty"`
}

// OAuthError is an error response returned by an OAuth or OIDC endpoint.
type OAuthError struct {
	Code        string
	Description string
	StatusCode  int
}

func (err *OAuthError) Error() string {
	if err.Description == "" {
		return fmt.Sprintf("OAuth endpoint: %s", err.Code)
	}
	return fmt.Sprintf("OAuth endpoint: %s: %s", err.Code, err.Description)
}

// Token contains an OAuth token response.
type Token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

// UserInfo contains identity claims used to resolve a DAV username.
type UserInfo struct {
	PreferredUsername string `json:"preferred_username"`
	Email             string `json:"email"`
	Subject           string `json:"sub"`
}

// Discover fetches and validates OIDC provider metadata.
func Discover(ctx context.Context, client *http.Client, server string) (Metadata, error) {
	endpoint := strings.TrimRight(server, "/") + "/.well-known/openid-configuration"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Metadata{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return Metadata{}, fmt.Errorf("OIDC discovery from %s: %w", endpoint, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Metadata{}, responseError(response)
	}
	var metadata Metadata
	if err := json.NewDecoder(response.Body).Decode(&metadata); err != nil {
		return Metadata{}, fmt.Errorf("decode OIDC discovery: %w", err)
	}
	if metadata.Issuer == "" || metadata.AuthorizationEndpoint == "" ||
		metadata.TokenEndpoint == "" || metadata.UserInfoEndpoint == "" {
		return Metadata{}, errors.New("OIDC discovery document is missing required endpoints")
	}
	endpoints := []struct {
		name string
		url  string
	}{
		{name: "issuer", url: metadata.Issuer},
		{name: "authorization_endpoint", url: metadata.AuthorizationEndpoint},
		{name: "token_endpoint", url: metadata.TokenEndpoint},
		{name: "userinfo_endpoint", url: metadata.UserInfoEndpoint},
	}
	if metadata.RegistrationEndpoint != "" {
		endpoints = append(endpoints, struct {
			name string
			url  string
		}{
			name: "registration_endpoint", url: metadata.RegistrationEndpoint,
		})
	}
	for _, endpoint := range endpoints {
		if err := validateHTTPSEndpoint(endpoint.name, endpoint.url); err != nil {
			return Metadata{}, err
		}
	}
	return metadata, nil
}

func validateHTTPSEndpoint(name, endpoint string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("OIDC %s is invalid: %w", name, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("OIDC %s must use http or https", name)
	}
	if parsed.Host == "" {
		return fmt.Errorf("OIDC %s must be an absolute URL", name)
	}
	return nil
}

// ValidateTransportSecurity rejects discovered clear-text endpoints unless the
// profile explicitly allows insecure development connections.
func ValidateTransportSecurity(metadata Metadata, allowInsecure bool) error {
	if allowInsecure {
		return nil
	}
	endpoints := []struct {
		name string
		url  string
	}{
		{name: "issuer", url: metadata.Issuer},
		{name: "authorization_endpoint", url: metadata.AuthorizationEndpoint},
		{name: "token_endpoint", url: metadata.TokenEndpoint},
		{name: "userinfo_endpoint", url: metadata.UserInfoEndpoint},
	}
	if metadata.RegistrationEndpoint != "" {
		endpoints = append(endpoints, struct {
			name string
			url  string
		}{
			name: "registration_endpoint", url: metadata.RegistrationEndpoint,
		})
	}
	for _, endpoint := range endpoints {
		parsed, err := url.Parse(endpoint.url)
		if err != nil {
			return fmt.Errorf("OIDC %s is invalid: %w", endpoint.name, err)
		}
		if parsed.Scheme != "https" {
			return fmt.Errorf(
				"OIDC %s must use https; use --insecure only for an explicitly trusted development server",
				endpoint.name,
			)
		}
	}
	return nil
}

// ExchangeCode exchanges an authorization code using PKCE.
func ExchangeCode(ctx context.Context, client *http.Client, endpoint, clientID, secret, code, verifier, redirectURI string) (Token, error) {
	return requestToken(ctx, client, endpoint, clientID, secret, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientID},
		"code_verifier": {verifier},
	})
}

// Refresh refreshes an OAuth access token.
func Refresh(ctx context.Context, client *http.Client, endpoint, clientID, secret, refreshToken string) (Token, error) {
	return requestToken(ctx, client, endpoint, clientID, secret, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {clientID},
	})
}

func requestToken(ctx context.Context, client *http.Client, endpoint, clientID, secret string, form url.Values) (Token, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if secret != "" {
		request.SetBasicAuth(clientID, secret)
	}
	response, err := client.Do(request)
	if err != nil {
		return Token{}, err
	}
	defer func() { _ = response.Body.Close() }()
	var token Token
	if err := json.NewDecoder(response.Body).Decode(&token); err != nil {
		return Token{}, fmt.Errorf("decode token response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || token.Error != "" {
		problem := token.Error
		if problem == "" {
			problem = response.Status
		}
		return Token{}, &OAuthError{
			Code: problem, Description: token.Description,
			StatusCode: response.StatusCode,
		}
	}
	if token.AccessToken == "" {
		return Token{}, errors.New("token endpoint returned no access token")
	}
	return token, nil
}

// FetchUserInfo retrieves identity claims for an access token.
func FetchUserInfo(ctx context.Context, client *http.Client, endpoint, accessToken string) (UserInfo, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return UserInfo{}, err
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := client.Do(request)
	if err != nil {
		return UserInfo{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return UserInfo{}, responseError(response)
	}
	var info UserInfo
	if err := json.NewDecoder(response.Body).Decode(&info); err != nil {
		return UserInfo{}, fmt.Errorf("decode OIDC userinfo: %w", err)
	}
	return info, nil
}

func responseError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(response.StatusCode)
	}
	return fmt.Errorf("%s: %s", response.Status, message)
}
