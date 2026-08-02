package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// ClientRegistration describes the native OAuth client requested by the CLI.
type ClientRegistration struct {
	RedirectURIs            []string `json:"redirect_uris"`
	ResponseTypes           []string `json:"response_types"`
	GrantTypes              []string `json:"grant_types"`
	ApplicationType         string   `json:"application_type"`
	ClientName              string   `json:"client_name"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

// RegisteredClient contains credentials returned by dynamic client registration.
type RegisteredClient struct {
	ClientID                string `json:"client_id"`
	ClientSecret            string `json:"client_secret,omitempty"`
	TokenEndpointAuthMethod string `json:"token_endpoint_auth_method,omitempty"`
}

// RegisterClient registers an untrusted native client with a loopback redirect.
func RegisterClient(
	ctx context.Context, client *http.Client, endpoint string,
) (RegisteredClient, error) {
	payload, err := json.Marshal(ClientRegistration{
		RedirectURIs:            []string{"http://127.0.0.1"},
		ResponseTypes:           []string{"code"},
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ApplicationType:         "native",
		ClientName:              "oCIS CLI",
		TokenEndpointAuthMethod: "client_secret_basic",
	})
	if err != nil {
		return RegisteredClient{}, fmt.Errorf("encode client registration: %w", err)
	}
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, endpoint, bytes.NewReader(payload),
	)
	if err != nil {
		return RegisteredClient{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return RegisteredClient{}, fmt.Errorf("register OIDC client: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return RegisteredClient{}, responseError(response)
	}
	var registered RegisteredClient
	if err := json.NewDecoder(response.Body).Decode(&registered); err != nil {
		return RegisteredClient{}, fmt.Errorf("decode client registration: %w", err)
	}
	if registered.ClientID == "" {
		return RegisteredClient{}, errors.New("client registration returned no client ID")
	}
	return registered, nil
}
