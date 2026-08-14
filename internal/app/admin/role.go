package admin

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/mzner/ocis-cli/internal/apperror"
	"github.com/mzner/ocis-cli/internal/graph"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

type adminRoleAssignment struct {
	AssignmentID  string `json:"assignmentId"`
	RoleID        string `json:"roleId"`
	Role          string `json:"role"`
	ApplicationID string `json:"applicationId"`
	Application   string `json:"application"`
	PrincipalID   string `json:"principalId"`
	PrincipalType string `json:"principalType,omitempty"`
}

type advertisedRole struct {
	application graph.Application
	role        graph.AppRole
}

func RunRole(
	ctx context.Context,
	request RoleRequest,
	selectedProfile string,
	options Options,
) error {
	request.User = strings.TrimSpace(request.User)
	request.Role = strings.TrimSpace(request.Role)
	if request.Operation != RoleAvailable && request.User == "" {
		return apperror.Wrap(
			apperror.KindUsage, "admin user role",
			fmt.Errorf("user identifier is required"),
		)
	}
	if (request.Operation == RoleGrant ||
		request.Operation == RoleRevoke) && request.Role == "" {
		return apperror.Wrap(
			apperror.KindUsage, "admin user role",
			fmt.Errorf("role name, role ID, or assignment ID is required"),
		)
	}
	selected, err := options.NewClient(ctx, selectedProfile)
	if err != nil {
		return err
	}
	if err := options.RequireAccountAdmin(ctx, selected); err != nil {
		return err
	}
	applications, err := selected.Graph().ListApplications(ctx)
	if err != nil {
		return roleServiceError(err)
	}
	roles := advertisedRoles(applications)
	if request.Operation == RoleAvailable {
		return listAvailableAdminRoles(roles, options)
	}
	user, err := resolveMutationUser(ctx, selected, request.User)
	if err != nil {
		return err
	}
	assignments, err := selected.Graph().ListAppRoleAssignments(ctx, user.ID)
	if err != nil {
		return roleServiceError(err)
	}
	switch request.Operation {
	case RoleList:
		return listAdminRoles(assignments, roles, options)
	case RoleGrant:
		role, err := ResolveAdvertisedRole(roles, request.Role)
		if err != nil {
			return err
		}
		if request.DryRun {
			return output(
				options, "admin-role-change",
				map[string]any{
					"operation": "grant", "userId": user.ID,
					"username": user.Username,
					"roleId":   role.role.ID, "role": role.role.DisplayName,
					"applicationId": role.application.ID, "dryRun": true,
				},
				"Would assign role %s (%s) to user %s (%s)\n",
				role.role.DisplayName, role.role.ID,
				fallback(user.Username, user.DisplayName), user.ID,
			)
		}
		created, err := selected.Graph().AssignAppRole(
			ctx, graph.AppRoleAssignment{
				AppRoleID: role.role.ID, PrincipalID: user.ID,
				ResourceID: role.application.ID,
			},
		)
		if err != nil {
			return roleServiceError(err)
		}
		return output(
			options, "admin-role-assignment", created,
			"Assigned role %s (%s) to user %s (%s)\n",
			role.role.DisplayName, role.role.ID,
			fallback(user.Username, user.DisplayName), user.ID,
		)
	case RoleRevoke:
		if err := rejectSelfTarget(ctx, selected, user, "revoke roles from"); err != nil {
			return err
		}
		assignment, roleName, err := ResolveRoleAssignment(
			assignments, roles, request.Role,
		)
		if err != nil {
			return err
		}
		if request.DryRun {
			return output(
				options, "admin-role-change",
				map[string]any{
					"operation": "revoke", "userId": user.ID,
					"username":     user.Username,
					"assignmentId": assignment.ID,
					"roleId":       assignment.AppRoleID, "role": roleName,
					"dryRun": true,
				},
				"Would revoke role %s (%s) from user %s (%s)\n",
				roleName, assignment.AppRoleID,
				fallback(user.Username, user.DisplayName), user.ID,
			)
		}
		if err := selected.Graph().RemoveAppRoleAssignment(
			ctx, user.ID, assignment.ID,
		); err != nil {
			return roleServiceError(err)
		}
		return output(
			options, "admin-role-change",
			map[string]any{
				"operation": "revoke", "userId": user.ID,
				"username":     user.Username,
				"assignmentId": assignment.ID,
				"roleId":       assignment.AppRoleID, "role": roleName,
				"revoked": true,
			},
			"Revoked role %s (%s) from user %s (%s)\n",
			roleName, assignment.AppRoleID,
			fallback(user.Username, user.DisplayName), user.ID,
		)
	default:
		return apperror.Wrap(
			apperror.KindUsage, "admin user role",
			fmt.Errorf("unknown role operation %q", request.Operation),
		)
	}
}

func advertisedRoles(applications []graph.Application) []advertisedRole {
	var roles []advertisedRole
	for _, application := range applications {
		for _, role := range application.AppRoles {
			roles = append(roles, advertisedRole{
				application: application, role: role,
			})
		}
	}
	return roles
}

func ResolveAdvertisedRole(
	roles []advertisedRole, selector string,
) (advertisedRole, error) {
	var matches []advertisedRole
	for _, role := range roles {
		if role.role.ID == selector ||
			strings.EqualFold(role.role.DisplayName, selector) {
			matches = append(matches, role)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return advertisedRole{}, fmt.Errorf(
			"unknown server role %q; use ocis admin user role available",
			selector,
		)
	}
	return advertisedRole{}, fmt.Errorf(
		"role %q is ambiguous across %d applications; use its role ID",
		selector, len(matches),
	)
}

func ResolveRoleAssignment(
	assignments []graph.AppRoleAssignment,
	roles []advertisedRole,
	selector string,
) (graph.AppRoleAssignment, string, error) {
	var matches []graph.AppRoleAssignment
	for _, assignment := range assignments {
		name := adminRoleDisplayName(roles, assignment)
		if assignment.ID == selector || assignment.AppRoleID == selector ||
			strings.EqualFold(name, selector) {
			matches = append(matches, assignment)
		}
	}
	if len(matches) == 1 {
		return matches[0], adminRoleDisplayName(roles, matches[0]), nil
	}
	if len(matches) == 0 {
		return graph.AppRoleAssignment{}, "", fmt.Errorf(
			"user does not have role or assignment %q", selector,
		)
	}
	return graph.AppRoleAssignment{}, "", fmt.Errorf(
		"role selector %q matches %d assignments; use the assignment ID",
		selector, len(matches),
	)
}

func listAdminRoles(
	assignments []graph.AppRoleAssignment,
	roles []advertisedRole,
	options Options,
) error {
	rows := make([]adminRoleAssignment, 0, len(assignments))
	for _, assignment := range assignments {
		applicationName := assignment.ResourceDisplayName
		for _, role := range roles {
			if role.application.ID == assignment.ResourceID {
				applicationName = role.application.DisplayName
				break
			}
		}
		rows = append(rows, adminRoleAssignment{
			AssignmentID:  assignment.ID,
			RoleID:        assignment.AppRoleID,
			Role:          adminRoleDisplayName(roles, assignment),
			ApplicationID: assignment.ResourceID,
			Application:   applicationName,
			PrincipalID:   assignment.PrincipalID,
			PrincipalType: assignment.PrincipalType,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		return strings.ToLower(rows[i].Role) < strings.ToLower(rows[j].Role)
	})
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "admin-role-assignment", rows)
	}
	return writeAdminTable(options.Out, func(writer io.Writer) error {
		if _, err := fmt.Fprintln(
			writer, "ROLE\tROLE ID\tASSIGNMENT ID\tAPPLICATION",
		); err != nil {
			return err
		}
		for _, row := range rows {
			if _, err := fmt.Fprintf(
				writer, "%s\t%s\t%s\t%s\n",
				row.Role, row.RoleID, row.AssignmentID, row.Application,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

func listAvailableAdminRoles(
	roles []advertisedRole,
	options Options,
) error {
	type roleRow struct {
		Role          string `json:"role"`
		RoleID        string `json:"roleId"`
		Application   string `json:"application"`
		ApplicationID string `json:"applicationId"`
	}
	rows := make([]roleRow, 0, len(roles))
	for _, role := range roles {
		rows = append(rows, roleRow{
			Role: role.role.DisplayName, RoleID: role.role.ID,
			Application:   role.application.DisplayName,
			ApplicationID: role.application.ID,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		return strings.ToLower(rows[i].Role) < strings.ToLower(rows[j].Role)
	})
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "admin-role", rows)
	}
	return writeAdminTable(options.Out, func(writer io.Writer) error {
		if _, err := fmt.Fprintln(
			writer, "ROLE\tROLE ID\tAPPLICATION\tAPPLICATION ID",
		); err != nil {
			return err
		}
		for _, row := range rows {
			if _, err := fmt.Fprintf(
				writer, "%s\t%s\t%s\t%s\n",
				row.Role, row.RoleID, row.Application, row.ApplicationID,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

func adminRoleDisplayName(
	roles []advertisedRole, assignment graph.AppRoleAssignment,
) string {
	for _, role := range roles {
		if role.application.ID == assignment.ResourceID &&
			role.role.ID == assignment.AppRoleID {
			return role.role.DisplayName
		}
	}
	return assignment.AppRoleID
}

func roleServiceError(err error) error {
	switch protocolStatus(err) {
	case 404, 405, 501:
		return fmt.Errorf(
			"server role service is not configured or does not support this operation: %w",
			err,
		)
	default:
		return err
	}
}
