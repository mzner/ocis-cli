package graph

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// DirectoryUser is the non-sensitive identity data returned by user search.
type DirectoryUser struct {
	ID                string           `json:"id,omitempty"`
	DisplayName       string           `json:"displayName,omitempty"`
	Username          string           `json:"onPremisesSamAccountName,omitempty"`
	Mail              string           `json:"mail,omitempty"`
	UserType          string           `json:"userType,omitempty"`
	AccountEnabled    *bool            `json:"accountEnabled,omitempty"`
	GivenName         string           `json:"givenName,omitempty"`
	Surname           string           `json:"surname,omitempty"`
	PreferredLanguage string           `json:"preferredLanguage,omitempty"`
	Attributes        []string         `json:"attributes,omitempty"`
	Identities        []ObjectIdentity `json:"identities,omitempty"`
}

// ObjectIdentity identifies a user at an identity provider.
type ObjectIdentity struct {
	Issuer           string `json:"issuer,omitempty"`
	IssuerAssignedID string `json:"issuerAssignedId,omitempty"`
}

// DirectoryGroup is the non-sensitive identity data returned by group search.
type DirectoryGroup struct {
	ID          string   `json:"id,omitempty"`
	DisplayName string   `json:"displayName,omitempty"`
	Description string   `json:"description,omitempty"`
	GroupTypes  []string `json:"groupTypes,omitempty"`
}

// DirectorySearchMode controls whether a directory search is treated as
// ordinary text or as an exact server-side LibreGraph search expression.
type DirectorySearchMode uint8

const (
	// DirectorySearchLiteral safely treats the value as one search phrase.
	DirectorySearchLiteral DirectorySearchMode = iota
	// DirectorySearchRaw passes the value to LibreGraph unchanged.
	DirectorySearchRaw
)

// DirectorySearch describes an optional administrative directory search.
type DirectorySearch struct {
	Value string
	Mode  DirectorySearchMode
}

// ListUsers returns users visible through the caller's account-management
// permissions. An empty search requests the server's complete inventory.
func (client *Client) ListUsers(
	ctx context.Context, search DirectorySearch,
) ([]DirectoryUser, error) {
	var result struct {
		Value []DirectoryUser `json:"value"`
	}
	resource, err := directoryListResource("/graph/v1.0/users", search)
	if err != nil {
		return nil, err
	}
	if err := client.doJSON(
		ctx, http.MethodGet, resource,
		nil, nil, &result, "list users",
	); err != nil {
		return nil, err
	}
	return result.Value, nil
}

// GetUser returns one user by the exact username or stable ID accepted by the
// configured oCIS identity backend.
func (client *Client) GetUser(
	ctx context.Context, nameOrID string,
) (DirectoryUser, error) {
	var result DirectoryUser
	if err := client.doJSON(
		ctx, http.MethodGet,
		fmt.Sprintf("/graph/v1.0/users/%s", url.PathEscape(nameOrID)),
		nil, nil, &result, "get user",
	); err != nil {
		return DirectoryUser{}, err
	}
	return result, nil
}

// ListGroups returns groups visible through the caller's account-management
// permissions. An empty search requests the complete group inventory.
func (client *Client) ListGroups(
	ctx context.Context, search DirectorySearch,
) ([]DirectoryGroup, error) {
	var result struct {
		Value []DirectoryGroup `json:"value"`
	}
	resource, err := directoryListResource("/graph/v1.0/groups", search)
	if err != nil {
		return nil, err
	}
	if err := client.doJSON(
		ctx, http.MethodGet, resource,
		nil, nil, &result, "list groups",
	); err != nil {
		return nil, err
	}
	return result.Value, nil
}

// GetGroup returns one group by the exact name or stable ID accepted by the
// configured oCIS identity backend.
func (client *Client) GetGroup(
	ctx context.Context, nameOrID string,
) (DirectoryGroup, error) {
	var result DirectoryGroup
	if err := client.doJSON(
		ctx, http.MethodGet,
		fmt.Sprintf("/graph/v1.0/groups/%s", url.PathEscape(nameOrID)),
		nil, nil, &result, "get group",
	); err != nil {
		return DirectoryGroup{}, err
	}
	return result, nil
}

// ListGroupMembers returns the direct user members of a group. Current oCIS
// does not model nested groups on this endpoint.
func (client *Client) ListGroupMembers(
	ctx context.Context, groupID string,
) ([]DirectoryUser, error) {
	var result []DirectoryUser
	if err := client.doJSON(
		ctx, http.MethodGet,
		fmt.Sprintf(
			"/graph/v1.0/groups/%s/members", url.PathEscape(groupID),
		),
		nil, nil, &result, "list group members",
	); err != nil {
		return nil, err
	}
	return result, nil
}

// SearchUsers searches the oCIS identity directory. The server controls the
// minimum search length and which attributes regular users may receive.
func (client *Client) SearchUsers(
	ctx context.Context, search string,
) ([]DirectoryUser, error) {
	var result struct {
		Value []DirectoryUser `json:"value"`
	}
	if err := client.doJSON(
		ctx, http.MethodGet, searchResource("/graph/v1.0/users", search),
		nil, nil, &result, "search users",
	); err != nil {
		return nil, err
	}
	return result.Value, nil
}

// SearchFederatedUsers searches only previously accepted OCM connections.
// Current oCIS deliberately omits federated users unless this exact userType
// filter is present.
func (client *Client) SearchFederatedUsers(
	ctx context.Context, search string,
) ([]DirectoryUser, error) {
	var result struct {
		Value []DirectoryUser `json:"value"`
	}
	query := url.Values{}
	query.Set("$search", search)
	query.Set("$filter", "userType eq 'Federated'")
	if err := client.doJSON(
		ctx, http.MethodGet, "/graph/v1.0/users?"+query.Encode(),
		nil, nil, &result, "search federated users",
	); err != nil {
		return nil, err
	}
	return result.Value, nil
}

// SearchGroups searches the oCIS identity directory.
func (client *Client) SearchGroups(
	ctx context.Context, search string,
) ([]DirectoryGroup, error) {
	var result struct {
		Value []DirectoryGroup `json:"value"`
	}
	if err := client.doJSON(
		ctx, http.MethodGet, searchResource("/graph/v1.0/groups", search),
		nil, nil, &result, "search groups",
	); err != nil {
		return nil, err
	}
	return result.Value, nil
}

func searchResource(resource string, search string) string {
	query := url.Values{}
	query.Set("$search", search)
	return resource + "?" + query.Encode()
}

func directoryListResource(
	resource string, search DirectorySearch,
) (string, error) {
	query := url.Values{}
	query.Set("$orderby", "displayName asc")
	expression, err := directorySearchExpression(search)
	if err != nil {
		return "", err
	}
	if expression != "" {
		query.Set("$search", expression)
	}
	return resource + "?" + query.Encode(), nil
}

func directorySearchExpression(search DirectorySearch) (string, error) {
	value := strings.TrimSpace(search.Value)
	if value == "" {
		return "", nil
	}
	switch search.Mode {
	case DirectorySearchLiteral:
		// LibreGraph's OData search grammar treats an unquoted hyphen or
		// whitespace as syntax. A quoted phrase keeps ordinary CLI input
		// literal and is the form consumed by current oCIS directory backends.
		if strings.ContainsRune(value, '"') {
			return "", errors.New(
				`literal directory search cannot contain a double quote; use a raw search expression`,
			)
		}
		return `"` + value + `"`, nil
	case DirectorySearchRaw:
		return value, nil
	default:
		return "", fmt.Errorf("unknown directory search mode %d", search.Mode)
	}
}
