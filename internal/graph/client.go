// Package graph implements the LibreGraph subset used to manage Spaces,
// directory identities, and direct sharing permissions.
package graph

import (
	"context"
	"net/http"

	"github.com/mzner/ocis-cli/internal/httpapi"
)

// Quota describes a space's byte allocation.
type Quota struct {
	Remaining int64  `json:"remaining"`
	Total     int64  `json:"total"`
	Used      int64  `json:"used"`
	State     string `json:"state,omitempty"`
}

// User describes a drive owner.
type User struct {
	ID          string `json:"id,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
}

// Deleted describes a disabled resource.
type Deleted struct {
	State string `json:"state,omitempty"`
}

// DriveRoot describes the root item of a space.
type DriveRoot struct {
	WebDAVURL string   `json:"webDavUrl,omitempty"`
	Deleted   *Deleted `json:"deleted,omitempty"`
}

// Drive describes one space returned by LibreGraph.
type Drive struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	Description          string `json:"description,omitempty"`
	DriveType            string `json:"driveType"`
	DriveAlias           string `json:"driveAlias,omitempty"`
	WebURL               string `json:"webUrl,omitempty"`
	LastModifiedDateTime string `json:"lastModifiedDateTime,omitempty"`
	Quota                Quota  `json:"quota"`
	Owner                struct {
		User User `json:"user"`
	} `json:"owner"`
	Root DriveRoot `json:"root"`
}

// CreateQuota specifies the byte allocation for a new project space.
type CreateQuota struct {
	Total int64 `json:"total"`
}

// CreateDriveRequest describes a project space to create.
type CreateDriveRequest struct {
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	DriveType   string       `json:"driveType"`
	Quota       *CreateQuota `json:"quota,omitempty"`
}

// Client manages spaces through LibreGraph.
type Client struct {
	api *httpapi.Client
}

// NewClient constructs a Spaces client.
func NewClient(config httpapi.Config, httpClient *http.Client) *Client {
	return &Client{api: httpapi.NewClient(config, httpClient)}
}

// ListMyDrives returns spaces where the authenticated user is a member.
func (client *Client) ListMyDrives(ctx context.Context) ([]Drive, error) {
	return client.listDrives(ctx, "/graph/v1.0/me/drives", "list my spaces")
}

// ListDrives returns the spaces visible through the caller's server-side
// permissions. For regular users, oCIS limits this to spaces where they are a
// member; Space Admins can receive additional spaces.
func (client *Client) ListDrives(ctx context.Context) ([]Drive, error) {
	return client.listDrives(ctx, "/graph/v1.0/drives", "list spaces")
}

func (client *Client) listDrives(
	ctx context.Context, resource string, operation string,
) ([]Drive, error) {
	var result struct {
		Value []Drive `json:"value"`
	}
	if err := client.doJSON(
		ctx, http.MethodGet, resource, nil, nil, &result, operation,
	); err != nil {
		return nil, err
	}
	return result.Value, nil
}

// CreateDrive creates a project space.
func (client *Client) CreateDrive(
	ctx context.Context, request CreateDriveRequest,
) (Drive, error) {
	var drive Drive
	if err := client.doJSON(
		ctx, http.MethodPost, "/graph/v1.0/drives", request, nil,
		&drive, "create space",
	); err != nil {
		return Drive{}, err
	}
	return drive, nil
}
