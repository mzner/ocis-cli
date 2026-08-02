package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Identity identifies a user or group in a permission.
type Identity struct {
	ID          string `json:"id,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	UserType    string `json:"@libre.graph.userType,omitempty"`
}

// IdentitySet contains the subject of a permission.
type IdentitySet struct {
	User  *Identity `json:"user,omitempty"`
	Group *Identity `json:"group,omitempty"`
}

// Permission describes one membership or share permission.
type Permission struct {
	ID                 string       `json:"id,omitempty"`
	HasPassword        bool         `json:"hasPassword,omitempty"`
	ExpirationDateTime *time.Time   `json:"expirationDateTime,omitempty"`
	GrantedToV2        *IdentitySet `json:"grantedToV2,omitempty"`
	Link               *SharingLink `json:"link,omitempty"`
	Roles              []string     `json:"roles,omitempty"`
	AllowedActions     []string     `json:"@libre.graph.permissions.actions,omitempty"`
}

// SharingLink contains the public-link facet of a permission.
type SharingLink struct {
	Type        string `json:"type,omitempty"`
	WebURL      string `json:"webUrl,omitempty"`
	DisplayName string `json:"@libre.graph.displayName,omitempty"`
}

// RoleDefinition describes a role offered by the server for a resource.
type RoleDefinition struct {
	ID          string `json:"id,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description,omitempty"`
}

// Permissions contains memberships and caller-specific capabilities.
type Permissions struct {
	AllowedRoles   []RoleDefinition `json:"@libre.graph.permissions.roles.allowedValues,omitempty"`
	AllowedActions []string         `json:"@libre.graph.permissions.actions.allowedValues,omitempty"`
	Value          []Permission     `json:"value,omitempty"`
}

// Recipient identifies a user or group being invited.
type Recipient struct {
	ObjectID string `json:"objectId"`
	Type     string `json:"@libre.graph.recipient.type"`
}

// InviteRequest describes a Space membership invitation.
type InviteRequest struct {
	Recipients []Recipient `json:"recipients"`
	Roles      []string    `json:"roles"`
}

// PermissionUpdateRequest changes the role of an existing member.
type PermissionUpdateRequest struct {
	Roles []string `json:"roles"`
}

// LinkPermissionUpdateRequest changes selected public-link properties.
// Expiration is omitted unless SetExpiration or ClearExpiration is called.
type LinkPermissionUpdateRequest struct {
	Link          *SharingLinkUpdate
	expirationSet bool
	expiration    *time.Time
}

// SharingLinkUpdate changes selected fields in a public-link facet.
type SharingLinkUpdate struct {
	Type        *string `json:"type,omitempty"`
	DisplayName *string `json:"@libre.graph.displayName,omitempty"`
}

// SetExpiration changes the public-link expiration.
func (request *LinkPermissionUpdateRequest) SetExpiration(value time.Time) {
	request.expirationSet = true
	request.expiration = &value
}

// ClearExpiration removes the public-link expiration.
func (request *LinkPermissionUpdateRequest) ClearExpiration() {
	request.expirationSet = true
	request.expiration = nil
}

// Empty reports whether no public-link property was selected.
func (request LinkPermissionUpdateRequest) Empty() bool {
	return request.Link == nil && !request.expirationSet
}

// MarshalJSON preserves the distinction between an omitted expiration and an
// explicit null, which LibreGraph uses to remove an expiration.
func (request LinkPermissionUpdateRequest) MarshalJSON() ([]byte, error) {
	payload := make(map[string]any, 2)
	if request.Link != nil {
		payload["link"] = request.Link
	}
	if request.expirationSet {
		payload["expirationDateTime"] = request.expiration
	}
	return json.Marshal(payload)
}

// Me describes the authenticated Graph user.
type Me struct {
	ID          string `json:"id,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	Username    string `json:"onPremisesSamAccountName,omitempty"`
	Mail        string `json:"mail,omitempty"`
}

// GetMe returns the authenticated Graph identity.
func (client *Client) GetMe(ctx context.Context) (Me, error) {
	var current Me
	if err := client.doJSON(
		ctx, http.MethodGet, "/graph/v1.0/me", nil, nil,
		&current, "get current user",
	); err != nil {
		return Me{}, err
	}
	return current, nil
}

// ListSpacePermissions lists Space members and caller-specific allowed values.
func (client *Client) ListSpacePermissions(
	ctx context.Context, driveID string,
) (Permissions, error) {
	var permissions Permissions
	if err := client.doJSON(
		ctx, http.MethodGet, permissionsResource(driveID), nil, nil,
		&permissions, "list space permissions",
	); err != nil {
		return Permissions{}, err
	}
	return permissions, nil
}

// AddSpaceMember grants a user or group access to a project space.
func (client *Client) AddSpaceMember(
	ctx context.Context, driveID string, request InviteRequest,
) (Permission, error) {
	var result struct {
		Value []Permission `json:"value"`
	}
	resource := fmt.Sprintf(
		"/graph/v1beta1/drives/%s/root/invite", url.PathEscape(driveID),
	)
	if err := client.doJSON(
		ctx, http.MethodPost, resource, request, nil,
		&result, "add space member",
	); err != nil {
		return Permission{}, err
	}
	if len(result.Value) != 1 {
		return Permission{}, fmt.Errorf(
			"decode add space member response: expected one permission, received %d",
			len(result.Value),
		)
	}
	return result.Value[0], nil
}

// UpdateSpaceMember changes a member's Space role.
func (client *Client) UpdateSpaceMember(
	ctx context.Context,
	driveID string,
	permissionID string,
	request PermissionUpdateRequest,
) (Permission, error) {
	var permission Permission
	if err := client.doJSON(
		ctx, http.MethodPatch,
		permissionResource(driveID, permissionID), request, nil,
		&permission, "update space member",
	); err != nil {
		return Permission{}, err
	}
	return permission, nil
}

// RemoveSpaceMember removes one Space permission.
func (client *Client) RemoveSpaceMember(
	ctx context.Context, driveID string, permissionID string,
) error {
	return client.doJSON(
		ctx, http.MethodDelete, permissionResource(driveID, permissionID),
		nil, nil, nil, "remove space member",
	)
}

func permissionsResource(driveID string) string {
	return fmt.Sprintf(
		"/graph/v1beta1/drives/%s/root/permissions", url.PathEscape(driveID),
	)
}

func permissionResource(driveID, permissionID string) string {
	return fmt.Sprintf(
		"%s/%s", permissionsResource(driveID), url.PathEscape(permissionID),
	)
}
