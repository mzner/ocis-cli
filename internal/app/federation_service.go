package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mzner/ocis-cli/internal/apperror"
	"github.com/mzner/ocis-cli/internal/federation"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

func runFederation(
	ctx context.Context,
	request FederationRequest,
	selectedProfile string,
	options RunOptions,
) error {
	if request.Operation == FederationConnectionRemove && !request.Confirmed {
		return usageFederation(
			"removing a federation connection requires explicit confirmation",
		)
	}
	client, err := newClientWithOptions(ctx, selectedProfile, options)
	if err != nil {
		return err
	}
	capabilities, err := client.sharingClient().Capabilities(ctx)
	if err != nil {
		return fmt.Errorf("check federation capabilities: %w", err)
	}
	switch request.Operation {
	case FederationInviteCreate, FederationInviteList:
		if !capabilities.Sharing.Federation.Outgoing {
			return federationDisabled("outgoing federation is disabled by the server")
		}
	case FederationInviteAccept:
		if !capabilities.Sharing.Federation.Incoming {
			return federationDisabled("incoming federation is disabled by the server")
		}
	case FederationConnectionList, FederationConnectionRemove:
		if !capabilities.Sharing.Federation.Incoming &&
			!capabilities.Sharing.Federation.Outgoing {
			return federationDisabled("federation is disabled by the server")
		}
	default:
		return usageFederation(fmt.Sprintf(
			"unknown federation command %q", request.Operation,
		))
	}

	switch request.Operation {
	case FederationInviteCreate:
		value, createErr := client.federationClient().CreateInvitation(
			ctx, federation.CreateInvitationRequest{
				Recipient:   strings.TrimSpace(request.Email),
				Description: strings.TrimSpace(request.Description),
			},
		)
		if createErr != nil {
			return createErr
		}
		return writeFederationInvitation(value, options)
	case FederationInviteList:
		values, listErr := client.federationClient().ListInvitations(ctx)
		if listErr != nil {
			return listErr
		}
		sort.Slice(values, func(left, right int) bool {
			return values[left].Expiration < values[right].Expiration
		})
		return writeFederationInvitations(values, options)
	case FederationInviteAccept:
		provider, providerErr := normalizeProviderDomain(request.Provider)
		if providerErr != nil {
			return usageFederation(providerErr.Error())
		}
		if strings.TrimSpace(request.Token) == "" {
			return usageFederation("invitation token must not be empty")
		}
		if acceptErr := client.federationClient().AcceptInvitation(
			ctx, federation.AcceptInvitationRequest{
				Token: request.Token, ProviderDomain: provider,
			},
		); acceptErr != nil {
			return acceptErr
		}
		return output(
			options, "federation-invitation",
			map[string]string{"status": "accepted", "provider": provider},
			"Accepted federation invitation from %s\n", provider,
		)
	case FederationConnectionList:
		values, listErr := client.federationClient().ListConnections(ctx)
		if listErr != nil {
			return listErr
		}
		values = filterFederationConnections(values, request.Identifier)
		sortFederationConnections(values)
		return writeFederationConnections(values, options)
	case FederationConnectionRemove:
		values, listErr := client.federationClient().ListConnections(ctx)
		if listErr != nil {
			return listErr
		}
		selected, resolveErr := resolveFederationConnection(
			values, request.Identifier, request.Provider, request.UserID,
		)
		if resolveErr != nil {
			return resolveErr
		}
		result := map[string]any{
			"operation": "remove", "userId": selected.UserID,
			"displayName": selected.DisplayName,
			"provider":    selected.Provider, "dryRun": request.DryRun,
		}
		if request.DryRun {
			return output(
				options, "federation-connection", result,
				"Would remove federation connection with %s (%s)\n",
				fallback(selected.DisplayName, selected.UserID), selected.Provider,
			)
		}
		if deleteErr := client.federationClient().DeleteConnection(
			ctx, federation.DeleteConnectionRequest{
				Provider: selected.Provider, UserID: selected.UserID,
			},
		); deleteErr != nil {
			return deleteErr
		}
		return output(
			options, "federation-connection", result,
			"Removed federation connection with %s (%s)\n",
			fallback(selected.DisplayName, selected.UserID), selected.Provider,
		)
	}
	return nil
}

func writeFederationInvitation(
	value federation.Invitation, options RunOptions,
) error {
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "federation-invitation", value)
	}
	if _, err := fmt.Fprintf(options.Out, "Invitation token: %s\n", value.Token); err != nil {
		return err
	}
	if value.Expiration != 0 {
		if _, err := fmt.Fprintf(
			options.Out, "Expires: %s\n", formatFederationExpiration(value.Expiration),
		); err != nil {
			return err
		}
	}
	if value.InviteLink != "" {
		_, err := fmt.Fprintf(options.Out, "Invite link: %s\n", value.InviteLink)
		return err
	}
	return nil
}

func writeFederationInvitations(
	values []federation.Invitation, options RunOptions,
) error {
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "federation-invitation", values)
	}
	writer := tabwriter.NewWriter(options.Out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "TOKEN\tEXPIRES\tDESCRIPTION\tINVITE LINK"); err != nil {
		return err
	}
	for _, value := range values {
		if _, err := fmt.Fprintf(
			writer, "%s\t%s\t%s\t%s\n", value.Token,
			formatFederationExpiration(value.Expiration), value.Description,
			value.InviteLink,
		); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func writeFederationConnections(
	values []federation.Connection, options RunOptions,
) error {
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "federation-connection", values)
	}
	writer := tabwriter.NewWriter(options.Out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(
		writer, "DISPLAY NAME\tMAIL\tPROVIDER\tUSER ID",
	); err != nil {
		return err
	}
	for _, value := range values {
		if _, err := fmt.Fprintf(
			writer, "%s\t%s\t%s\t%s\n", value.DisplayName, value.Mail,
			value.Provider, value.UserID,
		); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func formatFederationExpiration(value int64) string {
	if value == 0 {
		return "-"
	}
	return time.Unix(value, 0).UTC().Format(time.RFC3339)
}

func normalizeProviderDomain(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("provider must not be empty")
	}
	parseValue := value
	if !strings.Contains(value, "://") {
		parseValue = "//" + value
	}
	parsed, err := url.Parse(parseValue)
	if err != nil || parsed.Host == "" || parsed.Hostname() == "" {
		return "", fmt.Errorf("invalid federation provider %q", value)
	}
	if parsed.Scheme != "" && parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", fmt.Errorf(
			"invalid federation provider scheme %q; expected http or https",
			parsed.Scheme,
		)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") {
		return "", fmt.Errorf(
			"federation provider %q must contain only a server host and optional port",
			value,
		)
	}
	return parsed.Host, nil
}

func filterFederationConnections(
	values []federation.Connection, search string,
) []federation.Connection {
	search = strings.ToLower(strings.TrimSpace(search))
	if search == "" {
		return values
	}
	result := make([]federation.Connection, 0, len(values))
	for _, value := range values {
		if strings.Contains(strings.ToLower(value.DisplayName), search) ||
			strings.Contains(strings.ToLower(value.Mail), search) ||
			strings.Contains(strings.ToLower(value.UserID), search) ||
			strings.Contains(strings.ToLower(value.Provider), search) {
			result = append(result, value)
		}
	}
	return result
}

func resolveFederationConnection(
	values []federation.Connection,
	identifier string,
	provider string,
	identifierIsUserID bool,
) (federation.Connection, error) {
	identifier = strings.TrimSpace(identifier)
	provider = strings.TrimSpace(provider)
	if identifier == "" {
		return federation.Connection{}, usageFederation(
			"connection identifier must not be empty",
		)
	}
	matches := make([]federation.Connection, 0)
	for _, value := range values {
		if provider != "" && !federationProviderMatches(value.Provider, provider) {
			continue
		}
		matched := strings.EqualFold(value.UserID, identifier)
		if !identifierIsUserID {
			matched = matched || strings.EqualFold(value.DisplayName, identifier) ||
				strings.EqualFold(value.Mail, identifier)
		}
		if matched {
			matches = append(matches, value)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return federation.Connection{}, apperror.Wrap(
			apperror.KindNotFound, "federation connection",
			fmt.Errorf("federation connection %q was not found", identifier),
		)
	}
	providers := make([]string, 0, len(matches))
	for _, value := range matches {
		providers = append(providers, value.Provider)
	}
	sort.Strings(providers)
	return federation.Connection{}, usageFederation(fmt.Sprintf(
		"federation connection %q is ambiguous across providers %s; use --provider",
		identifier, strings.Join(providers, ", "),
	))
}

func federationProviderMatches(actual, requested string) bool {
	actual = strings.TrimRight(strings.TrimSpace(actual), "/")
	requested = strings.TrimRight(strings.TrimSpace(requested), "/")
	if strings.EqualFold(actual, requested) {
		return true
	}
	actualHost, actualErr := normalizeProviderDomain(actual)
	requestedHost, requestedErr := normalizeProviderDomain(requested)
	return actualErr == nil && requestedErr == nil &&
		strings.EqualFold(actualHost, requestedHost)
}

func sortFederationConnections(values []federation.Connection) {
	sort.Slice(values, func(left, right int) bool {
		leftKey := strings.ToLower(values[left].DisplayName + "\x00" +
			values[left].Provider + "\x00" + values[left].UserID)
		rightKey := strings.ToLower(values[right].DisplayName + "\x00" +
			values[right].Provider + "\x00" + values[right].UserID)
		return leftKey < rightKey
	})
}

func federationDisabled(message string) error {
	return apperror.Wrap(
		apperror.KindConflict, "federation", errors.New(message),
	)
}

func usageFederation(message string) error {
	return apperror.Wrap(
		apperror.KindUsage, "federation", errors.New(message),
	)
}
