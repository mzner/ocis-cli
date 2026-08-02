package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newAdminGroupCommand(options *globalOptions) *cobra.Command {
	command := &cobra.Command{
		Use:     "group",
		Aliases: []string{"groups"},
		Short:   "Manage server groups",
	}
	command.AddCommand(
		newAdminGroupListCommand(options),
		newAdminGroupInfoCommand(options),
		newAdminGroupCreateCommand(options),
		newAdminGroupUpdateCommand(options),
		newAdminGroupDeleteCommand(options),
		newAdminGroupMemberCommand(options),
	)
	return command
}

func newAdminGroupListCommand(options *globalOptions) *cobra.Command {
	var search, rawSearch string
	command := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List server groups",
		Args:    noArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runAdmin(command, options, app.AdminRequest{
				Operation: app.AdminGroupList,
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

func newAdminGroupInfoCommand(options *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "info NAME_OR_ID",
		Aliases: []string{"stat"},
		Short:   "Show one server group",
		Long: "Show one server group. The configured identity backend may " +
			"accept an exact group name; the opaque group ID is stable.",
		Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runAdmin(command, options, app.AdminRequest{
				Operation:  app.AdminGroupInfo,
				Identifier: args[0],
			})
		},
	}
}

func newAdminGroupMemberCommand(options *globalOptions) *cobra.Command {
	command := &cobra.Command{
		Use:     "member",
		Aliases: []string{"members"},
		Short:   "Inspect direct user membership",
		Long: "Inspect direct users in a group. oCIS does not support nested " +
			"groups as group members.",
	}
	command.AddCommand(
		newAdminGroupMemberListCommand(options),
		newAdminGroupMemberAddCommand(options),
		newAdminGroupMemberRemoveCommand(options),
	)
	return command
}

func newAdminGroupMemberListCommand(options *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "list GROUP_NAME_OR_ID",
		Aliases: []string{"ls"},
		Short:   "List a group's direct user members",
		Args:    exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runAdmin(command, options, app.AdminRequest{
				Operation:  app.AdminGroupMemberList,
				Identifier: args[0],
			})
		},
	}
}
