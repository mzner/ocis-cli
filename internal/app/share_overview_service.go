package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
	"github.com/mzner/ocis-cli/internal/sharing"
)

const (
	shareDirectionAll      = "all"
	shareDirectionOutgoing = "outgoing"
	shareDirectionReceived = "received"

	shareStateCurrent  = "current"
	shareStateAll      = "all"
	shareStateAccepted = "accepted"
	shareStatePending  = "pending"
	shareStateDeclined = "declined"
)

// ShareOverviewItem is one stable outgoing or received share inventory row.
type ShareOverviewItem struct {
	ShareID       string `json:"shareId"`
	Direction     string `json:"direction"`
	State         string `json:"state"`
	SpaceID       string `json:"spaceId,omitempty"`
	SpaceName     string `json:"spaceName"`
	Path          string `json:"path,omitempty"`
	Type          string `json:"type"`
	PartyID       string `json:"partyId,omitempty"`
	PartyName     string `json:"partyName"`
	Permissions   int    `json:"permissions"`
	Permission    string `json:"permission"`
	Expiration    string `json:"expiration,omitempty"`
	PublicLinkURL string `json:"publicLinkUrl,omitempty"`
}

func validateShareOverviewFilters(request ShareRequest) error {
	direction := normalizeOverviewDirection(request.Direction)
	state := normalizeOverviewState(request.State)
	if direction != shareDirectionAll &&
		direction != shareDirectionOutgoing &&
		direction != shareDirectionReceived {
		return usageShareOverview(fmt.Sprintf(
			"invalid direction %q; expected outgoing, received, or all",
			request.Direction,
		))
	}
	switch state {
	case shareStateCurrent, shareStateAll, shareStateAccepted,
		shareStatePending, shareStateDeclined:
	default:
		return usageShareOverview(fmt.Sprintf(
			"invalid state %q; expected current, accepted, pending, declined, or all",
			request.State,
		))
	}
	if direction == shareDirectionOutgoing &&
		state != shareStateCurrent && state != shareStateAll {
		return usageShareOverview(
			"accepted, pending, and declined states apply only to received shares",
		)
	}
	return nil
}

func listShareOverview(
	ctx context.Context, client *client, request ShareRequest, options RunOptions,
) error {
	direction := normalizeOverviewDirection(request.Direction)
	state := normalizeOverviewState(request.State)

	spaces, err := client.graphClient().ListMyDrives(ctx)
	if err != nil {
		return err
	}
	var selected *space
	if strings.TrimSpace(options.Space) != "" {
		value, resolveErr := resolveSpace(spaces, options.Space)
		if resolveErr != nil {
			return resolveErr
		}
		selected = &value
	}

	includeOutgoing := direction != shareDirectionReceived &&
		(state == shareStateCurrent || state == shareStateAll)
	includeReceived := direction != shareDirectionOutgoing
	if selected != nil && isReceivedSharesDrive(*selected) {
		includeOutgoing = false
	}

	items := make([]ShareOverviewItem, 0)
	if includeOutgoing {
		listRequest := sharing.ShareListRequest{}
		if selected != nil {
			listRequest.SpaceID = selected.ID
		}
		shares, listErr := client.sharingClient().ListShares(ctx, listRequest)
		if listErr != nil {
			return listErr
		}
		for _, value := range shares {
			items = append(
				items, overviewItem(value, shareDirectionOutgoing, spaces),
			)
		}
	}
	if includeReceived {
		shares, listErr := client.sharingClient().ListShares(
			ctx, sharing.ShareListRequest{Received: true, AllStates: true},
		)
		if listErr != nil {
			return listErr
		}
		for _, value := range shares {
			if !overviewReceivedStateMatches(value, state) ||
				!overviewSpaceMatches(value, selected) {
				continue
			}
			items = append(
				items, overviewItem(value, shareDirectionReceived, spaces),
			)
		}
	}

	sort.Slice(items, func(left, right int) bool {
		leftKey := []string{
			items[left].Direction, items[left].SpaceName, items[left].Path,
			items[left].Type, items[left].ShareID,
		}
		rightKey := []string{
			items[right].Direction, items[right].SpaceName, items[right].Path,
			items[right].Type, items[right].ShareID,
		}
		for index := range leftKey {
			if leftKey[index] != rightKey[index] {
				return leftKey[index] < rightKey[index]
			}
		}
		return false
	})
	return writeShareOverview(items, options)
}

func normalizeOverviewDirection(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return shareDirectionAll
	}
	return value
}

func normalizeOverviewState(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return shareStateCurrent
	}
	if value == "rejected" {
		return shareStateDeclined
	}
	return value
}

func overviewReceivedStateMatches(value sharing.Share, state string) bool {
	actual := value.StateName
	if actual == "" && value.State != nil {
		actual = sharing.ShareStateName(*value.State)
	}
	switch state {
	case shareStateAll:
		return true
	case shareStateCurrent:
		return actual == shareStateAccepted || actual == shareStatePending
	default:
		return actual == state
	}
}

func overviewSpaceMatches(value sharing.Share, selected *space) bool {
	if selected == nil || isReceivedSharesDrive(*selected) {
		return true
	}
	return shareSpaceMatches(value.SpaceID, selected.ID)
}

func shareSpaceMatches(shareSpaceID, selectedSpaceID string) bool {
	return shareSpaceID == selectedSpaceID ||
		strings.HasPrefix(shareSpaceID, selectedSpaceID+"!")
}

func isReceivedSharesDrive(value space) bool {
	return value.DriveType == "virtual" &&
		(strings.EqualFold(value.DriveAlias, "virtual/shares") ||
			strings.EqualFold(value.Name, "Shares"))
}

func overviewItem(
	value sharing.Share, direction string, spaces []space,
) ShareOverviewItem {
	state := "active"
	partyID := value.RecipientID
	partyName := fallback(value.RecipientName, value.RecipientID)
	if direction == shareDirectionReceived {
		state = fallback(value.StateName, "unknown")
		partyID = value.Owner
		partyName = fallback(value.OwnerName, value.Owner)
	} else if value.Type == "public_link" {
		partyID = ""
		partyName = fallback(value.Name, value.URL)
	}
	if partyName == "" {
		partyName = "-"
	}
	spaceName := overviewSpaceName(value.SpaceID, spaces)
	if spaceName == "" && direction == shareDirectionReceived {
		spaceName = "Shares"
	}
	if spaceName == "" {
		spaceName = "unknown"
	}
	return ShareOverviewItem{
		ShareID: value.ID, Direction: direction, State: state,
		SpaceID: value.SpaceID, SpaceName: spaceName,
		Path: value.Path, Type: value.Type,
		PartyID: partyID, PartyName: partyName,
		Permissions: value.Permissions,
		Permission:  permissionName(value.Permissions),
		Expiration:  value.Expiration, PublicLinkURL: value.URL,
	}
}

func overviewSpaceName(spaceID string, spaces []space) string {
	bestName, bestID := "", ""
	for _, candidate := range spaces {
		if shareSpaceMatches(spaceID, candidate.ID) &&
			len(candidate.ID) > len(bestID) {
			bestName, bestID = candidate.Name, candidate.ID
		}
	}
	return bestName
}

func writeShareOverview(items []ShareOverviewItem, options RunOptions) error {
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "share-overview", items)
	}
	writer := tabwriter.NewWriter(options.Out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(
		writer,
		"DIRECTION\tSTATE\tSPACE\tPATH\tTYPE\tPARTY\tPERMISSION\tSHARE ID",
	); err != nil {
		return err
	}
	for _, value := range items {
		if _, err := fmt.Fprintf(
			writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			value.Direction, value.State, value.SpaceName, value.Path,
			value.Type, value.PartyName, value.Permission, value.ShareID,
		); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func usageShareOverview(message string) error {
	return apperror.Wrap(
		apperror.KindUsage, "share overview", fmt.Errorf("%s", message),
	)
}
