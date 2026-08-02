package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newAdminUserCommand(options *globalOptions) *cobra.Command {
	command := &cobra.Command{
		Use:     "user",
		Aliases: []string{"users"},
		Short:   "Manage server user accounts",
	}
	command.AddCommand(
		newAdminUserListCommand(options),
		newAdminUserInfoCommand(options),
		newAdminUserCreateCommand(options),
		newAdminUserUpdateCommand(options),
		newAdminUserStateCommand(options, true),
		newAdminUserStateCommand(options, false),
		newAdminUserDeleteCommand(options),
		newAdminUserRoleCommand(options),
	)
	return command
}

func newAdminUserListCommand(options *globalOptions) *cobra.Command {
	var search, rawSearch string
	command := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List server user accounts",
		Args:    noArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runAdmin(command, options, app.AdminRequest{
				Operation: app.AdminUserList,
				Search:    search,
				RawSearch: rawSearch,
			})
		},
	}
	command.Flags().StringVar(
		&search, "search", "",
		"search for literal text (spaces and hyphens are safe)",
	)
	command.Flags().StringVar(
		&rawSearch, "search-raw", "",
		"pass an exact server-side LibreGraph search expression",
	)
	command.MarkFlagsMutuallyExclusive("search", "search-raw")
	return command
}

func newAdminUserInfoCommand(options *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "info USERNAME_OR_ID",
		Aliases: []string{"stat"},
		Short:   "Show one server user account",
		Long: "Show one server user account. The configured identity backend " +
			"may accept an exact username; the opaque user ID is stable. " +
			"Display names are not selectors.",
		Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runAdmin(command, options, app.AdminRequest{
				Operation:  app.AdminUserInfo,
				Identifier: args[0],
			})
		},
	}
}
