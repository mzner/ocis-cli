// Package trash implements recycle-bin operations through the oCIS WebDAV API.
package trash

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/mzner/ocis-cli/internal/httpapi"
)

const maxResponseSize = 8 << 20

// Item describes one deleted resource.
type Item struct {
	ID               string `json:"id"`
	OriginalName     string `json:"originalName"`
	OriginalPath     string `json:"originalPath"`
	Type             string `json:"type"`
	Size             int64  `json:"size,omitempty"`
	DeletedAt        string `json:"deletedAt,omitempty"`
	DeletedTimestamp int64  `json:"deletedTimestamp,omitempty"`
	SpaceID          string `json:"spaceId,omitempty"`
}

// Config selects either a specific Space trash bin or the implicit personal
// trash bin when SpaceID is empty.
type Config struct {
	API      httpapi.Config
	Server   string
	Username string
	SpaceID  string
}

// Client manages one selected recycle bin.
type Client struct {
	api      *httpapi.Client
	server   string
	username string
	spaceID  string
}

// NewClient constructs a trash client.
func NewClient(config Config, httpClient *http.Client) *Client {
	return &Client{
		api:      httpapi.NewClient(config.API, httpClient),
		server:   strings.TrimRight(config.Server, "/"),
		username: config.Username, spaceID: config.SpaceID,
	}
}

// List returns the top-level deleted resources in the selected trash bin.
func (client *Client) List(ctx context.Context) ([]Item, error) {
	body := []byte(`<?xml version="1.0"?>
<d:propfind xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns">
  <d:prop>
    <d:resourcetype/>
    <d:getcontentlength/>
    <oc:size/>
    <oc:trashbin-original-filename/>
    <oc:trashbin-original-location/>
    <oc:trashbin-delete-datetime/>
    <oc:trashbin-delete-timestamp/>
    <oc:spaceid/>
  </d:prop>
</d:propfind>`)
	headers := http.Header{
		"Content-Type": {"application/xml"},
		"Depth":        {"1"},
	}
	response, err := client.api.Do(
		ctx, "PROPFIND", client.trashRoot(), body, headers,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusMultiStatus {
		return nil, httpapi.ResponseError(response)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseSize))
	if err != nil {
		return nil, err
	}
	items, err := DecodeList(data, client.trashRoot())
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(left, right int) bool {
		if items[left].DeletedTimestamp != items[right].DeletedTimestamp {
			return items[left].DeletedTimestamp > items[right].DeletedTimestamp
		}
		return items[left].OriginalPath < items[right].OriginalPath
	})
	return items, nil
}

// Restore moves an item from trash to its original path.
func (client *Client) Restore(
	ctx context.Context, item Item, overwrite bool,
) error {
	if err := validateItem(item); err != nil {
		return err
	}
	headers := http.Header{
		"Destination": {client.destination(item.OriginalPath)},
		"Overwrite":   {"F"},
	}
	if overwrite {
		headers.Set("Overwrite", "T")
	}
	return client.mutate(ctx, "MOVE", client.itemResource(item.ID), headers)
}

// Remove permanently deletes one trash item.
func (client *Client) Remove(ctx context.Context, itemID string) error {
	if strings.TrimSpace(itemID) == "" {
		return fmt.Errorf("trash item ID must not be empty")
	}
	return client.mutate(
		ctx, http.MethodDelete, client.itemResource(itemID), nil,
	)
}

// Empty permanently deletes every item in the selected trash bin.
func (client *Client) Empty(ctx context.Context) error {
	return client.mutate(ctx, http.MethodDelete, client.trashRoot(), nil)
}

func (client *Client) mutate(
	ctx context.Context, method string, resource string, headers http.Header,
) error {
	response, err := client.api.Do(ctx, method, resource, nil, headers)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return httpapi.ResponseError(response)
	}
	return nil
}

func (client *Client) trashRoot() string {
	if client.spaceID != "" {
		return "/dav/spaces/trash-bin/" + url.PathEscape(client.spaceID)
	}
	return "/remote.php/dav/trash-bin/" + url.PathEscape(client.username)
}

func (client *Client) itemResource(itemID string) string {
	return strings.TrimRight(client.trashRoot(), "/") + "/" +
		escapePath(itemID)
}

func (client *Client) destination(originalPath string) string {
	resource := "/remote.php/dav/files/" + url.PathEscape(client.username)
	if client.spaceID != "" {
		resource = "/dav/spaces/" + url.PathEscape(client.spaceID)
	}
	return client.server + strings.TrimRight(resource, "/") +
		escapeRemote(originalPath)
}

func escapePath(value string) string {
	parts := strings.Split(strings.Trim(value, "/"), "/")
	for index := range parts {
		parts[index] = url.PathEscape(parts[index])
	}
	return strings.Join(parts, "/")
}

func escapeRemote(value string) string {
	cleaned := "/" + strings.TrimPrefix(path.Clean("/"+value), "/")
	if cleaned == "/" {
		return "/"
	}
	return "/" + escapePath(cleaned)
}

func validateItem(item Item) error {
	if strings.TrimSpace(item.ID) == "" {
		return fmt.Errorf("trash item ID must not be empty")
	}
	if strings.TrimSpace(item.OriginalPath) == "" {
		return fmt.Errorf("trash item %q has no original path", item.ID)
	}
	return nil
}
