// Package federation implements the authenticated ScienceMesh endpoints used
// to establish and manage oCIS Open Cloud Mesh connections.
package federation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/mzner/ocis-cli/internal/httpapi"
)

const maxResponseBytes = 8 << 20

// Invitation is a short-lived token another oCIS user can accept to establish
// a federated connection.
type Invitation struct {
	Token       string `json:"token"`
	Description string `json:"description,omitempty"`
	Expiration  int64  `json:"expiration,omitempty"`
	InviteLink  string `json:"inviteLink,omitempty"`
}

// Connection identifies a remote user who has accepted a federation invite.
type Connection struct {
	DisplayName string `json:"displayName"`
	Provider    string `json:"provider"`
	UserID      string `json:"userId"`
	Mail        string `json:"mail,omitempty"`
}

// CreateInvitationRequest describes a new federation invitation.
type CreateInvitationRequest struct {
	Recipient   string `json:"recipient,omitempty"`
	Description string `json:"description,omitempty"`
}

// AcceptInvitationRequest identifies the invitation and issuing provider.
type AcceptInvitationRequest struct {
	Token          string `json:"token"`
	ProviderDomain string `json:"providerDomain"`
}

// DeleteConnectionRequest identifies one accepted remote user.
type DeleteConnectionRequest struct {
	Provider string `json:"idp"`
	UserID   string `json:"user_id"`
}

// Client manages ScienceMesh federation invitations and connections.
type Client struct {
	api *httpapi.Client
}

// NewClient constructs a federation client.
func NewClient(config httpapi.Config, httpClient *http.Client) *Client {
	return &Client{api: httpapi.NewClient(config, httpClient)}
}

// CreateInvitation generates a short-lived federation invitation.
func (client *Client) CreateInvitation(
	ctx context.Context, request CreateInvitationRequest,
) (Invitation, error) {
	var invitation rawInvitation
	if err := client.doJSON(
		ctx, http.MethodPost, "/sciencemesh/generate-invite", request,
		&invitation, "create federation invitation",
	); err != nil {
		return Invitation{}, err
	}
	return invitation.invitation(), nil
}

// ListInvitations returns active invitations created by the current user.
func (client *Client) ListInvitations(
	ctx context.Context,
) ([]Invitation, error) {
	var values []rawInvitation
	if err := client.doJSON(
		ctx, http.MethodGet, "/sciencemesh/list-invite", nil,
		&values, "list federation invitations",
	); err != nil {
		return nil, err
	}
	result := make([]Invitation, 0, len(values))
	for _, value := range values {
		result = append(result, value.invitation())
	}
	return result, nil
}

// AcceptInvitation establishes a connection with the invitation issuer.
func (client *Client) AcceptInvitation(
	ctx context.Context, request AcceptInvitationRequest,
) error {
	if strings.TrimSpace(request.Token) == "" ||
		strings.TrimSpace(request.ProviderDomain) == "" {
		return fmt.Errorf("invitation token and provider domain must not be empty")
	}
	return client.doJSON(
		ctx, http.MethodPost, "/sciencemesh/accept-invite", request,
		nil, "accept federation invitation",
	)
}

// ListConnections returns remote users connected to the current user.
func (client *Client) ListConnections(
	ctx context.Context,
) ([]Connection, error) {
	var values []rawConnection
	if err := client.doJSON(
		ctx, http.MethodGet, "/sciencemesh/find-accepted-users", nil,
		&values, "list federation connections",
	); err != nil {
		return nil, err
	}
	result := make([]Connection, 0, len(values))
	for _, value := range values {
		result = append(result, value.connection())
	}
	return result, nil
}

// DeleteConnection removes one accepted remote-user connection.
func (client *Client) DeleteConnection(
	ctx context.Context, request DeleteConnectionRequest,
) error {
	if strings.TrimSpace(request.Provider) == "" ||
		strings.TrimSpace(request.UserID) == "" {
		return fmt.Errorf("connection provider and user ID must not be empty")
	}
	return client.doJSON(
		ctx, http.MethodDelete, "/sciencemesh/delete-accepted-user", request,
		nil, "delete federation connection",
	)
}

func (client *Client) doJSON(
	ctx context.Context,
	method string,
	resource string,
	payload any,
	result any,
	operation string,
) error {
	var body []byte
	var err error
	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode %s request: %w", operation, err)
		}
	}
	headers := http.Header{"Accept": {"application/json"}}
	if payload != nil {
		headers.Set("Content-Type", "application/json")
	}
	response, err := client.api.Do(ctx, method, resource, body, headers)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return httpapi.ResponseError(response)
	}
	if result == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
		return nil
	}
	if err := json.NewDecoder(
		io.LimitReader(response.Body, maxResponseBytes),
	).Decode(result); err != nil {
		return fmt.Errorf("decode %s response: %w", operation, err)
	}
	return nil
}

type rawInvitation struct {
	Token       string `json:"token"`
	Description string `json:"description,omitempty"`
	Expiration  int64  `json:"expiration,omitempty"`
	InviteLink  string `json:"invite_link,omitempty"`
}

func (value rawInvitation) invitation() Invitation {
	return Invitation(value)
}

type rawConnection struct {
	DisplayName string `json:"display_name"`
	Provider    string `json:"idp"`
	UserID      string `json:"user_id"`
	Mail        string `json:"mail"`
}

func (value rawConnection) connection() Connection {
	return Connection(value)
}
