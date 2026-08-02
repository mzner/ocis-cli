package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/mzner/ocis-cli/internal/apperror"
	"github.com/mzner/ocis-cli/internal/graph"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

func runAdmin(
	ctx context.Context,
	request AdminRequest,
	selectedProfile string,
	options RunOptions,
) error {
	if options.Space != "" {
		return apperror.Wrap(
			apperror.KindUsage, "admin",
			errors.New(
				"--space cannot be used with administrative inventory commands",
			),
		)
	}
	if err := validateAdminRequest(request); err != nil {
		return apperror.Wrap(apperror.KindUsage, "admin", err)
	}
	client, err := newClientWithOptions(ctx, selectedProfile, options)
	if err != nil {
		return err
	}
	switch request.Operation {
	case AdminUserList:
		if err := requireAccountAdminMFA(ctx, client); err != nil {
			return err
		}
		return listAdminUsers(
			ctx, client, adminDirectorySearch(request), options,
		)
	case AdminUserInfo:
		if err := requireAccountAdminMFA(ctx, client); err != nil {
			return err
		}
		return showAdminUser(ctx, client, request.Identifier, options)
	case AdminGroupList:
		if err := requireAccountAdminMFA(ctx, client); err != nil {
			return err
		}
		return listAdminGroups(
			ctx, client, adminDirectorySearch(request), options,
		)
	case AdminGroupInfo:
		if err := requireAccountAdminMFA(ctx, client); err != nil {
			return err
		}
		return showAdminGroup(ctx, client, request.Identifier, options)
	case AdminGroupMemberList:
		if err := requireAccountAdminMFA(ctx, client); err != nil {
			return err
		}
		return listAdminGroupMembers(
			ctx, client, request.Identifier, options,
		)
	case AdminSpaceList:
		return listAdminSpaces(ctx, client, options)
	case AdminSpaceInfo:
		return showAdminSpace(ctx, client, request.Identifier, options)
	default:
		return apperror.Wrap(
			apperror.KindUsage, "admin",
			fmt.Errorf("unknown admin operation %q", request.Operation),
		)
	}
}

func validateAdminRequest(request AdminRequest) error {
	hasSearch := strings.TrimSpace(request.Search) != ""
	hasRawSearch := strings.TrimSpace(request.RawSearch) != ""
	if hasSearch && hasRawSearch {
		return errors.New("--search and --search-raw cannot be used together")
	}
	if strings.ContainsRune(request.Search, '"') {
		return errors.New(
			`--search cannot contain a double quote; use --search-raw for an exact LibreGraph expression`,
		)
	}
	if request.Operation != AdminUserList &&
		request.Operation != AdminGroupList &&
		(hasSearch || hasRawSearch) {
		return errors.New("search is only supported for user and group lists")
	}
	switch request.Operation {
	case AdminUserList, AdminGroupList:
		return nil
	case AdminSpaceList:
		return nil
	case AdminUserInfo, AdminGroupInfo, AdminGroupMemberList, AdminSpaceInfo:
		if strings.TrimSpace(request.Identifier) == "" {
			return errors.New("identifier is required")
		}
		return nil
	default:
		return fmt.Errorf("unknown admin operation %q", request.Operation)
	}
}

func adminDirectorySearch(request AdminRequest) graph.DirectorySearch {
	if strings.TrimSpace(request.RawSearch) != "" {
		return graph.DirectorySearch{
			Value: request.RawSearch,
			Mode:  graph.DirectorySearchRaw,
		}
	}
	return graph.DirectorySearch{
		Value: request.Search,
		Mode:  graph.DirectorySearchLiteral,
	}
}

func listAdminUsers(
	ctx context.Context,
	client *client,
	search graph.DirectorySearch,
	options RunOptions,
) error {
	users, err := client.graphClient().ListUsers(ctx, search)
	if err != nil {
		return unavailableAdminList("user inventory", err)
	}
	sort.Slice(users, func(i, j int) bool {
		left := strings.ToLower(users[i].DisplayName)
		right := strings.ToLower(users[j].DisplayName)
		if left == right {
			return users[i].ID < users[j].ID
		}
		return left < right
	})
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "admin-user", users)
	}
	return writeAdminTable(options.Out, func(writer io.Writer) error {
		if _, err := fmt.Fprintln(
			writer, "STATUS\tUSERNAME\tDISPLAY NAME\tEMAIL\tID",
		); err != nil {
			return err
		}
		for _, user := range users {
			if _, err := fmt.Fprintf(
				writer, "%s\t%s\t%s\t%s\t%s\n",
				adminUserStatus(user.AccountEnabled),
				user.Username, user.DisplayName, user.Mail, user.ID,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

func showAdminUser(
	ctx context.Context,
	client *client,
	identifier string,
	options RunOptions,
) error {
	user, err := client.graphClient().GetUser(ctx, identifier)
	if err != nil {
		return err
	}
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "admin-user", user)
	}
	return writeAdminUser(options.Out, user)
}

func writeAdminUser(writer io.Writer, user graph.DirectoryUser) error {
	fields := [][2]string{
		{"ID", user.ID},
		{"Username", user.Username},
		{"Display name", user.DisplayName},
		{"Email", user.Mail},
		{"Account", adminUserStatus(user.AccountEnabled)},
		{"Type", user.UserType},
		{"Given name", user.GivenName},
		{"Surname", user.Surname},
		{"Language", user.PreferredLanguage},
	}
	for _, field := range fields {
		if field[1] == "" {
			continue
		}
		if _, err := fmt.Fprintf(
			writer, "%-14s %s\n", field[0]+":", field[1],
		); err != nil {
			return err
		}
	}
	return nil
}

func adminUserStatus(enabled *bool) string {
	switch {
	case enabled == nil:
		return "unknown"
	case *enabled:
		return "enabled"
	default:
		return "disabled"
	}
}

func listAdminGroups(
	ctx context.Context,
	client *client,
	search graph.DirectorySearch,
	options RunOptions,
) error {
	groups, err := client.graphClient().ListGroups(ctx, search)
	if err != nil {
		return unavailableAdminList("group inventory", err)
	}
	sort.Slice(groups, func(i, j int) bool {
		left := strings.ToLower(groups[i].DisplayName)
		right := strings.ToLower(groups[j].DisplayName)
		if left == right {
			return groups[i].ID < groups[j].ID
		}
		return left < right
	})
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "admin-group", groups)
	}
	return writeAdminTable(options.Out, func(writer io.Writer) error {
		if _, err := fmt.Fprintln(
			writer, "ACCESS\tNAME\tDESCRIPTION\tID",
		); err != nil {
			return err
		}
		for _, group := range groups {
			if _, err := fmt.Fprintf(
				writer, "%s\t%s\t%s\t%s\n",
				adminGroupAccess(group), group.DisplayName,
				group.Description, group.ID,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

func showAdminGroup(
	ctx context.Context,
	client *client,
	identifier string,
	options RunOptions,
) error {
	group, err := client.graphClient().GetGroup(ctx, identifier)
	if err != nil {
		return err
	}
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "admin-group", group)
	}
	return writeAdminGroup(options.Out, group)
}

func writeAdminGroup(writer io.Writer, group graph.DirectoryGroup) error {
	fields := [][2]string{
		{"ID", group.ID},
		{"Name", group.DisplayName},
		{"Description", group.Description},
		{"Access", adminGroupAccess(group)},
	}
	for _, field := range fields {
		if field[1] == "" {
			continue
		}
		if _, err := fmt.Fprintf(
			writer, "%-13s %s\n", field[0]+":", field[1],
		); err != nil {
			return err
		}
	}
	if len(group.GroupTypes) > 0 {
		_, err := fmt.Fprintf(
			writer, "%-13s %s\n", "Types:",
			strings.Join(group.GroupTypes, ", "),
		)
		return err
	}
	return nil
}

func adminGroupAccess(group graph.DirectoryGroup) string {
	for _, groupType := range group.GroupTypes {
		if strings.EqualFold(groupType, "ReadOnly") {
			return "read-only"
		}
	}
	return "writable"
}

func listAdminGroupMembers(
	ctx context.Context,
	client *client,
	identifier string,
	options RunOptions,
) error {
	group, err := client.graphClient().GetGroup(ctx, identifier)
	if err != nil {
		return err
	}
	if strings.TrimSpace(group.ID) == "" {
		return fmt.Errorf(
			"server returned group %q without a stable ID",
			group.DisplayName,
		)
	}
	members, err := client.graphClient().ListGroupMembers(ctx, group.ID)
	if err != nil {
		return err
	}
	sort.Slice(members, func(i, j int) bool {
		left := strings.ToLower(members[i].DisplayName)
		right := strings.ToLower(members[j].DisplayName)
		if left == right {
			return members[i].ID < members[j].ID
		}
		return left < right
	})
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "admin-group-member", members)
	}
	if _, err := fmt.Fprintf(
		options.Out, "Group: %s (%s)\n", group.DisplayName, group.ID,
	); err != nil {
		return err
	}
	return writeAdminTable(options.Out, func(writer io.Writer) error {
		if _, err := fmt.Fprintln(
			writer, "USERNAME\tDISPLAY NAME\tEMAIL\tID",
		); err != nil {
			return err
		}
		for _, user := range members {
			if _, err := fmt.Fprintf(
				writer, "%s\t%s\t%s\t%s\n",
				user.Username, user.DisplayName, user.Mail, user.ID,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

func listAdminSpaces(
	ctx context.Context, client *client, options RunOptions,
) error {
	spaces, err := client.graphClient().ListDrives(ctx)
	if err != nil {
		return unavailableAdminList("global Space inventory", err)
	}
	sort.Slice(spaces, func(i, j int) bool {
		left := strings.ToLower(spaces[i].Name)
		right := strings.ToLower(spaces[j].Name)
		if left == right {
			return spaces[i].ID < spaces[j].ID
		}
		return left < right
	})
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "admin-space", spaces)
	}
	return writeAdminTable(options.Out, func(writer io.Writer) error {
		if _, err := fmt.Fprintln(
			writer, "TYPE\tNAME\tALIAS\tOWNER\tID",
		); err != nil {
			return err
		}
		for _, value := range spaces {
			if _, err := fmt.Fprintf(
				writer, "%s\t%s\t%s\t%s\t%s\n",
				value.DriveType, value.Name, value.DriveAlias,
				fallback(
					value.Owner.User.DisplayName, value.Owner.User.ID,
				),
				value.ID,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

func showAdminSpace(
	ctx context.Context,
	client *client,
	identifier string,
	options RunOptions,
) error {
	spaces, err := client.graphClient().ListDrives(ctx)
	if err != nil {
		return unavailableAdminList("global Space inventory", err)
	}
	selected, err := resolveAdminSpace(spaces, identifier)
	if err != nil {
		return err
	}
	details, err := loadSpaceDetails(ctx, client, selected)
	if err != nil {
		return err
	}
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "admin-space", details)
	}
	return writeSpaceDetails(options, details)
}

func resolveAdminSpace(spaces []space, identifier string) (space, error) {
	var matches []space
	for _, value := range spaces {
		if value.ID == identifier ||
			strings.EqualFold(value.Name, identifier) ||
			strings.EqualFold(value.DriveAlias, identifier) {
			matches = append(matches, value)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return space{}, apperror.Wrap(
			apperror.KindUsage, "admin space",
			fmt.Errorf(
				"unknown Space %q; run ocis admin space list",
				identifier,
			),
		)
	default:
		return space{}, apperror.Wrap(
			apperror.KindUsage, "admin space",
			fmt.Errorf("space name %q is ambiguous; use its ID", identifier),
		)
	}
}

func unavailableAdminList(capability string, err error) error {
	switch protocolStatus(err) {
	case http.StatusNotFound, http.StatusMethodNotAllowed,
		http.StatusNotImplemented:
		return fmt.Errorf(
			"server does not expose %s through LibreGraph: %w",
			capability, err,
		)
	default:
		return err
	}
}

func writeAdminTable(
	writer io.Writer, write func(io.Writer) error,
) error {
	table := tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)
	if err := write(table); err != nil {
		return err
	}
	return table.Flush()
}
