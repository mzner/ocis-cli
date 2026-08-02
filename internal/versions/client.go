// Package versions implements file-version operations through the oCIS
// WebDAV metadata endpoint.
package versions

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/mzner/ocis-cli/internal/httpapi"
)

const maxResponseSize = 8 << 20

// Version describes one historical file version.
type Version struct {
	ID                string `json:"id"`
	Modified          string `json:"modified,omitempty"`
	ModifiedTimestamp int64  `json:"modifiedTimestamp,omitempty"`
	Size              int64  `json:"size"`
	ETag              string `json:"etag,omitempty"`
}

// Content contains a readable historical version body and its response
// metadata. The caller must close Body.
type Content struct {
	Body io.ReadCloser
	Size int64
	ETag string
}

// Client manages historical file versions.
type Client struct {
	api *httpapi.Client
}

// NewClient constructs a versions client.
func NewClient(config httpapi.Config, httpClient *http.Client) *Client {
	return &Client{api: httpapi.NewClient(config, httpClient)}
}

// List returns the historical versions of one resource ID.
func (client *Client) List(
	ctx context.Context, resourceID string,
) ([]Version, error) {
	if strings.TrimSpace(resourceID) == "" {
		return nil, fmt.Errorf("resource ID must not be empty")
	}
	body := []byte(`<?xml version="1.0"?>
<d:propfind xmlns:d="DAV:">
  <d:prop>
    <d:getcontentlength/>
    <d:getlastmodified/>
    <d:getetag/>
    <d:resourcetype/>
  </d:prop>
</d:propfind>`)
	response, err := client.api.Do(
		ctx, "PROPFIND", client.root(resourceID), body,
		http.Header{"Content-Type": {"application/xml"}, "Depth": {"1"}},
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
	available, err := DecodeList(data, client.root(resourceID))
	if err != nil {
		return nil, err
	}
	sort.Slice(available, func(left, right int) bool {
		if available[left].ModifiedTimestamp !=
			available[right].ModifiedTimestamp {
			return available[left].ModifiedTimestamp >
				available[right].ModifiedTimestamp
		}
		return available[left].ID > available[right].ID
	})
	return available, nil
}

// Open returns a stream for one historical version.
func (client *Client) Open(
	ctx context.Context, resourceID string, versionID string,
) (Content, error) {
	if strings.TrimSpace(resourceID) == "" ||
		strings.TrimSpace(versionID) == "" {
		return Content{}, fmt.Errorf("resource and version IDs must not be empty")
	}
	response, err := client.api.Do(
		ctx, http.MethodGet, client.item(resourceID, versionID), nil, nil,
	)
	if err != nil {
		return Content{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer func() { _ = response.Body.Close() }()
		return Content{}, httpapi.ResponseError(response)
	}
	return Content{
		Body: response.Body, Size: response.ContentLength,
		ETag: response.Header.Get("ETag"),
	}, nil
}

// Restore makes one historical version the current file content.
func (client *Client) Restore(
	ctx context.Context, resourceID string, versionID string,
) error {
	if strings.TrimSpace(resourceID) == "" ||
		strings.TrimSpace(versionID) == "" {
		return fmt.Errorf("resource and version IDs must not be empty")
	}
	response, err := client.api.Do(
		ctx, "COPY", client.item(resourceID, versionID), nil, nil,
	)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return httpapi.ResponseError(response)
	}
	return nil
}

func (client *Client) root(resourceID string) string {
	return "/remote.php/dav/meta/" + url.PathEscape(resourceID) + "/v"
}

func (client *Client) item(resourceID string, versionID string) string {
	return client.root(resourceID) + "/" + url.PathEscape(versionID)
}
