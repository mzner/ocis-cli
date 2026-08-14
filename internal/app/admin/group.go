package admin

import (
	"context"
	"fmt"
	"strings"

	"github.com/mzner/ocis-cli/internal/apperror"
	"github.com/mzner/ocis-cli/internal/graph"
)

func RunGroupCreate(
	ctx context.Context,
	request GroupCreateRequest,
	selectedProfile string,
	options Options,
) error {
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" {
		return apperror.Wrap(
			apperror.KindUsage, "admin group create",
			fmt.Errorf("group name is required"),
		)
	}
	selected, err := options.NewClient(ctx, selectedProfile)
	if err != nil {
		return err
	}
	if err := options.RequireAccountAdmin(ctx, selected); err != nil {
		return err
	}
	if request.DryRun {
		return output(
			options, "admin-group-change",
			map[string]any{
				"operation": "create", "name": request.Name, "dryRun": true,
			},
			"Would create group %s\n", request.Name,
		)
	}
	created, err := selected.Graph().CreateGroup(
		ctx, graph.CreateGroupRequest{DisplayName: request.Name},
	)
	if err != nil {
		return adminMutationError("group", err)
	}
	return output(
		options, "admin-group", created,
		"Created group %s (%s)\n", created.DisplayName, created.ID,
	)
}

func RunGroupUpdate(
	ctx context.Context,
	request GroupUpdateRequest,
	selectedProfile string,
	options Options,
) error {
	request.Identifier = strings.TrimSpace(request.Identifier)
	request.Name = strings.TrimSpace(request.Name)
	if request.Identifier == "" || request.Name == "" {
		return apperror.Wrap(
			apperror.KindUsage, "admin group update",
			fmt.Errorf("group identifier and --name are required"),
		)
	}
	selected, err := options.NewClient(ctx, selectedProfile)
	if err != nil {
		return err
	}
	if err := options.RequireAccountAdmin(ctx, selected); err != nil {
		return err
	}
	group, err := resolveMutationGroup(ctx, selected, request.Identifier)
	if err != nil {
		return err
	}
	if err := rejectReadOnlyGroup(group); err != nil {
		return err
	}
	if request.DryRun {
		return output(
			options, "admin-group-change",
			map[string]any{
				"operation": "update", "id": group.ID,
				"oldName": group.DisplayName, "name": request.Name,
				"dryRun": true,
			},
			"Would rename group %s (%s) to %s\n",
			group.DisplayName, group.ID, request.Name,
		)
	}
	name := request.Name
	if err := selected.Graph().UpdateGroup(
		ctx, group.ID, graph.UpdateGroupRequest{DisplayName: &name},
	); err != nil {
		return adminMutationError("group", err)
	}
	return output(
		options, "admin-group-change",
		map[string]any{
			"operation": "update", "id": group.ID,
			"oldName": group.DisplayName, "name": request.Name,
			"updated": true,
		},
		"Renamed group %s (%s) to %s\n",
		group.DisplayName, group.ID, request.Name,
	)
}

func RunGroupDelete(
	ctx context.Context,
	request GroupDeleteRequest,
	selectedProfile string,
	options Options,
) error {
	request.Identifier = strings.TrimSpace(request.Identifier)
	if request.Identifier == "" {
		return apperror.Wrap(
			apperror.KindUsage, "admin group delete",
			fmt.Errorf("group identifier is required"),
		)
	}
	selected, err := options.NewClient(ctx, selectedProfile)
	if err != nil {
		return err
	}
	if err := options.RequireAccountAdmin(ctx, selected); err != nil {
		return err
	}
	group, err := resolveMutationGroup(ctx, selected, request.Identifier)
	if err != nil {
		return err
	}
	if err := rejectReadOnlyGroup(group); err != nil {
		return err
	}
	if request.DryRun {
		return output(
			options, "admin-group-change",
			map[string]any{
				"operation": "delete", "id": group.ID,
				"name": group.DisplayName, "dryRun": true,
			},
			"Would permanently delete group %s (%s)\n",
			group.DisplayName, group.ID,
		)
	}
	if err := selected.Graph().DeleteGroup(ctx, group.ID); err != nil {
		return adminMutationError("group", err)
	}
	return output(
		options, "admin-group-change",
		map[string]any{
			"operation": "delete", "id": group.ID,
			"name": group.DisplayName, "deleted": true,
		},
		"Deleted group %s (%s)\n", group.DisplayName, group.ID,
	)
}

func RunGroupMemberMutation(
	ctx context.Context,
	request GroupMemberMutationRequest,
	selectedProfile string,
	options Options,
) error {
	request.Group = strings.TrimSpace(request.Group)
	request.User = strings.TrimSpace(request.User)
	if request.Group == "" || request.User == "" {
		return apperror.Wrap(
			apperror.KindUsage, "admin group member",
			fmt.Errorf("group and user identifiers are required"),
		)
	}
	selected, err := options.NewClient(ctx, selectedProfile)
	if err != nil {
		return err
	}
	if err := options.RequireAccountAdmin(ctx, selected); err != nil {
		return err
	}
	group, err := resolveMutationGroup(ctx, selected, request.Group)
	if err != nil {
		return err
	}
	if err := rejectReadOnlyGroup(group); err != nil {
		return err
	}
	user, err := resolveMutationUser(ctx, selected, request.User)
	if err != nil {
		return err
	}
	action := "add"
	if request.Remove {
		action = "remove"
	}
	if request.DryRun {
		return output(
			options, "admin-group-member-change",
			map[string]any{
				"operation": action, "groupId": group.ID,
				"group": group.DisplayName, "userId": user.ID,
				"username": user.Username, "dryRun": true,
			},
			"Would %s user %s (%s) %s group %s (%s)\n",
			action, fallback(user.Username, user.DisplayName), user.ID,
			map[bool]string{true: "from", false: "to"}[request.Remove],
			group.DisplayName, group.ID,
		)
	}
	if request.Remove {
		err = selected.Graph().RemoveGroupMember(ctx, group.ID, user.ID)
	} else {
		err = selected.Graph().AddGroupMember(ctx, group.ID, user.ID)
	}
	if err != nil {
		return adminMutationError("group membership", err)
	}
	return output(
		options, "admin-group-member-change",
		map[string]any{
			"operation": action, "groupId": group.ID,
			"group": group.DisplayName, "userId": user.ID,
			"username": user.Username, "changed": true,
		},
		"%s user %s (%s) %s group %s (%s)\n",
		titleWord(action), fallback(user.Username, user.DisplayName), user.ID,
		map[bool]string{true: "from", false: "to"}[request.Remove],
		group.DisplayName, group.ID,
	)
}
