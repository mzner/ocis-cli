package command

import (
	"strings"

	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newAdminUserCreateCommand(options *globalOptions) *cobra.Command {
	var displayName, mail, givenName, surname string
	var disabled, dryRun bool
	command := &cobra.Command{
		Use:   "create USERNAME",
		Short: "Create a server user account",
		Args:  exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			var password string
			var err error
			if !dryRun {
				password, err = readSecret(
					command, "OCIS_USER_PASSWORD", "Initial password: ",
				)
				if err != nil {
					return err
				}
			}
			return runAdminUserCreate(
				command, options, app.AdminUserCreateRequest{
					Username: args[0], DisplayName: displayName,
					Mail: mail, GivenName: givenName, Surname: surname,
					Password: password, Disabled: disabled, DryRun: dryRun,
				},
			)
		},
	}
	command.Flags().StringVar(
		&displayName, "display-name", "", "user display name (required)",
	)
	command.Flags().StringVar(&mail, "email", "", "user email address (required)")
	command.Flags().StringVar(&givenName, "given-name", "", "user given name")
	command.Flags().StringVar(&surname, "surname", "", "user surname")
	command.Flags().BoolVar(
		&disabled, "disabled", false, "create the account disabled",
	)
	command.Flags().BoolVar(
		&dryRun, "dry-run", false, "show the operation without creating the user",
	)
	_ = command.MarkFlagRequired("display-name")
	_ = command.MarkFlagRequired("email")
	return command
}

func newAdminUserUpdateCommand(options *globalOptions) *cobra.Command {
	var username, displayName, mail, givenName, surname string
	var setPassword, dryRun bool
	command := &cobra.Command{
		Use:   "update USERNAME_OR_ID",
		Short: "Update selected user account fields",
		Args:  exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			request := app.AdminUserUpdateRequest{
				Identifier: args[0], SetPassword: setPassword, DryRun: dryRun,
			}
			request.Username = changedString(command, "username", username)
			request.DisplayName = changedString(
				command, "display-name", displayName,
			)
			request.Mail = changedString(command, "email", mail)
			request.GivenName = changedString(
				command, "given-name", givenName,
			)
			request.Surname = changedString(command, "surname", surname)
			if setPassword && !dryRun {
				password, err := readSecret(
					command, "OCIS_USER_PASSWORD", "New password: ",
				)
				if err != nil {
					return err
				}
				request.Password = password
			}
			return runAdminUserUpdate(command, options, request)
		},
	}
	command.Flags().StringVar(&username, "username", "", "new username")
	command.Flags().StringVar(
		&displayName, "display-name", "", "new display name",
	)
	command.Flags().StringVar(&mail, "email", "", "new email address")
	command.Flags().StringVar(&givenName, "given-name", "", "new given name")
	command.Flags().StringVar(&surname, "surname", "", "new surname")
	command.Flags().BoolVar(
		&setPassword, "set-password", false,
		"read a replacement password securely or from OCIS_USER_PASSWORD",
	)
	command.Flags().BoolVar(
		&dryRun, "dry-run", false, "show the operation without updating the user",
	)
	return command
}

func newAdminUserStateCommand(
	options *globalOptions, enabled bool,
) *cobra.Command {
	action := "enable"
	if !enabled {
		action = "disable"
	}
	var dryRun, yes bool
	command := &cobra.Command{
		Use:   action + " USERNAME_OR_ID",
		Short: titleWord(action) + " a server user account",
		Args:  exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if !enabled && !yes && !dryRun {
				confirmed, err := confirmAction(
					command, "Disable user "+args[0]+"?",
				)
				if err != nil || !confirmed {
					return err
				}
			}
			return runAdminUserState(
				command, options, app.AdminUserStateRequest{
					Identifier: args[0], Enabled: enabled, DryRun: dryRun,
				},
			)
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"resolve the user and show the operation without changing it",
	)
	if !enabled {
		command.Flags().BoolVar(
			&yes, "yes", false, "skip the confirmation prompt",
		)
	}
	return command
}

func newAdminUserDeleteCommand(options *globalOptions) *cobra.Command {
	var dryRun, yes bool
	command := &cobra.Command{
		Use:   "delete USERNAME_OR_ID",
		Short: "Permanently delete a server user account",
		Args:  exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if !yes && !dryRun {
				confirmed, err := confirmAction(
					command, "Permanently delete user "+args[0]+"?",
				)
				if err != nil || !confirmed {
					return err
				}
			}
			return runAdminUserDelete(
				command, options, app.AdminUserDeleteRequest{
					Identifier: args[0], DryRun: dryRun,
				},
			)
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"resolve the user and show the operation without deleting it",
	)
	command.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	return command
}

func changedString(
	command *cobra.Command, name, value string,
) *string {
	if !command.Flags().Changed(name) {
		return nil
	}
	copy := value
	return &copy
}

func runAdminUserCreate(
	command *cobra.Command,
	options *globalOptions,
	request app.AdminUserCreateRequest,
) error {
	return app.RunAdminUserCreateWithOptions(
		command.Context(), request, options.profile, options.runOptions(command),
	)
}

func runAdminUserUpdate(
	command *cobra.Command,
	options *globalOptions,
	request app.AdminUserUpdateRequest,
) error {
	return app.RunAdminUserUpdateWithOptions(
		command.Context(), request, options.profile, options.runOptions(command),
	)
}

func runAdminUserState(
	command *cobra.Command,
	options *globalOptions,
	request app.AdminUserStateRequest,
) error {
	return app.RunAdminUserStateWithOptions(
		command.Context(), request, options.profile, options.runOptions(command),
	)
}

func runAdminUserDelete(
	command *cobra.Command,
	options *globalOptions,
	request app.AdminUserDeleteRequest,
) error {
	return app.RunAdminUserDeleteWithOptions(
		command.Context(), request, options.profile, options.runOptions(command),
	)
}

func titleWord(value string) string {
	if value == "" {
		return ""
	}
	return strings.ToUpper(value[:1]) + value[1:]
}
