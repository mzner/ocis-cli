package share

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mzner/ocis-cli/internal/graph"
)

type recipient struct {
	ID          string
	DisplayName string
	Username    string
	Mail        string
}

func resolveRecipient(
	ctx context.Context,
	client Client,
	recipientType string,
	identifier string,
	isID bool,
	usage func(string) error,
) (recipient, error) {
	if isID {
		return recipient{ID: identifier, DisplayName: identifier}, nil
	}
	var candidates []recipient
	switch recipientType {
	case "user":
		users, err := client.Graph().SearchUsers(ctx, identifier)
		if err != nil {
			return recipient{}, err
		}
		for _, user := range users {
			candidates = append(candidates, recipientFromUser(user))
		}
	case "group":
		groups, err := client.Graph().SearchGroups(ctx, identifier)
		if err != nil {
			return recipient{}, err
		}
		for _, group := range groups {
			candidates = append(candidates, recipient{
				ID: group.ID, DisplayName: group.DisplayName,
			})
		}
	}
	return selectRecipient(candidates, identifier, recipientType, usage)
}

func resolveFederatedRecipient(
	ctx context.Context,
	client Client,
	identifier string,
	isID bool,
) (recipient, error) {
	if isID {
		return recipient{ID: identifier, DisplayName: identifier}, nil
	}
	users, err := client.Graph().SearchFederatedUsers(ctx, identifier)
	if err != nil {
		return recipient{}, err
	}
	candidates := make([]recipient, 0, len(users))
	for _, user := range users {
		if strings.EqualFold(user.UserType, "Federated") {
			candidates = append(candidates, recipientFromUser(user))
		}
	}
	return selectRecipient(candidates, identifier, "federated user", usageShare)
}

func recipientFromUser(user graph.DirectoryUser) recipient {
	return recipient{
		ID: user.ID, DisplayName: user.DisplayName,
		Username: user.Username, Mail: user.Mail,
	}
}

func selectRecipient(
	candidates []recipient,
	identifier string,
	recipientType string,
	usage func(string) error,
) (recipient, error) {
	var exact []recipient
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
		return recipient{}, usage(fmt.Sprintf(
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
	return recipient{}, usage(fmt.Sprintf(
		"%s %q is ambiguous: %s; use --recipient-id with the intended ID",
		recipientType, identifier, strings.Join(labels, ", "),
	))
}
