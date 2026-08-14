package share

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mzner/ocis-cli/internal/apperror"
	"github.com/mzner/ocis-cli/internal/graph"
	appoutput "github.com/mzner/ocis-cli/internal/output"
	"github.com/mzner/ocis-cli/internal/sharing"
)

// Run validates and executes one sharing operation.
func Run(
	ctx context.Context, request Request, selectedProfile string, options Options,
) error {
	options.Logger.Debug("run share operation", "operation", request.Operation)
	if request.Operation == Create {
		if err := validateShareCreateRequest(request); err != nil {
			return err
		}
	}
	if request.Operation == LinkUpdate {
		if err := validatePublicLinkUpdate(request); err != nil {
			return err
		}
	}
	if request.Operation == Received {
		if _, _, err := receivedShareStateFilter(request.State); err != nil {
			return err
		}
	}
	if request.Operation == Overview {
		if err := validateShareOverviewFilters(request); err != nil {
			return err
		}
	}
	if request.Operation == Remove && !request.Confirmed {
		return usageShare(
			"removing a share requires explicit confirmation",
		)
	}
	if request.DryRun {
		switch request.Operation {
		case Create:
			permissions := request.Permissions
			if permissions == 0 {
				permissions = 1
			}
			return output(
				options, "share",
				map[string]any{
					"operation": "create", "path": cleanRemote(request.Path),
					"permissions": permissions, "dryRun": true,
				},
				"Would create public link for %s\n", cleanRemote(request.Path),
			)
		case Revoke:
			return output(
				options, "share",
				map[string]any{"operation": "revoke", "id": request.ID, "dryRun": true},
				"Would revoke public link %s\n", request.ID,
			)
		}
	}
	client, err := options.NewClient(ctx, selectedProfile)
	if err != nil {
		return err
	}
	switch request.Operation {
	case Create, List, DirectAdd, FederatedAdd, Roles:
		if err := client.SelectSpace(options.Space); err != nil {
			return err
		}
	case Received:
		if options.Space != "" {
			return usageShare(
				"--space cannot filter received shares; use the optional received path",
			)
		}
	case Overview:
		// An overview is cross-Space by default. Its optional --space value is
		// a filter and must not activate or mutate the saved Space selection.
	case LinkInfo, LinkUpdate, DirectUpdate, Remove,
		Accept, Decline:
		if options.Space != "" {
			return usageShare(
				"--space cannot filter an operation addressed by share ID",
			)
		}
	}
	switch request.Operation {
	case Create:
		return createPublicLink(ctx, client, request, options)
	case List:
		if request.LinksOnly {
			return listPublicLinks(ctx, client, request, options)
		}
		return listOutgoingShares(ctx, client, request, options)
	case LinkInfo:
		return showPublicLink(ctx, client, request.ID, options)
	case LinkUpdate:
		return updatePublicLink(ctx, client, request, options)
	case DirectAdd:
		return addDirectShare(ctx, client, request, options)
	case FederatedAdd:
		return addFederatedShare(ctx, client, request, options)
	case DirectUpdate:
		return updateDirectShare(ctx, client, request, options)
	case Remove:
		return removeShare(ctx, client, request, options)
	case Received:
		return listReceivedShares(ctx, client, request, options)
	case Overview:
		return listShareOverview(ctx, client, request, options)
	case Accept, Decline:
		return respondToReceivedShare(ctx, client, request, options)
	case Roles:
		return listShareRoles(
			ctx, client, request.Path, request.Federated, options,
		)
	case Revoke:
		if err := client.Sharing().RevokeLink(ctx, request.ID); err != nil {
			return err
		}
		return output(
			options, "share", map[string]string{"revoked": request.ID},
			"Revoked public link %s\n", request.ID,
		)
	default:
		return apperror.Wrap(
			apperror.KindUsage, "share",
			fmt.Errorf("unknown share command %q", request.Operation),
		)
	}
}

func addDirectShare(
	ctx context.Context, client Client, request Request, options Options,
) error {
	request.Recipient = strings.TrimSpace(request.Recipient)
	request.RecipientType = strings.ToLower(
		strings.TrimSpace(request.RecipientType),
	)
	if request.Recipient == "" {
		return usageShare("recipient must not be empty")
	}
	if request.RecipientType != "user" && request.RecipientType != "group" {
		return usageShare(fmt.Sprintf(
			"invalid recipient type %q; expected user or group",
			request.RecipientType,
		))
	}
	capabilities, err := client.Sharing().Capabilities(ctx)
	if err != nil {
		return fmt.Errorf("check sharing capabilities: %w", err)
	}
	if !capabilities.Sharing.APIEnabled {
		return apperror.Wrap(
			apperror.KindConflict, "share add",
			errors.New("direct sharing is disabled by the server"),
		)
	}
	if request.RecipientType == "group" &&
		!capabilities.Sharing.GroupEnabled {
		return apperror.Wrap(
			apperror.KindConflict, "share add",
			errors.New("group sharing is disabled by the server"),
		)
	}
	remote := cleanRemote(request.Path)
	metadata, err := client.Stat(remote)
	if err != nil {
		return err
	}
	if metadata.ResourceID == "" {
		return fmt.Errorf(
			"server did not return a stable resource ID for %s", remote,
		)
	}
	_, role, err := resolveDirectRole(
		ctx, client, metadata.ResourceID, request.Role, false,
	)
	if err != nil {
		return err
	}
	recipient, err := resolveRecipient(
		ctx, client, request.RecipientType, request.Recipient,
		request.RecipientIsID, usageShare,
	)
	if err != nil {
		return err
	}
	if request.DryRun {
		return output(
			options, "share",
			map[string]any{
				"operation": "add", "path": remote,
				"resourceId": metadata.ResourceID,
				"recipient":  request.Recipient, "recipientId": recipient.ID,
				"recipientType": request.RecipientType,
				"role":          role.DisplayName, "roleId": role.ID, "dryRun": true,
			},
			"Would share %s with %s %s as %s\n",
			remote, request.RecipientType,
			fallback(recipient.DisplayName, recipient.ID), role.DisplayName,
		)
	}
	permission, err := client.Graph().InviteItem(
		ctx, metadata.ResourceID,
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
	result := directShareOutput{
		ID: permission.ID, Path: remote, RecipientType: request.RecipientType,
		RecipientID:   recipient.ID,
		RecipientName: fallback(recipient.DisplayName, recipient.ID),
		RoleID:        role.ID, Role: role.DisplayName,
	}
	return output(
		options, "share", result,
		"Shared %s with %s %s as %s\nShare ID: %s\n",
		remote, request.RecipientType, result.RecipientName,
		result.Role, result.ID,
	)
}

func addFederatedShare(
	ctx context.Context, client Client, request Request, options Options,
) error {
	request.Recipient = strings.TrimSpace(request.Recipient)
	if request.Recipient == "" {
		return usageShare("federated recipient must not be empty")
	}
	capabilities, err := client.Sharing().Capabilities(ctx)
	if err != nil {
		return fmt.Errorf("check federation capabilities: %w", err)
	}
	if !capabilities.Sharing.Federation.Outgoing {
		return apperror.Wrap(
			apperror.KindConflict, "share federated add",
			errors.New("outgoing federation is disabled by the server"),
		)
	}
	remote := cleanRemote(request.Path)
	metadata, err := client.Stat(remote)
	if err != nil {
		return err
	}
	if metadata.ResourceID == "" {
		return fmt.Errorf(
			"server did not return a stable resource ID for %s", remote,
		)
	}
	_, role, err := resolveDirectRole(
		ctx, client, metadata.ResourceID, request.Role, true,
	)
	if err != nil {
		return err
	}
	recipient, err := resolveFederatedRecipient(
		ctx, client, request.Recipient, request.RecipientIsID,
	)
	if err != nil {
		return err
	}
	if request.DryRun {
		return output(
			options, "share",
			map[string]any{
				"operation": "add", "path": remote,
				"resourceId": metadata.ResourceID,
				"recipient":  request.Recipient, "recipientId": recipient.ID,
				"recipientType": "federated", "role": role.DisplayName,
				"roleId": role.ID, "dryRun": true,
			},
			"Would share %s with federated user %s as %s\n",
			remote, fallback(recipient.DisplayName, recipient.ID), role.DisplayName,
		)
	}
	permission, err := client.Graph().InviteItem(
		ctx, metadata.ResourceID,
		graph.InviteRequest{
			Recipients: []graph.Recipient{{ObjectID: recipient.ID, Type: "user"}},
			Roles:      []string{role.ID},
		},
	)
	if err != nil {
		return err
	}
	result := directShareOutput{
		ID: permission.ID, Path: remote, RecipientType: "federated",
		RecipientID:   recipient.ID,
		RecipientName: fallback(recipient.DisplayName, recipient.ID),
		RoleID:        role.ID, Role: role.DisplayName,
	}
	return output(
		options, "share", result,
		"Shared %s with federated user %s as %s\nShare ID: %s\n",
		remote, result.RecipientName, result.Role, result.ID,
	)
}

func updateDirectShare(
	ctx context.Context, client Client, request Request, options Options,
) error {
	selected, err := resolveOutgoingShare(ctx, client, request.ID, true)
	if err != nil {
		return err
	}
	_, role, err := resolveDirectRole(
		ctx, client, selected.ResourceID, request.Role,
		selected.Type == "federated",
	)
	if err != nil {
		return err
	}
	if request.DryRun {
		return output(
			options, "share",
			map[string]any{
				"operation": "update", "id": selected.ID,
				"path": selected.Path, "role": role.DisplayName,
				"roleId": role.ID, "dryRun": true,
			},
			"Would update share %s on %s to %s\n",
			selected.ID, selected.Path, role.DisplayName,
		)
	}
	permission, err := client.Graph().UpdateItemPermission(
		ctx, selected.ResourceID, selected.ID,
		graph.PermissionUpdateRequest{Roles: []string{role.ID}},
	)
	if err != nil {
		return err
	}
	if permission.ID == "" {
		permission.ID = selected.ID
	}
	result := directShareOutput{
		ID: permission.ID, Path: selected.Path,
		RecipientType: selected.Type, RecipientID: selected.RecipientID,
		RecipientName: selected.RecipientName,
		RoleID:        role.ID, Role: role.DisplayName,
	}
	return output(
		options, "share", result,
		"Updated share %s on %s to %s\n",
		selected.ID, selected.Path, role.DisplayName,
	)
}

func removeShare(
	ctx context.Context, client Client, request Request, options Options,
) error {
	selected, err := resolveOutgoingShare(ctx, client, request.ID, false)
	if err != nil {
		return err
	}
	if request.DryRun {
		return output(
			options, "share",
			map[string]any{
				"operation": "remove", "id": selected.ID,
				"path": selected.Path, "recipient": selected.RecipientName,
				"recipientType": selected.Type, "dryRun": true,
			},
			"Would remove %s share %s from %s\n",
			selected.Type, selected.ID, selected.Path,
		)
	}
	if selected.Type == "public_link" {
		if err := client.Sharing().RevokeLink(ctx, selected.ID); err != nil {
			return err
		}
	} else {
		if err := client.Graph().RemoveItemPermission(
			ctx, selected.ResourceID, selected.ID,
		); err != nil {
			return err
		}
	}
	return output(
		options, "share",
		map[string]any{
			"removed": selected.ID, "path": selected.Path,
			"recipient":     selected.RecipientName,
			"recipientType": selected.Type,
		},
		"Removed %s share %s from %s\n",
		selected.Type, selected.ID, selected.Path,
	)
}

func listOutgoingShares(
	ctx context.Context, client Client, request Request, options Options,
) error {
	values, err := client.Sharing().ListShares(
		ctx, sharing.ShareListRequest{
			Path: request.Path, SpaceID: client.SelectedSpaceID(),
		},
	)
	if err != nil {
		return err
	}
	return writeShares(values, false, options)
}

func listReceivedShares(
	ctx context.Context, client Client, request Request, options Options,
) error {
	state, allStates, err := receivedShareStateFilter(request.State)
	if err != nil {
		return err
	}
	values, err := client.Sharing().ListShares(
		ctx, sharing.ShareListRequest{
			Path: request.Path, Received: true,
			State: state, AllStates: allStates,
		},
	)
	if err != nil {
		return err
	}
	return writeShares(values, true, options)
}

func writeShares(
	values []sharing.Share, received bool, options Options,
) error {
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "share", values)
	}
	for _, value := range values {
		party := fallback(value.RecipientName, value.RecipientID)
		if received {
			party = fallback(value.OwnerName, value.Owner)
		}
		if party == "" {
			party = "-"
		}
		target := value.URL
		if target == "" {
			target = party
		}
		state := "-"
		if received {
			state = value.StateName
			if state == "" {
				state = "unknown"
			}
		}
		if _, err := fmt.Fprintf(
			options.Out, "%-12s %-12s %-10s %-8s %-24s %s\n",
			value.ID, value.Type, state,
			PermissionName(value.Permissions),
			value.Path, target,
		); err != nil {
			return err
		}
	}
	return nil
}

func receivedShareStateFilter(value string) (*int, bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return nil, false, nil
	case "all":
		return nil, true, nil
	case "accepted":
		state := 0
		return &state, false, nil
	case "pending":
		state := 1
		return &state, false, nil
	case "declined", "rejected":
		state := 2
		return &state, false, nil
	default:
		return nil, false, apperror.Wrap(
			apperror.KindUsage, "share received",
			fmt.Errorf(
				"invalid state %q; expected accepted, pending, declined, or all",
				value,
			),
		)
	}
}

func respondToReceivedShare(
	ctx context.Context, client Client, request Request, options Options,
) error {
	values, err := client.Sharing().ListShares(
		ctx, sharing.ShareListRequest{Received: true, AllStates: true},
	)
	if err != nil {
		return err
	}
	var selected *sharing.Share
	for index := range values {
		if values[index].ID == request.ID {
			selected = &values[index]
			break
		}
	}
	if selected == nil {
		return apperror.Wrap(
			apperror.KindNotFound, string(request.Operation),
			fmt.Errorf("received share %q was not found", request.ID),
		)
	}
	action, nextState := "accept", "accepted"
	if request.Operation == Decline {
		action, nextState = "decline", "declined"
	}
	value := map[string]any{
		"operation": action, "id": selected.ID, "path": selected.Path,
		"previousState": selected.StateName, "state": nextState,
		"dryRun": request.DryRun,
	}
	if request.DryRun {
		return output(
			options, "share-response", value,
			"Would %s received share %s (%s)\n",
			action, selected.ID, selected.Path,
		)
	}
	if request.Operation == Accept {
		err = client.Sharing().AcceptShare(ctx, request.ID)
	} else {
		err = client.Sharing().DeclineShare(ctx, request.ID)
	}
	if err != nil {
		return err
	}
	return output(
		options, "share-response", value,
		"%s received share %s (%s)\n",
		strings.ToUpper(action[:1])+action[1:], selected.ID, selected.Path,
	)
}

func listShareRoles(
	ctx context.Context,
	client Client,
	remote string,
	federated bool,
	options Options,
) error {
	if federated {
		capabilities, err := client.Sharing().Capabilities(ctx)
		if err != nil {
			return fmt.Errorf("check federation capabilities: %w", err)
		}
		if !capabilities.Sharing.Federation.Outgoing {
			return apperror.Wrap(
				apperror.KindConflict, "share federated roles",
				errors.New("outgoing federation is disabled by the server"),
			)
		}
	}
	remote = cleanRemote(remote)
	metadata, err := client.Stat(remote)
	if err != nil {
		return err
	}
	if metadata.ResourceID == "" {
		return fmt.Errorf(
			"server did not return a stable resource ID for %s", remote,
		)
	}
	var permissions graph.Permissions
	if federated {
		permissions, err = client.Graph().ListFederatedItemPermissions(
			ctx, metadata.ResourceID,
		)
	} else {
		permissions, err = client.Graph().ListItemPermissions(
			ctx, metadata.ResourceID,
		)
	}
	if err != nil {
		return err
	}
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "share-role", permissions.AllowedRoles)
	}
	for _, role := range permissions.AllowedRoles {
		if _, err := fmt.Fprintf(
			options.Out, "%-36s %-30s %s\n",
			role.ID, role.DisplayName, role.Description,
		); err != nil {
			return err
		}
	}
	return nil
}

func resolveOutgoingShare(
	ctx context.Context, client Client, shareID string, directOnly bool,
) (sharing.Share, error) {
	shareID = strings.TrimSpace(shareID)
	if shareID == "" {
		return sharing.Share{}, usageShare("share ID must not be empty")
	}
	values, err := client.Sharing().ListShares(
		ctx, sharing.ShareListRequest{},
	)
	if err != nil {
		return sharing.Share{}, err
	}
	for _, value := range values {
		if value.ID != shareID {
			continue
		}
		if directOnly && value.Type != "user" && value.Type != "group" &&
			value.Type != "federated" {
			return sharing.Share{}, usageShare(fmt.Sprintf(
				"%s is a %s share; use ocis share link update for public links",
				shareID, value.Type,
			))
		}
		if !directOnly && value.Type != "user" &&
			value.Type != "group" && value.Type != "federated" &&
			value.Type != "public_link" {
			return sharing.Share{}, usageShare(fmt.Sprintf(
				"share type %q cannot be removed with this command",
				value.Type,
			))
		}
		if value.Type != "public_link" && value.ResourceID == "" {
			return sharing.Share{}, fmt.Errorf(
				"server did not return a resource ID for share %s", shareID,
			)
		}
		return value, nil
	}
	return sharing.Share{}, apperror.Wrap(
		apperror.KindNotFound, "share",
		fmt.Errorf("outgoing share %q was not found", shareID),
	)
}

func resolveDirectRole(
	ctx context.Context, client Client, resourceID string, requested string,
	federated bool,
) (graph.Permissions, graph.RoleDefinition, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return graph.Permissions{}, graph.RoleDefinition{},
			usageShare("role must not be empty")
	}
	var permissions graph.Permissions
	var err error
	if federated {
		permissions, err = client.Graph().ListFederatedItemPermissions(
			ctx, resourceID,
		)
	} else {
		permissions, err = client.Graph().ListItemPermissions(ctx, resourceID)
	}
	if err != nil {
		return graph.Permissions{}, graph.RoleDefinition{}, err
	}
	for _, role := range permissions.AllowedRoles {
		if strings.EqualFold(role.ID, requested) ||
			strings.EqualFold(role.DisplayName, requested) {
			return permissions, role, nil
		}
	}
	target := canonicalShareRole(requested)
	if target != "" {
		for _, role := range permissions.AllowedRoles {
			if canonicalShareRole(role.DisplayName) == target &&
				normalizedShareRole(role.DisplayName) == target {
				return permissions, role, nil
			}
		}
		var matches []graph.RoleDefinition
		for _, role := range permissions.AllowedRoles {
			if canonicalShareRole(role.DisplayName) == target {
				matches = append(matches, role)
			}
		}
		if len(matches) == 1 {
			return permissions, matches[0], nil
		}
		if len(matches) > 1 {
			return graph.Permissions{}, graph.RoleDefinition{},
				usageShare(fmt.Sprintf(
					"role alias %q matches multiple server roles; use an exact role name or ID",
					requested,
				))
		}
	}
	available := make([]string, 0, len(permissions.AllowedRoles))
	for _, role := range permissions.AllowedRoles {
		available = append(
			available, fmt.Sprintf("%s (%s)", role.DisplayName, role.ID),
		)
	}
	sort.Strings(available)
	return graph.Permissions{}, graph.RoleDefinition{},
		usageShare(fmt.Sprintf(
			"role %q is not available for this resource; available roles: %s",
			requested, strings.Join(available, ", "),
		))
}

func canonicalShareRole(value string) string {
	normalized := normalizedShareRole(value)
	first, _, _ := strings.Cut(normalized, " ")
	switch first {
	case "view", "viewer", "read", "reader":
		return "view"
	case "edit", "editor", "write", "writer":
		return "edit"
	case "upload", "uploader":
		return "upload"
	case "manage", "manager":
		return "manage"
	default:
		return ""
	}
}

func normalizedShareRole(value string) string {
	return strings.TrimPrefix(
		strings.ToLower(strings.TrimSpace(value)), "can ",
	)
}

func usageShare(message string) error {
	return apperror.Wrap(apperror.KindUsage, "share", errors.New(message))
}

type directShareOutput struct {
	ID            string `json:"id"`
	Path          string `json:"path"`
	RecipientType string `json:"recipientType"`
	RecipientID   string `json:"recipientId"`
	RecipientName string `json:"recipientName"`
	RoleID        string `json:"roleId"`
	Role          string `json:"role"`
}

func createPublicLink(
	ctx context.Context, client Client, request Request, options Options,
) error {
	capabilities, err := client.Sharing().Capabilities(ctx)
	if err != nil {
		return fmt.Errorf("check sharing capabilities: %w", err)
	}
	if !capabilities.Sharing.APIEnabled ||
		!capabilities.Sharing.Public.Enabled {
		return apperror.Wrap(
			apperror.KindConflict, "share create",
			errors.New("public-link sharing is disabled by the server"),
		)
	}
	if capabilities.Sharing.Public.Password.Enforced && request.Password == "" {
		return apperror.Wrap(
			apperror.KindConflict, "share create",
			errors.New("the server requires public-link passwords; use --password"),
		)
	}
	if request.Expiration != "" &&
		!capabilities.Sharing.Public.ExpireDate.Enabled {
		return apperror.Wrap(
			apperror.KindConflict, "share create",
			errors.New("public-link expiration is disabled by the server"),
		)
	}
	permissions := request.Permissions
	if permissions == 0 {
		permissions = 1
	}
	value, err := client.Sharing().CreateLink(ctx, sharing.CreateRequest{
		Path: request.Path, SpaceID: client.SelectedSpaceID(),
		Name: request.Name, Password: request.Password,
		Expiration: request.Expiration, Permissions: permissions,
	})
	if err != nil {
		return err
	}
	return output(
		options, "share", value,
		"Created public link %s\nID: %s\n", value.URL, value.ID,
	)
}

func validateShareCreateRequest(request Request) error {
	if request.Expiration == "" {
		return nil
	}
	if _, err := time.Parse(time.DateOnly, request.Expiration); err != nil {
		return apperror.Wrap(
			apperror.KindUsage, "share create",
			errors.New("--expire must use YYYY-MM-DD"),
		)
	}
	return nil
}

func validatePublicLinkUpdate(request Request) error {
	if !request.UpdateName && !request.UpdateExpiration &&
		!request.UpdateAccess && !request.UpdatePassword {
		return usageShare("select at least one public-link property to update")
	}
	if request.UpdateExpiration && request.Expiration != "" {
		if _, err := time.Parse(time.DateOnly, request.Expiration); err != nil {
			return usageShare("--expire must use YYYY-MM-DD")
		}
	}
	if request.UpdateAccess {
		if _, err := publicLinkType(request.Permissions); err != nil {
			return err
		}
	}
	if request.UpdatePassword && !request.RemovePassword &&
		request.Password == "" && !request.DryRun {
		return usageShare("public-link password must not be empty")
	}
	return nil
}

func showPublicLink(
	ctx context.Context, client Client, id string, options Options,
) error {
	value, err := loadPublicLink(ctx, client, id)
	if err != nil {
		return err
	}
	return writePublicLink(value, options)
}

func updatePublicLink(
	ctx context.Context, client Client, request Request, options Options,
) error {
	selected, err := loadPublicLink(ctx, client, request.ID)
	if err != nil {
		return err
	}
	var update graph.LinkPermissionUpdateRequest
	var linkUpdate graph.SharingLinkUpdate
	if request.UpdateName {
		name := request.Name
		linkUpdate.DisplayName = &name
	}
	if request.UpdateAccess {
		linkType, err := publicLinkType(request.Permissions)
		if err != nil {
			return err
		}
		linkUpdate.Type = &linkType
	}
	if linkUpdate.DisplayName != nil || linkUpdate.Type != nil {
		update.Link = &linkUpdate
	}
	if request.UpdateExpiration {
		if request.Expiration == "" {
			update.ClearExpiration()
		} else {
			expiration, err := time.Parse(time.DateOnly, request.Expiration)
			if err != nil {
				return usageShare("--expire must use YYYY-MM-DD")
			}
			update.SetExpiration(expiration.UTC())
		}
	}
	if request.DryRun {
		changes := make(map[string]any, 4)
		if request.UpdateName {
			changes["name"] = request.Name
		}
		if request.UpdateExpiration {
			changes["expiration"] = request.Expiration
		}
		if request.UpdateAccess {
			changes["permissions"] = PermissionName(request.Permissions)
		}
		if request.UpdatePassword {
			changes["password"] = "set"
			if request.RemovePassword {
				changes["password"] = "remove"
			}
		}
		return output(
			options, "share",
			map[string]any{
				"operation": "update", "id": selected.ID,
				"path": selected.Path, "changes": changes, "dryRun": true,
			},
			"Would update public link %s for %s\n",
			selected.ID, selected.Path,
		)
	}

	passwordSet := request.UpdatePassword && !request.RemovePassword
	if passwordSet {
		if _, err := client.Graph().SetItemPermissionPassword(
			ctx, selected.ResourceID, selected.ID, request.Password,
		); err != nil {
			return err
		}
	}
	if !update.Empty() {
		if _, err := client.Graph().UpdateLinkPermission(
			ctx, selected.ResourceID, selected.ID, update,
		); err != nil {
			if passwordSet {
				return fmt.Errorf(
					"public-link password was updated but remaining changes failed: %w",
					err,
				)
			}
			return err
		}
	}
	if request.UpdatePassword && request.RemovePassword {
		if _, err := client.Graph().SetItemPermissionPassword(
			ctx, selected.ResourceID, selected.ID, "",
		); err != nil {
			if !update.Empty() {
				return fmt.Errorf(
					"public-link properties were updated but password removal failed: %w",
					err,
				)
			}
			return err
		}
	}
	updated, err := loadPublicLink(ctx, client, selected.ID)
	if err != nil {
		return fmt.Errorf("public link was updated but reloading it failed: %w", err)
	}
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "share", updated)
	}
	_, err = fmt.Fprintf(
		options.Out, "Updated public link %s for %s\n", updated.ID, updated.Path,
	)
	return err
}

func loadPublicLink(
	ctx context.Context, client Client, id string,
) (sharing.Link, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return sharing.Link{}, usageShare("share ID must not be empty")
	}
	value, err := client.Sharing().GetLink(ctx, id)
	if err != nil {
		return sharing.Link{}, err
	}
	if value.ResourceID == "" {
		return sharing.Link{}, fmt.Errorf(
			"server did not return a resource ID for public link %s", id,
		)
	}
	permission, err := client.Graph().GetItemPermission(
		ctx, value.ResourceID, value.ID,
	)
	if err != nil {
		return sharing.Link{}, err
	}
	if permission.Link == nil {
		return sharing.Link{}, fmt.Errorf(
			"permission %s is not a public link", value.ID,
		)
	}
	value.PasswordProtected = permission.HasPassword
	value.LinkType = permission.Link.Type
	if permission.Link.WebURL != "" {
		value.URL = permission.Link.WebURL
	}
	value.Name = permission.Link.DisplayName
	if permission.ExpirationDateTime == nil {
		value.Expiration = ""
	} else {
		value.Expiration = permission.ExpirationDateTime.Format(time.RFC3339)
	}
	if permissions := publicLinkPermissions(permission.Link.Type); permissions != 0 {
		value.Permissions = permissions
	}
	return value, nil
}

func writePublicLink(value sharing.Link, options Options) error {
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "share", value)
	}
	expiration := value.Expiration
	if expiration == "" {
		expiration = "-"
	}
	name := value.Name
	if name == "" {
		name = "-"
	}
	_, err := fmt.Fprintf(
		options.Out,
		"ID: %s\nPath: %s\nURL: %s\nName: %s\nAccess: %s\n"+
			"Expiration: %s\nPassword protected: %t\n",
		value.ID, value.Path, value.URL, name,
		PermissionName(value.Permissions), expiration,
		value.PasswordProtected,
	)
	return err
}

func publicLinkType(permissions int) (string, error) {
	switch permissions {
	case 1:
		return "view", nil
	case 5:
		return "upload", nil
	case 15:
		return "edit", nil
	default:
		return "", usageShare(fmt.Sprintf(
			"unsupported public-link permissions %d", permissions,
		))
	}
}

func publicLinkPermissions(linkType string) int {
	switch linkType {
	case "view":
		return 1
	case "upload":
		return 5
	case "edit":
		return 15
	default:
		return 0
	}
}

func listPublicLinks(
	ctx context.Context, client Client, request Request, options Options,
) error {
	values, err := client.Sharing().ListLinks(ctx, sharing.ListRequest{
		Path: request.Path, SpaceID: client.SelectedSpaceID(),
	})
	if err != nil {
		return err
	}
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "share", values)
	}
	for _, value := range values {
		expiration := value.Expiration
		if expiration == "" {
			expiration = "-"
		}
		_, _ = fmt.Fprintf(
			options.Out, "%-12s %-8s %-12s %-24s %s\n",
			value.ID, PermissionName(value.Permissions), expiration,
			value.Path, value.URL,
		)
	}
	return nil
}

// PermissionName returns a stable human label for an OCS permission mask.
func PermissionName(permissions int) string {
	switch permissions {
	case 1:
		return "read"
	case 3:
		return "edit"
	case 4:
		return "upload"
	case 5:
		return "upload"
	case 15:
		return "edit"
	default:
		return strings.TrimSpace(fmt.Sprintf("%d", permissions))
	}
}
