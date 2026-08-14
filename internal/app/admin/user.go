package admin

import (
	"context"
	"fmt"
	"strings"

	"github.com/mzner/ocis-cli/internal/apperror"
	"github.com/mzner/ocis-cli/internal/graph"
)

func RunUserCreate(
	ctx context.Context,
	request UserCreateRequest,
	selectedProfile string,
	options Options,
) error {
	request.Username = strings.TrimSpace(request.Username)
	request.DisplayName = strings.TrimSpace(request.DisplayName)
	request.Mail = strings.TrimSpace(request.Mail)
	if request.Username == "" || request.DisplayName == "" ||
		request.Mail == "" || (!request.DryRun && request.Password == "") {
		return apperror.Wrap(
			apperror.KindUsage, "admin user create",
			fmt.Errorf(
				"username, --display-name, --email, and a non-empty password are required",
			),
		)
	}
	plan := map[string]any{
		"operation": "create", "username": request.Username,
		"displayName": request.DisplayName, "mail": request.Mail,
		"givenName": request.GivenName, "surname": request.Surname,
		"disabled":         request.Disabled,
		"passwordProvided": request.Password != "",
		"dryRun":           request.DryRun,
	}
	selected, err := options.NewClient(ctx, selectedProfile)
	if err != nil {
		return err
	}
	if err := options.RequireAccountAdmin(ctx, selected); err != nil {
		return err
	}
	if capabilities, ok := adminCapabilities(ctx, selected, options); ok &&
		capabilities.Graph.Users.CreateDisabled {
		return fmt.Errorf(
			"server advertises user creation as disabled for its identity backend",
		)
	}
	if request.DryRun {
		return output(
			options, "admin-user-change", plan,
			"Would create user %s\n", request.Username,
		)
	}
	create := graph.CreateUserRequest{
		Username: request.Username, DisplayName: request.DisplayName,
		Mail: request.Mail, GivenName: strings.TrimSpace(request.GivenName),
		Surname:  strings.TrimSpace(request.Surname),
		Password: &graph.PasswordProfile{Password: request.Password},
	}
	if request.Disabled {
		enabled := false
		create.AccountEnabled = &enabled
	}
	created, err := selected.Graph().CreateUser(ctx, create)
	if err != nil {
		return adminMutationError("user", err)
	}
	return output(
		options, "admin-user", created,
		"Created user %s (%s)\n",
		fallback(created.Username, created.DisplayName), created.ID,
	)
}

func RunUserUpdate(
	ctx context.Context,
	request UserUpdateRequest,
	selectedProfile string,
	options Options,
) error {
	request.Identifier = strings.TrimSpace(request.Identifier)
	if request.Identifier == "" {
		return apperror.Wrap(
			apperror.KindUsage, "admin user update",
			fmt.Errorf("user identifier is required"),
		)
	}
	fields := SelectedUserUpdateFields(request)
	if len(fields) == 0 {
		return apperror.Wrap(
			apperror.KindUsage, "admin user update",
			fmt.Errorf("select at least one user field to update"),
		)
	}
	if request.SetPassword && request.Password == "" {
		return apperror.Wrap(
			apperror.KindUsage, "admin user update",
			fmt.Errorf("replacement password cannot be empty"),
		)
	}
	selected, err := options.NewClient(ctx, selectedProfile)
	if err != nil {
		return err
	}
	if err := options.RequireAccountAdmin(ctx, selected); err != nil {
		return err
	}
	user, err := resolveMutationUser(ctx, selected, request.Identifier)
	if err != nil {
		return err
	}
	if capabilities, ok := adminCapabilities(ctx, selected, options); ok {
		if err := requireWritableUserFields(capabilities, fields...); err != nil {
			return err
		}
	}
	if request.DryRun {
		return output(
			options, "admin-user-change",
			map[string]any{
				"operation": "update", "id": user.ID,
				"username": user.Username, "fields": fields,
				"passwordProvided": request.SetPassword, "dryRun": true,
			},
			"Would update user %s (%s): %s\n",
			fallback(user.Username, user.DisplayName), user.ID,
			strings.Join(fields, ", "),
		)
	}
	update := graph.UpdateUserRequest{
		Username: request.Username, DisplayName: request.DisplayName,
		Mail: request.Mail, GivenName: request.GivenName,
		Surname: request.Surname,
	}
	if request.SetPassword {
		update.Password = &graph.PasswordProfile{Password: request.Password}
	}
	updated, err := selected.Graph().UpdateUser(
		ctx, user.ID, update,
	)
	if err != nil {
		return adminMutationError("user", err)
	}
	return output(
		options, "admin-user", updated,
		"Updated user %s (%s)\n",
		fallback(updated.Username, updated.DisplayName), updated.ID,
	)
}

func RunUserState(
	ctx context.Context,
	request UserStateRequest,
	selectedProfile string,
	options Options,
) error {
	request.Identifier = strings.TrimSpace(request.Identifier)
	if request.Identifier == "" {
		return apperror.Wrap(
			apperror.KindUsage, "admin user state",
			fmt.Errorf("user identifier is required"),
		)
	}
	selected, err := options.NewClient(ctx, selectedProfile)
	if err != nil {
		return err
	}
	if err := options.RequireAccountAdmin(ctx, selected); err != nil {
		return err
	}
	user, err := resolveMutationUser(ctx, selected, request.Identifier)
	if err != nil {
		return err
	}
	action := "enable"
	if !request.Enabled {
		action = "disable"
		if err := rejectSelfTarget(ctx, selected, user, action); err != nil {
			return err
		}
	}
	if capabilities, ok := adminCapabilities(ctx, selected, options); ok {
		if err := requireWritableUserFields(
			capabilities, "user.accountEnabled",
		); err != nil {
			return err
		}
	}
	if request.DryRun {
		return output(
			options, "admin-user-change",
			map[string]any{
				"operation": action, "id": user.ID,
				"username": user.Username, "dryRun": true,
			},
			"Would %s user %s (%s)\n",
			action, fallback(user.Username, user.DisplayName), user.ID,
		)
	}
	updated, err := selected.Graph().UpdateUser(
		ctx, user.ID,
		graph.UpdateUserRequest{AccountEnabled: &request.Enabled},
	)
	if err != nil {
		return adminMutationError("user account state", err)
	}
	return output(
		options, "admin-user", updated,
		"%s user %s (%s)\n",
		titleWord(action), fallback(updated.Username, updated.DisplayName),
		updated.ID,
	)
}

func RunUserDelete(
	ctx context.Context,
	request UserDeleteRequest,
	selectedProfile string,
	options Options,
) error {
	request.Identifier = strings.TrimSpace(request.Identifier)
	if request.Identifier == "" {
		return apperror.Wrap(
			apperror.KindUsage, "admin user delete",
			fmt.Errorf("user identifier is required"),
		)
	}
	selected, err := options.NewClient(ctx, selectedProfile)
	if err != nil {
		return err
	}
	if err := options.RequireAccountAdmin(ctx, selected); err != nil {
		return err
	}
	user, err := resolveMutationUser(ctx, selected, request.Identifier)
	if err != nil {
		return err
	}
	if err := rejectSelfTarget(ctx, selected, user, "delete"); err != nil {
		return err
	}
	if capabilities, ok := adminCapabilities(ctx, selected, options); ok &&
		capabilities.Graph.Users.DeleteDisabled {
		return fmt.Errorf(
			"server advertises user deletion as disabled for its identity backend",
		)
	}
	if request.DryRun {
		return output(
			options, "admin-user-change",
			map[string]any{
				"operation": "delete", "id": user.ID,
				"username": user.Username, "dryRun": true,
			},
			"Would permanently delete user %s (%s)\n",
			fallback(user.Username, user.DisplayName), user.ID,
		)
	}
	if err := selected.Graph().DeleteUser(ctx, user.ID); err != nil {
		return adminMutationError("user", err)
	}
	return output(
		options, "admin-user-change",
		map[string]any{
			"operation": "delete", "id": user.ID,
			"username": user.Username, "deleted": true,
		},
		"Deleted user %s (%s)\n",
		fallback(user.Username, user.DisplayName), user.ID,
	)
}

func SelectedUserUpdateFields(request UserUpdateRequest) []string {
	fields := make([]string, 0, 6)
	for _, field := range []struct {
		value *string
		name  string
	}{
		{request.Username, "user.onPremisesSamAccountName"},
		{request.DisplayName, "user.displayName"},
		{request.Mail, "user.mail"},
		{request.GivenName, "user.givenName"},
		{request.Surname, "user.surname"},
	} {
		if field.value != nil {
			fields = append(fields, field.name)
		}
	}
	if request.SetPassword {
		fields = append(fields, "user.passwordProfile")
	}
	return fields
}

func titleWord(value string) string {
	if value == "" {
		return ""
	}
	return strings.ToUpper(value[:1]) + value[1:]
}
