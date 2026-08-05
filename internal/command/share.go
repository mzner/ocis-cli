package command

import (
	"fmt"
	"strings"

	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newShareCommand(options *globalOptions) *cobra.Command {
	command := &cobra.Command{
		Use: "share", Short: "Manage direct shares and public links",
	}
	command.AddCommand(
		newShareCreateCommand(options),
		newShareListCommand(options, false),
		newShareRevokeCommand(options),
		newShareRecipientCommand(options, "user"),
		newShareRecipientCommand(options, "group"),
		newShareRolesCommand(options),
		newShareUpdateCommand(options),
		newShareRemoveCommand(options),
		newShareOverviewCommand(options),
		newShareReceivedCommand(options),
		newShareResponseCommand(options, "accept"),
		newShareResponseCommand(options, "decline"),
		newShareLinkCommand(options),
	)
	return command
}

func newShareOverviewCommand(options *globalOptions) *cobra.Command {
	var direction, state string
	command := &cobra.Command{
		Use: "overview", Short: "List outgoing and received shares across Spaces",
		Args: noArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runShare(command, options, app.ShareRequest{
				Operation: app.ShareOverview, Direction: direction, State: state,
			})
		},
	}
	command.Flags().StringVar(
		&direction, "direction", "all",
		"filter by direction: outgoing, received, or all",
	)
	command.Flags().StringVar(
		&state, "state", "current",
		"filter by state: current, accepted, pending, declined, or all",
	)
	return command
}

func newShareLinkCommand(options *globalOptions) *cobra.Command {
	command := &cobra.Command{
		Use: "link", Aliases: []string{"links"},
		Short: "Manage public links",
	}
	command.AddCommand(
		newShareCreateCommand(options),
		newShareListCommand(options, true),
		newShareLinkInfoCommand(options),
		newShareLinkUpdateCommand(options),
		newShareRevokeCommand(options),
	)
	return command
}

func newShareCreateCommand(options *globalOptions) *cobra.Command {
	var name, expiration, permissions string
	var password, dryRun bool
	command := &cobra.Command{
		Use: "create REMOTE_PATH", Short: "Create a public link", Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			permissionValue, err := parsePublicLinkPermissions(permissions)
			if err != nil {
				return usageError("share create", err.Error())
			}
			secret := ""
			if password && !dryRun {
				secret, err = readSecret(
					command, "OCIS_SHARE_PASSWORD", "Public-link password: ",
				)
				if err != nil {
					return err
				}
			}
			return runShare(command, options, app.ShareRequest{
				Operation: app.ShareCreate, Path: args[0], Name: name,
				Password: secret, Expiration: expiration,
				Permissions: permissionValue, DryRun: dryRun,
			})
		},
	}
	command.Flags().StringVar(&name, "name", "", "human-readable link name")
	command.Flags().StringVar(&expiration, "expire", "", "expiration date in YYYY-MM-DD")
	command.Flags().StringVar(
		&permissions, "permissions", "read", "link access: read, upload, or edit",
	)
	command.Flags().BoolVar(
		&password, "password", false,
		"protect the link using OCIS_SHARE_PASSWORD or a secure prompt",
	)
	command.Flags().BoolVar(&dryRun, "dry-run", false, "show the planned link without creating it")
	return command
}

func newShareListCommand(
	options *globalOptions, linksOnly bool,
) *cobra.Command {
	short := "List outgoing shares"
	if linksOnly {
		short = "List public links"
	}
	return &cobra.Command{
		Use: "list [REMOTE_PATH]", Aliases: []string{"ls"},
		Short: short, Args: maximumArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			remote := ""
			if len(args) == 1 {
				remote = args[0]
			}
			return runShare(command, options, app.ShareRequest{
				Operation: app.ShareList, Path: remote, LinksOnly: linksOnly,
			})
		},
	}
}

func newShareRevokeCommand(options *globalOptions) *cobra.Command {
	var dryRun bool
	command := &cobra.Command{
		Use:   "revoke ID",
		Short: "Revoke a public link", Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runShare(command, options, app.ShareRequest{
				Operation: app.ShareRevoke, ID: args[0], DryRun: dryRun,
			})
		},
	}
	command.Flags().BoolVar(&dryRun, "dry-run", false, "show the planned revocation")
	return command
}

func newShareLinkInfoCommand(options *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use: "info SHARE_ID", Aliases: []string{"stat"},
		Short: "Show public-link details", Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runShare(command, options, app.ShareRequest{
				Operation: app.ShareLinkInfo, ID: args[0],
			})
		},
	}
}

func newShareLinkUpdateCommand(options *globalOptions) *cobra.Command {
	var name, expiration, permissions string
	var removeExpiration, password, removePassword, dryRun bool
	command := &cobra.Command{
		Use: "update SHARE_ID", Short: "Update a public link",
		Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			updateName := command.Flags().Changed("name")
			updateExpiration := command.Flags().Changed("expire") ||
				removeExpiration
			updateAccess := command.Flags().Changed("permissions")
			updatePassword := password || removePassword
			permissionValue := 0
			var err error
			if updateAccess {
				permissionValue, err = parsePublicLinkPermissions(permissions)
				if err != nil {
					return usageError("share link update", err.Error())
				}
			}
			if removeExpiration {
				expiration = ""
			}
			secret := ""
			if password && !dryRun {
				secret, err = readSecret(
					command, "OCIS_SHARE_PASSWORD", "Public-link password: ",
				)
				if err != nil {
					return err
				}
			}
			return runShare(command, options, app.ShareRequest{
				Operation: app.ShareLinkUpdate, ID: args[0],
				Name: name, UpdateName: updateName,
				Expiration: expiration, UpdateExpiration: updateExpiration,
				Permissions: permissionValue, UpdateAccess: updateAccess,
				Password: secret, UpdatePassword: updatePassword,
				RemovePassword: removePassword, DryRun: dryRun,
			})
		},
	}
	command.Flags().StringVar(&name, "name", "", "set or clear the link name")
	command.Flags().StringVar(
		&expiration, "expire", "", "set expiration date in YYYY-MM-DD",
	)
	command.Flags().BoolVar(
		&removeExpiration, "remove-expiration", false,
		"remove the expiration date",
	)
	command.Flags().StringVar(
		&permissions, "permissions", "",
		"set link access to read, upload, or edit",
	)
	command.Flags().BoolVar(
		&password, "password", false,
		"set a password using OCIS_SHARE_PASSWORD or a secure prompt",
	)
	command.Flags().BoolVar(
		&removePassword, "remove-password", false,
		"remove the public-link password",
	)
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"resolve the link and show changes without updating it",
	)
	command.MarkFlagsMutuallyExclusive("expire", "remove-expiration")
	command.MarkFlagsMutuallyExclusive("password", "remove-password")
	return command
}

func newShareRecipientCommand(
	options *globalOptions, recipientType string,
) *cobra.Command {
	command := &cobra.Command{
		Use: recipientType, Short: "Share with a " + recipientType,
	}
	command.AddCommand(newShareAddCommand(options, recipientType))
	return command
}

func newShareAddCommand(
	options *globalOptions, recipientType string,
) *cobra.Command {
	var role string
	var recipientIsID, dryRun bool
	command := &cobra.Command{
		Use:   "add REMOTE_PATH RECIPIENT",
		Short: "Grant a " + recipientType + " access to a remote resource",
		Args:  exactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			return runShare(command, options, app.ShareRequest{
				Operation: app.ShareDirectAdd, Path: args[0],
				Recipient: args[1], RecipientType: recipientType,
				RecipientIsID: recipientIsID, Role: role, DryRun: dryRun,
			})
		},
	}
	command.Flags().StringVar(
		&role, "role", "viewer",
		"server-advertised role name, ID, or unambiguous alias",
	)
	command.Flags().BoolVar(
		&recipientIsID, "recipient-id", false,
		"treat RECIPIENT as an opaque Graph ID",
	)
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"resolve the resource, recipient, and role without sharing",
	)
	return command
}

func newShareRolesCommand(options *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "roles REMOTE_PATH",
		Short: "List server-advertised roles for a remote resource",
		Args:  exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runShare(command, options, app.ShareRequest{
				Operation: app.ShareRoles, Path: args[0],
			})
		},
	}
}

func newShareUpdateCommand(options *globalOptions) *cobra.Command {
	var role string
	var dryRun bool
	command := &cobra.Command{
		Use: "update SHARE_ID", Short: "Change a direct share role",
		Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runShare(command, options, app.ShareRequest{
				Operation: app.ShareDirectUpdate, ID: args[0],
				Role: role, DryRun: dryRun,
			})
		},
	}
	command.Flags().StringVar(
		&role, "role", "",
		"server-advertised role name, ID, or unambiguous alias",
	)
	_ = command.MarkFlagRequired("role")
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"resolve the share and role without updating it",
	)
	return command
}

func newShareRemoveCommand(options *globalOptions) *cobra.Command {
	var dryRun, yes bool
	command := &cobra.Command{
		Use: "remove SHARE_ID", Aliases: []string{"rm"},
		Short: "Remove a direct share or public link", Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if !yes && !dryRun {
				confirmed, err := confirmAction(
					command, "Remove share "+args[0]+"?",
				)
				if err != nil || !confirmed {
					return err
				}
			}
			return runShare(command, options, app.ShareRequest{
				Operation: app.ShareRemove, ID: args[0],
				Confirmed: true, DryRun: dryRun,
			})
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"resolve the share without removing it",
	)
	command.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	return command
}

func newShareReceivedCommand(options *globalOptions) *cobra.Command {
	var state string
	command := &cobra.Command{
		Use: "received [REMOTE_PATH]", Short: "List shares received by you",
		Args: maximumArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			remote := ""
			if len(args) == 1 {
				remote = args[0]
			}
			return runShare(command, options, app.ShareRequest{
				Operation: app.ShareReceived, Path: remote, State: state,
			})
		},
	}
	command.Flags().StringVar(
		&state, "state", "",
		"filter by state: accepted, pending, declined, or all",
	)
	return command
}

func newShareResponseCommand(
	options *globalOptions, action string,
) *cobra.Command {
	var dryRun bool
	operation := app.ShareAccept
	short := "Accept a received share"
	if action == "decline" {
		operation = app.ShareDecline
		short = "Decline a received share"
	}
	command := &cobra.Command{
		Use: action + " SHARE_ID", Short: short, Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runShare(command, options, app.ShareRequest{
				Operation: operation, ID: args[0], DryRun: dryRun,
			})
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"resolve the received share without changing its state",
	)
	return command
}

func parsePublicLinkPermissions(value string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "read":
		return 1, nil
	case "upload":
		return 5, nil
	case "edit":
		return 15, nil
	default:
		return 0, fmt.Errorf(
			"invalid permissions %q; expected read, upload, or edit", value,
		)
	}
}
