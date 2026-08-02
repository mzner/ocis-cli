// Package search implements the oCIS WebDAV search-files REPORT protocol.
package search

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"

	"github.com/mzner/ocis-cli/internal/httpapi"
)

const endpoint = "/remote.php/dav/spaces"

// Request describes one server-side search.
type Request struct {
	Pattern string
	Limit   int
}

// Response contains one page of ranked search results.
type Response struct {
	Items []Item `json:"items"`
	Total int    `json:"total"`
}

// Client performs WebDAV search requests.
type Client struct {
	api *httpapi.Client
}

// NewClient constructs a search client.
func NewClient(config httpapi.Config, httpClient *http.Client) *Client {
	return &Client{api: httpapi.NewClient(config, httpClient)}
}

// Search runs an oCIS search-files REPORT.
func (client *Client) Search(
	ctx context.Context, request Request,
) (Response, error) {
	body, err := marshalReport(request)
	if err != nil {
		return Response{}, err
	}
	headers := http.Header{
		"Accept":       {"application/xml"},
		"Content-Type": {"application/xml; charset=utf-8"},
	}
	response, err := client.api.Do(
		ctx, "REPORT", endpoint, body, headers,
	)
	if err != nil {
		return Response{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusMultiStatus {
		return Response{}, httpapi.ResponseError(response)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 32<<20))
	if err != nil {
		return Response{}, fmt.Errorf("read search response: %w", err)
	}
	items, err := Decode(data)
	if err != nil {
		return Response{}, err
	}
	total, err := ParseContentRange(response.Header.Get("Content-Range"))
	if err != nil {
		return Response{}, err
	}
	if total < len(items) {
		total = len(items)
	}
	return Response{Items: items, Total: total}, nil
}

type report struct {
	XMLName xml.Name     `xml:"http://owncloud.org/ns search-files"`
	Prop    reportProp   `xml:"DAV: prop"`
	Search  reportSearch `xml:"http://owncloud.org/ns search"`
}

type reportProp struct {
	FileID        struct{} `xml:"http://owncloud.org/ns fileid"`
	ParentID      struct{} `xml:"http://owncloud.org/ns file-parent"`
	Name          struct{} `xml:"http://owncloud.org/ns name"`
	LastModified  struct{} `xml:"DAV: getlastmodified"`
	ContentType   struct{} `xml:"DAV: getcontenttype"`
	ResourceType  struct{} `xml:"DAV: resourcetype"`
	ContentLength struct{} `xml:"DAV: getcontentlength"`
	Size          struct{} `xml:"http://owncloud.org/ns size"`
	Permissions   struct{} `xml:"http://owncloud.org/ns permissions"`
	Highlights    struct{} `xml:"http://owncloud.org/ns highlights"`
	SpaceID       struct{} `xml:"http://owncloud.org/ns spaceid"`
	Tags          struct{} `xml:"http://owncloud.org/ns tags"`
	ETag          struct{} `xml:"DAV: getetag"`
	Score         struct{} `xml:"http://owncloud.org/ns score"`
}

type reportSearch struct {
	Pattern string `xml:"http://owncloud.org/ns pattern"`
	Limit   int    `xml:"http://owncloud.org/ns limit"`
}

func marshalReport(request Request) ([]byte, error) {
	data, err := xml.Marshal(report{
		Search: reportSearch(request),
	})
	if err != nil {
		return nil, fmt.Errorf("encode search request: %w", err)
	}
	return append([]byte(xml.Header), data...), nil
}
