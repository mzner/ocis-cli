package graph

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// PasswordProfile contains password material accepted by the user API.
type PasswordProfile struct {
	Password string `json:"password"`
}

// CreateUserRequest contains writable fields accepted by current oCIS.
type CreateUserRequest struct {
	Username       string           `json:"onPremisesSamAccountName"`
	DisplayName    string           `json:"displayName"`
	Mail           string           `json:"mail"`
	GivenName      string           `json:"givenName,omitempty"`
	Surname        string           `json:"surname,omitempty"`
	AccountEnabled *bool            `json:"accountEnabled,omitempty"`
	Password       *PasswordProfile `json:"passwordProfile,omitempty"`
}

// UpdateUserRequest contains explicitly selected writable user fields.
type UpdateUserRequest struct {
	Username       *string          `json:"onPremisesSamAccountName,omitempty"`
	DisplayName    *string          `json:"displayName,omitempty"`
	Mail           *string          `json:"mail,omitempty"`
	GivenName      *string          `json:"givenName,omitempty"`
	Surname        *string          `json:"surname,omitempty"`
	AccountEnabled *bool            `json:"accountEnabled,omitempty"`
	Password       *PasswordProfile `json:"passwordProfile,omitempty"`
}

// CreateGroupRequest contains the writable group creation fields.
type CreateGroupRequest struct {
	DisplayName string `json:"displayName"`
}

// UpdateGroupRequest contains explicitly selected writable group fields.
type UpdateGroupRequest struct {
	DisplayName *string `json:"displayName,omitempty"`
}

// Application exposes the server-configured role application.
type Application struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"displayName,omitempty"`
	AppRoles    []AppRole `json:"appRoles,omitempty"`
}

// AppRole is one server-advertised administrative role.
type AppRole struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName,omitempty"`
}

// AppRoleAssignment assigns one advertised role to a user.
type AppRoleAssignment struct {
	ID                  string `json:"id,omitempty"`
	AppRoleID           string `json:"appRoleId"`
	PrincipalID         string `json:"principalId"`
	PrincipalType       string `json:"principalType,omitempty"`
	ResourceID          string `json:"resourceId"`
	ResourceDisplayName string `json:"resourceDisplayName,omitempty"`
}

// CheckAdminMFA uses the oCIS user inventory guard to verify both full
// account-management permission and server-side MFA state. The result is
// intentionally discarded.
func (client *Client) CheckAdminMFA(ctx context.Context) error {
	var result struct {
		Value []DirectoryUser `json:"value"`
	}
	return client.doJSON(
		ctx, http.MethodGet, "/graph/v1.0/users?$top=1",
		nil, nil, &result, "verify administrator MFA",
	)
}

// CheckMFA uses an MFA-protected Graph inventory route without assuming the
// caller also has account-administration permission. Space authorization is a
// separate oCIS permission and remains enforced by each Space endpoint.
func (client *Client) CheckMFA(ctx context.Context) error {
	var result struct {
		Value []Drive `json:"value"`
	}
	return client.doJSON(
		ctx, http.MethodGet, "/graph/v1.0/drives?$top=1",
		nil, nil, &result, "verify MFA",
	)
}

// CreateUser creates a user through LibreGraph.
func (client *Client) CreateUser(
	ctx context.Context, request CreateUserRequest,
) (DirectoryUser, error) {
	var result DirectoryUser
	if err := client.doJSON(
		ctx, http.MethodPost, "/graph/v1.0/users",
		request, nil, &result, "create user",
	); err != nil {
		return DirectoryUser{}, err
	}
	return result, nil
}

// UpdateUser updates a user by stable ID.
func (client *Client) UpdateUser(
	ctx context.Context, userID string, request UpdateUserRequest,
) (DirectoryUser, error) {
	var result DirectoryUser
	if err := client.doJSON(
		ctx, http.MethodPatch, userResource(userID),
		request, nil, &result, "update user",
	); err != nil {
		return DirectoryUser{}, err
	}
	return result, nil
}

// DeleteUser permanently deletes a user by stable ID.
func (client *Client) DeleteUser(ctx context.Context, userID string) error {
	return client.doJSON(
		ctx, http.MethodDelete, userResource(userID),
		nil, nil, nil, "delete user",
	)
}

// CreateGroup creates a group through LibreGraph.
func (client *Client) CreateGroup(
	ctx context.Context, request CreateGroupRequest,
) (DirectoryGroup, error) {
	var result DirectoryGroup
	if err := client.doJSON(
		ctx, http.MethodPost, "/graph/v1.0/groups",
		request, nil, &result, "create group",
	); err != nil {
		return DirectoryGroup{}, err
	}
	return result, nil
}

// UpdateGroup updates a group by stable ID.
func (client *Client) UpdateGroup(
	ctx context.Context, groupID string, request UpdateGroupRequest,
) error {
	return client.doJSON(
		ctx, http.MethodPatch, groupResource(groupID),
		request, nil, nil, "update group",
	)
}

// DeleteGroup permanently deletes a group by stable ID.
func (client *Client) DeleteGroup(ctx context.Context, groupID string) error {
	return client.doJSON(
		ctx, http.MethodDelete, groupResource(groupID),
		nil, nil, nil, "delete group",
	)
}

// AddGroupMember adds one direct user member by stable IDs.
func (client *Client) AddGroupMember(
	ctx context.Context, groupID, userID string,
) error {
	payload := struct {
		ODataID string `json:"@odata.id"`
	}{
		ODataID: fmt.Sprintf(
			"%s/graph/v1.0/users/%s",
			client.api.Server(), url.PathEscape(userID),
		),
	}
	return client.doJSON(
		ctx, http.MethodPost, groupResource(groupID)+"/members/$ref",
		payload, nil, nil, "add group member",
	)
}

// RemoveGroupMember removes one direct user member by stable IDs.
func (client *Client) RemoveGroupMember(
	ctx context.Context, groupID, userID string,
) error {
	return client.doJSON(
		ctx, http.MethodDelete,
		groupResource(groupID)+"/members/"+url.PathEscape(userID)+"/$ref",
		nil, nil, nil, "remove group member",
	)
}

// ListApplications returns the server-advertised applications and role IDs.
func (client *Client) ListApplications(
	ctx context.Context,
) ([]Application, error) {
	var result struct {
		Value []Application `json:"value"`
	}
	if err := client.doJSON(
		ctx, http.MethodGet, "/graph/v1.0/applications",
		nil, nil, &result, "list role applications",
	); err != nil {
		return nil, err
	}
	return result.Value, nil
}

// ListAppRoleAssignments returns a user's role assignments.
func (client *Client) ListAppRoleAssignments(
	ctx context.Context, userID string,
) ([]AppRoleAssignment, error) {
	var result struct {
		Value []AppRoleAssignment `json:"value"`
	}
	if err := client.doJSON(
		ctx, http.MethodGet, userResource(userID)+"/appRoleAssignments",
		nil, nil, &result, "list user roles",
	); err != nil {
		return nil, err
	}
	return result.Value, nil
}

// AssignAppRole assigns a server-advertised role to a user.
func (client *Client) AssignAppRole(
	ctx context.Context, request AppRoleAssignment,
) (AppRoleAssignment, error) {
	var result AppRoleAssignment
	if err := client.doJSON(
		ctx, http.MethodPost,
		userResource(request.PrincipalID)+"/appRoleAssignments",
		request, nil, &result, "assign user role",
	); err != nil {
		return AppRoleAssignment{}, err
	}
	return result, nil
}

// RemoveAppRoleAssignment removes an assignment by stable IDs.
func (client *Client) RemoveAppRoleAssignment(
	ctx context.Context, userID, assignmentID string,
) error {
	return client.doJSON(
		ctx, http.MethodDelete,
		userResource(userID)+"/appRoleAssignments/"+
			url.PathEscape(assignmentID),
		nil, nil, nil, "remove user role",
	)
}

func userResource(userID string) string {
	return "/graph/v1.0/users/" + url.PathEscape(userID)
}

func groupResource(groupID string) string {
	return "/graph/v1.0/groups/" + url.PathEscape(groupID)
}
