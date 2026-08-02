package graph

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// ListItemPermissions returns permissions and server-advertised roles for a
// file or folder.
func (client *Client) ListItemPermissions(
	ctx context.Context, resourceID string,
) (Permissions, error) {
	resource, err := itemPermissionsResource(resourceID)
	if err != nil {
		return Permissions{}, err
	}
	var permissions Permissions
	if err := client.doJSON(
		ctx, http.MethodGet, resource, nil, nil, &permissions,
		"list item permissions",
	); err != nil {
		return Permissions{}, err
	}
	sort.Slice(permissions.AllowedRoles, func(left, right int) bool {
		return permissions.AllowedRoles[left].DisplayName <
			permissions.AllowedRoles[right].DisplayName
	})
	return permissions, nil
}

// InviteItem grants a user or group a server-advertised role on a file or
// folder.
func (client *Client) InviteItem(
	ctx context.Context, resourceID string, request InviteRequest,
) (Permission, error) {
	resource, err := itemPermissionsResource(resourceID)
	if err != nil {
		return Permission{}, err
	}
	if len(request.Recipients) != 1 || len(request.Roles) != 1 {
		return Permission{}, fmt.Errorf(
			"item invitation requires exactly one recipient and one role",
		)
	}
	var result struct {
		Value []Permission `json:"value"`
	}
	resource = strings.TrimSuffix(resource, "/permissions") + "/invite"
	if err := client.doJSON(
		ctx, http.MethodPost, resource, request, nil, &result,
		"invite item recipient",
	); err != nil {
		return Permission{}, err
	}
	if len(result.Value) != 1 {
		return Permission{}, fmt.Errorf(
			"decode item invitation: expected one permission, received %d",
			len(result.Value),
		)
	}
	return result.Value[0], nil
}

// UpdateItemPermission changes a direct share to another advertised role.
func (client *Client) UpdateItemPermission(
	ctx context.Context,
	resourceID string,
	permissionID string,
	request PermissionUpdateRequest,
) (Permission, error) {
	resource, err := itemPermissionResource(resourceID, permissionID)
	if err != nil {
		return Permission{}, err
	}
	var permission Permission
	if err := client.doJSON(
		ctx, http.MethodPatch, resource, request, nil, &permission,
		"update item permission",
	); err != nil {
		return Permission{}, err
	}
	return permission, nil
}

// GetItemPermission returns one permission on a file or folder.
func (client *Client) GetItemPermission(
	ctx context.Context, resourceID, permissionID string,
) (Permission, error) {
	resource, err := itemPermissionResource(resourceID, permissionID)
	if err != nil {
		return Permission{}, err
	}
	var permission Permission
	if err := client.doJSON(
		ctx, http.MethodGet, resource, nil, nil, &permission,
		"get item permission",
	); err != nil {
		return Permission{}, err
	}
	return permission, nil
}

// UpdateLinkPermission changes selected public-link properties.
func (client *Client) UpdateLinkPermission(
	ctx context.Context,
	resourceID string,
	permissionID string,
	request LinkPermissionUpdateRequest,
) (Permission, error) {
	if request.Empty() {
		return Permission{}, fmt.Errorf("public-link update must not be empty")
	}
	resource, err := itemPermissionResource(resourceID, permissionID)
	if err != nil {
		return Permission{}, err
	}
	var permission Permission
	if err := client.doJSON(
		ctx, http.MethodPatch, resource, request, nil, &permission,
		"update public link",
	); err != nil {
		return Permission{}, err
	}
	return permission, nil
}

// SetItemPermissionPassword creates, replaces, or removes a public-link
// password. An empty password requests removal.
func (client *Client) SetItemPermissionPassword(
	ctx context.Context, resourceID, permissionID, password string,
) (Permission, error) {
	resource, err := itemPermissionResource(resourceID, permissionID)
	if err != nil {
		return Permission{}, err
	}
	var permission Permission
	if err := client.doJSON(
		ctx, http.MethodPost, resource+"/setPassword",
		map[string]string{"password": password}, nil, &permission,
		"set public-link password",
	); err != nil {
		return Permission{}, err
	}
	return permission, nil
}

// RemoveItemPermission removes one direct sharing permission.
func (client *Client) RemoveItemPermission(
	ctx context.Context, resourceID string, permissionID string,
) error {
	resource, err := itemPermissionResource(resourceID, permissionID)
	if err != nil {
		return err
	}
	return client.doJSON(
		ctx, http.MethodDelete, resource, nil, nil, nil,
		"remove item permission",
	)
}

func itemPermissionResource(
	resourceID string, permissionID string,
) (string, error) {
	if strings.TrimSpace(permissionID) == "" {
		return "", fmt.Errorf("permission ID must not be empty")
	}
	resource, err := itemPermissionsResource(resourceID)
	if err != nil {
		return "", err
	}
	return resource + "/" + url.PathEscape(permissionID), nil
}

func itemPermissionsResource(resourceID string) (string, error) {
	resourceID = strings.TrimSpace(resourceID)
	driveID, _, found := strings.Cut(resourceID, "!")
	if !found || driveID == "" {
		return "", fmt.Errorf("invalid resource ID %q", resourceID)
	}
	return fmt.Sprintf(
		"/graph/v1beta1/drives/%s/items/%s/permissions",
		url.PathEscape(driveID), url.PathEscape(resourceID),
	), nil
}
