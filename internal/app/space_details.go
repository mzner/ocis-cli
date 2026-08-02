package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/mzner/ocis-cli/internal/graph"
)

const (
	actionAddMember    = "libre.graph/driveItem/permissions/create"
	actionListMembers  = "libre.graph/driveItem/permissions/read"
	actionUpdateMember = "libre.graph/driveItem/permissions/update"
	actionRemoveMember = "libre.graph/driveItem/permissions/delete"
)

// SpaceMember is the stable application representation of a Space member.
type SpaceMember struct {
	PermissionID string   `json:"permissionId"`
	SubjectType  string   `json:"subjectType"`
	SubjectID    string   `json:"subjectId"`
	DisplayName  string   `json:"displayName"`
	Role         string   `json:"role,omitempty"`
	RoleID       string   `json:"roleId,omitempty"`
	Actions      []string `json:"actions,omitempty"`
}

type spaceRole struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Description string `json:"description,omitempty"`
}

type spaceAdministration struct {
	CanListMembers   bool `json:"canListMembers"`
	CanAddMembers    bool `json:"canAddMembers"`
	CanUpdateMembers bool `json:"canUpdateMembers"`
	CanRemoveMembers bool `json:"canRemoveMembers"`
	CanManageMembers bool `json:"canManageMembers"`
}

type spaceDetails struct {
	Space                space               `json:"space"`
	Members              []SpaceMember       `json:"members"`
	AvailableRoles       []spaceRole         `json:"availableRoles"`
	AllowedActions       []string            `json:"allowedActions"`
	CurrentUser          *graph.Me           `json:"currentUser,omitempty"`
	CurrentRole          string              `json:"currentRole,omitempty"`
	PermissionsAvailable bool                `json:"permissionsAvailable"`
	Administration       spaceAdministration `json:"administration"`
	QuotaUsagePercent    float64             `json:"quotaUsagePercent,omitempty"`
	QuotaUnlimited       bool                `json:"quotaUnlimited"`
}

func loadSpaceDetails(
	ctx context.Context, client *client, selected space,
) (spaceDetails, error) {
	details := spaceDetails{
		Space: selected, Members: []SpaceMember{}, AvailableRoles: []spaceRole{},
		QuotaUnlimited: selected.Quota.Total == 0,
	}
	if selected.Quota.Total > 0 {
		details.QuotaUsagePercent =
			float64(selected.Quota.Used) / float64(selected.Quota.Total) * 100
	}
	permissions, err := client.graphClient().ListSpacePermissions(ctx, selected.ID)
	if err != nil {
		switch protocolStatus(err) {
		case 401, 403:
			return details, nil
		default:
			return spaceDetails{}, err
		}
	}
	details.PermissionsAvailable = true
	details.Members = membersFromPermissions(permissions)
	details.AvailableRoles = rolesFromPermissions(permissions)
	details.AllowedActions = permissions.AllowedActions
	details.Administration = administrationFromActions(permissions.AllowedActions)
	details.Administration.CanListMembers = true

	current, err := client.graphClient().GetMe(ctx)
	if err == nil {
		details.CurrentUser = &current
		for _, member := range details.Members {
			if member.SubjectType == "user" && member.SubjectID == current.ID {
				details.CurrentRole = member.Role
				break
			}
		}
	}
	return details, nil
}

func writeSpaceDetails(options RunOptions, details spaceDetails) error {
	value := details.Space
	quota := fmt.Sprintf("%d / %d bytes", value.Quota.Used, value.Quota.Total)
	if details.QuotaUnlimited {
		quota = fmt.Sprintf("%d bytes used / unlimited", value.Quota.Used)
	} else {
		quota += fmt.Sprintf(" (%.1f%%)", details.QuotaUsagePercent)
	}
	if _, err := fmt.Fprintf(
		options.Out,
		"%s\n  ID: %s\n  Type: %s\n  Alias: %s\n  Description: %s\n"+
			"  Quota: %s\n  Quota state: %s\n",
		value.Name, value.ID, value.DriveType, value.DriveAlias,
		value.Description, quota, value.Quota.State,
	); err != nil {
		return err
	}
	if !details.PermissionsAvailable {
		_, err := fmt.Fprintln(
			options.Out,
			"  Members: unavailable (not permitted)\n  Manage members: no",
		)
		return err
	}
	if _, err := fmt.Fprintf(
		options.Out,
		"  Members: %d\n  Current role: %s\n  Manage members: %s\n",
		len(details.Members), fallback(details.CurrentRole, "none"),
		yesNo(details.Administration.CanManageMembers),
	); err != nil {
		return err
	}
	for _, member := range details.Members {
		if _, err := fmt.Fprintf(
			options.Out, "    %-7s %-10s %-24s %s\n",
			member.SubjectType, member.Role, member.DisplayName,
			member.PermissionID,
		); err != nil {
			return err
		}
	}
	return nil
}

func membersFromPermissions(permissions graph.Permissions) []SpaceMember {
	roles := roleNames(permissions.AllowedRoles)
	members := make([]SpaceMember, 0, len(permissions.Value))
	for _, permission := range permissions.Value {
		if permission.GrantedToV2 == nil {
			continue
		}
		subjectType := ""
		var identity *graph.Identity
		switch {
		case permission.GrantedToV2.User != nil:
			subjectType, identity = "user", permission.GrantedToV2.User
		case permission.GrantedToV2.Group != nil:
			subjectType, identity = "group", permission.GrantedToV2.Group
		default:
			continue
		}
		roleID := ""
		if len(permission.Roles) > 0 {
			roleID = permission.Roles[0]
		}
		members = append(members, SpaceMember{
			PermissionID: permission.ID, SubjectType: subjectType,
			SubjectID: identity.ID, DisplayName: identity.DisplayName,
			Role: roleDisplayName(roles, roleID), RoleID: roleID,
			Actions: permission.AllowedActions,
		})
	}
	return members
}

func rolesFromPermissions(permissions graph.Permissions) []spaceRole {
	roles := make([]spaceRole, 0, len(permissions.AllowedRoles))
	for _, role := range permissions.AllowedRoles {
		roles = append(roles, spaceRole{
			ID: role.ID, DisplayName: role.DisplayName,
			Description: role.Description,
		})
	}
	return roles
}

func roleNames(roles []graph.RoleDefinition) map[string]string {
	result := make(map[string]string, len(roles))
	for _, role := range roles {
		result[role.ID] = role.DisplayName
	}
	return result
}

func roleDisplayName(roles map[string]string, roleID string) string {
	if name := roles[roleID]; name != "" {
		return strings.ToLower(name)
	}
	return roleID
}

func administrationFromActions(actions []string) spaceAdministration {
	result := spaceAdministration{
		CanListMembers:   containsString(actions, actionListMembers),
		CanAddMembers:    containsString(actions, actionAddMember),
		CanUpdateMembers: containsString(actions, actionUpdateMember),
		CanRemoveMembers: containsString(actions, actionRemoveMember),
	}
	result.CanManageMembers = result.CanAddMembers &&
		result.CanUpdateMembers && result.CanRemoveMembers
	return result
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func fallback(value, alternative string) string {
	if value == "" {
		return alternative
	}
	return value
}
