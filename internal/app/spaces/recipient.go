package spaces

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mzner/ocis-cli/internal/graph"
)

type spaceRecipient struct {
	ID          string
	DisplayName string
	Username    string
	Mail        string
}

func resolveSpaceRecipient(
	ctx context.Context,
	client Client,
	recipientType string,
	identifier string,
	isID bool,
) (spaceRecipient, error) {
	return resolveRecipient(
		ctx, client, recipientType, identifier, isID, usageSpaceMember,
	)
}

func resolveRecipient(
	ctx context.Context,
	client Client,
	recipientType string,
	identifier string,
	isID bool,
	usage func(string) error,
) (spaceRecipient, error) {
	if isID {
		return spaceRecipient{ID: identifier, DisplayName: identifier}, nil
	}
	var candidates []spaceRecipient
	switch recipientType {
	case "user":
		users, err := client.Graph().SearchUsers(ctx, identifier)
		if err != nil {
			return spaceRecipient{}, err
		}
		candidates = make([]spaceRecipient, 0, len(users))
		for _, user := range users {
			candidates = append(candidates, recipientFromUser(user))
		}
	case "group":
		groups, err := client.Graph().SearchGroups(ctx, identifier)
		if err != nil {
			return spaceRecipient{}, err
		}
		candidates = make([]spaceRecipient, 0, len(groups))
		for _, group := range groups {
			candidates = append(candidates, spaceRecipient{
				ID: group.ID, DisplayName: group.DisplayName,
			})
		}
	}
	return selectRecipient(candidates, identifier, recipientType, usage)
}

func recipientFromUser(user graph.DirectoryUser) spaceRecipient {
	return spaceRecipient{
		ID: user.ID, DisplayName: user.DisplayName,
		Username: user.Username, Mail: user.Mail,
	}
}

func selectSpaceRecipient(
	candidates []spaceRecipient, identifier string, recipientType string,
) (spaceRecipient, error) {
	return selectRecipient(
		candidates, identifier, recipientType, usageSpaceMember,
	)
}

func selectRecipient(
	candidates []spaceRecipient,
	identifier string,
	recipientType string,
	usage func(string) error,
) (spaceRecipient, error) {
	var exact []spaceRecipient
	for _, candidate := range candidates {
		if strings.EqualFold(candidate.ID, identifier) ||
			strings.EqualFold(candidate.DisplayName, identifier) ||
			strings.EqualFold(candidate.Username, identifier) ||
			strings.EqualFold(candidate.Mail, identifier) {
			exact = append(exact, candidate)
		}
	}
	switch len(exact) {
	case 1:
		return exact[0], nil
	case 0:
		if len(candidates) == 1 {
			return candidates[0], nil
		}
	default:
		candidates = exact
	}
	if len(candidates) == 0 {
		return spaceRecipient{}, usage(fmt.Sprintf(
			"no %s matched %q; use --recipient-id with an opaque Graph ID",
			recipientType, identifier,
		))
	}
	labels := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		labels = append(labels, fmt.Sprintf(
			"%s (%s)", fallback(candidate.DisplayName, candidate.Username),
			candidate.ID,
		))
	}
	sort.Strings(labels)
	return spaceRecipient{}, usage(fmt.Sprintf(
		"%s %q is ambiguous: %s; use --recipient-id with the intended ID",
		recipientType, identifier, strings.Join(labels, ", "),
	))
}
