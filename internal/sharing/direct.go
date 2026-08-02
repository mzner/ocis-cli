package sharing

import (
	"context"
	"net/http"
	"net/url"
	"sort"

	"github.com/mzner/ocis-cli/internal/httpapi"
)

// Share describes an outgoing or received OCS share.
type Share struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	Path          string `json:"path,omitempty"`
	ItemType      string `json:"itemType,omitempty"`
	RecipientID   string `json:"recipientId,omitempty"`
	RecipientName string `json:"recipientName,omitempty"`
	RecipientInfo string `json:"recipientInfo,omitempty"`
	Owner         string `json:"owner,omitempty"`
	OwnerName     string `json:"ownerName,omitempty"`
	Permissions   int    `json:"permissions"`
	Expiration    string `json:"expiration,omitempty"`
	State         int    `json:"state,omitempty"`
	URL           string `json:"url,omitempty"`
	Name          string `json:"name,omitempty"`
	SpaceID       string `json:"spaceId,omitempty"`
	ResourceID    string `json:"resourceId,omitempty"`
}

// ShareListRequest filters outgoing or received shares.
type ShareListRequest struct {
	Path     string
	SpaceID  string
	Received bool
}

// ListShares returns outgoing user, group, and public-link shares or received
// user and group shares.
func (client *Client) ListShares(
	ctx context.Context, request ShareListRequest,
) ([]Share, error) {
	query := url.Values{"format": {"json"}}
	if request.Received {
		query.Set("shared_with_me", "true")
		query.Set("share_types", "0,1")
	} else {
		query.Set("reshares", "true")
	}
	if request.SpaceID != "" {
		query.Set("space_ref", spaceReference(request.SpaceID, request.Path))
	} else if request.Path != "" {
		query.Set("path", cleanPath(request.Path))
	}
	response, err := client.api.Do(
		ctx, http.MethodGet, sharesEndpoint()+"?"+query.Encode(), nil,
		ocsHeaders(""),
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, httpapi.ResponseError(response)
	}
	var raw []rawShare
	if err := decodeOCS(response.Body, &raw); err != nil {
		return nil, err
	}
	shares := make([]Share, 0, len(raw))
	for _, value := range raw {
		shares = append(shares, value.share())
	}
	sort.Slice(shares, func(left, right int) bool {
		if shares[left].Path != shares[right].Path {
			return shares[left].Path < shares[right].Path
		}
		if shares[left].Type != shares[right].Type {
			return shares[left].Type < shares[right].Type
		}
		return shares[left].ID < shares[right].ID
	})
	return shares, nil
}

type rawShare struct {
	ID                      stringValue `json:"id"`
	ShareType               stringValue `json:"share_type"`
	Path                    stringValue `json:"path"`
	FileTarget              stringValue `json:"file_target"`
	ItemType                stringValue `json:"item_type"`
	ShareWith               stringValue `json:"share_with"`
	ShareWithDisplayName    stringValue `json:"share_with_displayname"`
	ShareWithAdditionalInfo stringValue `json:"share_with_additional_info"`
	UIDOwner                stringValue `json:"uid_owner"`
	DisplayNameOwner        stringValue `json:"displayname_owner"`
	Permissions             intValue    `json:"permissions"`
	Expiration              stringValue `json:"expiration"`
	State                   intValue    `json:"state"`
	URL                     stringValue `json:"url"`
	Name                    stringValue `json:"name"`
	SpaceID                 stringValue `json:"space_id"`
	FileSource              stringValue `json:"file_source"`
}

func (raw rawShare) share() Share {
	sharePath := string(raw.Path)
	if sharePath == "" {
		sharePath = string(raw.FileTarget)
	}
	return Share{
		ID: string(raw.ID), Type: shareTypeName(string(raw.ShareType)),
		Path: sharePath, ItemType: string(raw.ItemType),
		RecipientID:   string(raw.ShareWith),
		RecipientName: string(raw.ShareWithDisplayName),
		RecipientInfo: string(raw.ShareWithAdditionalInfo),
		Owner:         string(raw.UIDOwner), OwnerName: string(raw.DisplayNameOwner),
		Permissions: int(raw.Permissions), Expiration: string(raw.Expiration),
		State: int(raw.State), URL: string(raw.URL), Name: string(raw.Name),
		SpaceID: string(raw.SpaceID), ResourceID: string(raw.FileSource),
	}
}

func shareTypeName(value string) string {
	switch value {
	case "0", "user":
		return "user"
	case "1", "group":
		return "group"
	case "3", "public_link":
		return "public_link"
	default:
		return value
	}
}
