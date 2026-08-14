package spaces

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mzner/ocis-cli/internal/apperror"
	"github.com/mzner/ocis-cli/internal/graph"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

func RunMember(
	ctx context.Context,
	request MemberRequest,
	selectedProfile string,
	options Options,
) error {
	options.Logger.Debug(
		"run space member operation", "operation", request.Operation,
	)
	client, selected, err := resolveProjectSpace(
		ctx, request.Space, selectedProfile, options,
	)
	if err != nil {
		return err
	}
	switch request.Operation {
	case MemberList:
		return listSpaceMembers(ctx, client, selected, options)
	case MemberAdd:
		return addSpaceMember(ctx, client, selected, request, options)
	case MemberUpdate:
		return updateSpaceMember(ctx, client, selected, request, options)
	case MemberRemove:
		return removeSpaceMember(ctx, client, selected, request, options)
	default:
		return apperror.Wrap(
			apperror.KindUsage, "space member",
			fmt.Errorf("unknown member operation %q", request.Operation),
		)
	}
}

func listSpaceMembers(
	ctx context.Context, client Client, selected graph.Drive, options Options,
) error {
	permissions, err := client.Graph().ListSpacePermissions(ctx, selected.ID)
	if err != nil {
		return err
	}
	members := membersFromPermissions(permissions)
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "space-member", members)
	}
	for _, member := range members {
		if _, err := fmt.Fprintf(
			options.Out, "%-7s %-10s %-24s %-36s %s\n",
			member.SubjectType, member.Role, member.DisplayName,
			member.SubjectID, member.PermissionID,
		); err != nil {
			return err
		}
	}
	return nil
}

func addSpaceMember(
	ctx context.Context,
	client Client,
	selected graph.Drive,
	request MemberRequest,
	options Options,
) error {
	request.RecipientID = strings.TrimSpace(request.RecipientID)
	request.RecipientType = strings.ToLower(strings.TrimSpace(request.RecipientType))
	if request.RecipientID == "" {
		return usageSpaceMember("recipient ID must not be empty")
	}
	if request.RecipientType != "user" && request.RecipientType != "group" {
		return usageSpaceMember(
			fmt.Sprintf(
				"invalid recipient type %q; expected user or group",
				request.RecipientType,
			),
		)
	}
	permissions, role, err := resolveSpaceRole(
		ctx, client, selected.ID, request.Role,
	)
	if err != nil {
		return err
	}
	recipient, err := resolveSpaceRecipient(
		ctx, client, request.RecipientType, request.RecipientID,
		request.RecipientIsID,
	)
	if err != nil {
		return err
	}
	if request.DryRun {
		return output(
			options, "space-member",
			map[string]any{
				"operation": "add", "spaceId": selected.ID,
				"recipient":     request.RecipientID,
				"recipientId":   recipient.ID,
				"displayName":   recipient.DisplayName,
				"recipientType": request.RecipientType,
				"role":          strings.ToLower(role.DisplayName),
				"roleId":        role.ID, "dryRun": true,
			},
			"Would add %s %s to %s as %s\n",
			request.RecipientType,
			fallback(recipient.DisplayName, recipient.ID), selected.Name,
			strings.ToLower(role.DisplayName),
		)
	}
	permission, err := client.Graph().AddSpaceMember(
		ctx, selected.ID,
		graph.InviteRequest{
			Recipients: []graph.Recipient{{
				ObjectID: recipient.ID, Type: request.RecipientType,
			}},
			Roles: []string{role.ID},
		},
	)
	if err != nil {
		return err
	}
	member := memberFromPermission(permissions, permission)
	return output(
		options, "space-member", member,
		"Added %s %s to %s as %s\n",
		member.SubjectType, member.DisplayName, selected.Name, member.Role,
	)
}

func updateSpaceMember(
	ctx context.Context,
	client Client,
	selected graph.Drive,
	request MemberRequest,
	options Options,
) error {
	request.PermissionID = strings.TrimSpace(request.PermissionID)
	if request.PermissionID == "" {
		return usageSpaceMember("permission ID must not be empty")
	}
	permissions, role, err := resolveSpaceRole(
		ctx, client, selected.ID, request.Role,
	)
	if err != nil {
		return err
	}
	if request.DryRun {
		return output(
			options, "space-member",
			map[string]any{
				"operation": "update", "spaceId": selected.ID,
				"permissionId": request.PermissionID,
				"role":         strings.ToLower(role.DisplayName),
				"roleId":       role.ID, "dryRun": true,
			},
			"Would update permission %s in %s to %s\n",
			request.PermissionID, selected.Name,
			strings.ToLower(role.DisplayName),
		)
	}
	permission, err := client.Graph().UpdateSpaceMember(
		ctx, selected.ID, request.PermissionID,
		graph.PermissionUpdateRequest{Roles: []string{role.ID}},
	)
	if err != nil {
		return err
	}
	member := memberFromPermission(permissions, permission)
	return output(
		options, "space-member", member,
		"Updated %s to %s in %s\n",
		fallback(member.DisplayName, request.PermissionID),
		member.Role, selected.Name,
	)
}

func removeSpaceMember(
	ctx context.Context,
	client Client,
	selected graph.Drive,
	request MemberRequest,
	options Options,
) error {
	request.PermissionID = strings.TrimSpace(request.PermissionID)
	if request.PermissionID == "" {
		return usageSpaceMember("permission ID must not be empty")
	}
	if request.DryRun {
		return output(
			options, "space-member",
			map[string]any{
				"operation": "remove", "spaceId": selected.ID,
				"permissionId": request.PermissionID, "dryRun": true,
			},
			"Would remove permission %s from %s\n",
			request.PermissionID, selected.Name,
		)
	}
	if err := client.Graph().RemoveSpaceMember(
		ctx, selected.ID, request.PermissionID,
	); err != nil {
		return err
	}
	return output(
		options, "space-member",
		map[string]any{
			"removed": request.PermissionID, "spaceId": selected.ID,
		},
		"Removed permission %s from %s\n",
		request.PermissionID, selected.Name,
	)
}

func resolveSpaceRole(
	ctx context.Context,
	client Client,
	spaceID string,
	requested string,
) (graph.Permissions, graph.RoleDefinition, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return graph.Permissions{}, graph.RoleDefinition{},
			usageSpaceMember("role must not be empty")
	}
	permissions, err := client.Graph().ListSpacePermissions(ctx, spaceID)
	if err != nil {
		return graph.Permissions{}, graph.RoleDefinition{}, err
	}
	for _, role := range permissions.AllowedRoles {
		if strings.EqualFold(role.ID, requested) ||
			strings.EqualFold(role.DisplayName, requested) {
			return permissions, role, nil
		}
	}
	var semanticMatches []graph.RoleDefinition
	for _, role := range permissions.AllowedRoles {
		if roleSemantic(role.DisplayName) == roleSemantic(requested) &&
			roleSemantic(requested) != "" {
			semanticMatches = append(semanticMatches, role)
		}
	}
	if len(semanticMatches) == 1 {
		return permissions, semanticMatches[0], nil
	}
	if len(semanticMatches) > 1 {
		return graph.Permissions{}, graph.RoleDefinition{},
			usageSpaceMember(fmt.Sprintf(
				"role alias %q matches multiple server roles; use an exact role name or ID",
				requested,
			))
	}
	names := make([]string, 0, len(permissions.AllowedRoles))
	for _, role := range permissions.AllowedRoles {
		names = append(names, strings.ToLower(role.DisplayName))
	}
	sort.Strings(names)
	return graph.Permissions{}, graph.RoleDefinition{},
		usageSpaceMember(fmt.Sprintf(
			"role %q is not available for this Space; available roles: %s",
			requested, strings.Join(names, ", "),
		))
}

func roleSemantic(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.TrimPrefix(normalized, "can ")
	first, _, _ := strings.Cut(normalized, " ")
	switch first {
	case "view", "viewer", "read", "reader":
		return "viewer"
	case "edit", "editor", "write", "writer":
		return "editor"
	case "manage", "manager":
		return "manager"
	default:
		return ""
	}
}

func memberFromPermission(
	permissions graph.Permissions, permission graph.Permission,
) SpaceMember {
	permissions.Value = []graph.Permission{permission}
	members := membersFromPermissions(permissions)
	if len(members) == 1 {
		return members[0]
	}
	roleID := ""
	if len(permission.Roles) > 0 {
		roleID = permission.Roles[0]
	}
	return SpaceMember{
		PermissionID: permission.ID,
		Role:         roleDisplayName(roleNames(permissions.AllowedRoles), roleID),
		RoleID:       roleID,
	}
}

func usageSpaceMember(message string) error {
	return apperror.Wrap(
		apperror.KindUsage, "space member", fmt.Errorf("%s", message),
	)
}
