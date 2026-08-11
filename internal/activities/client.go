// Package activities implements the authenticated oCIS Graph activity API.
package activities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/mzner/ocis-cli/internal/httpapi"
)

const (
	endpoint         = "/graph/v1beta1/extensions/org.libregraph/activities"
	maxResponseBytes = 8 << 20
	maxLimit         = 1000
)

// Activity is one server-recorded resource activity.
type Activity struct {
	ID       string   `json:"id"`
	Times    Times    `json:"times"`
	Template Template `json:"template"`
}

// Times contains server timestamps associated with an activity.
type Times struct {
	RecordedTime string `json:"recordedTime"`
}

// Template contains the localized activity message and its structured values.
type Template struct {
	Message   string         `json:"message"`
	Variables map[string]any `json:"variables,omitempty"`
}

// ListRequest describes server-side activity filters. A nil Depth omits the
// filter; -1 explicitly requests the complete subtree.
type ListRequest struct {
	ItemID string
	Depth  *int
	Limit  int
	Sort   string
}

// Client reads activities visible to the authenticated user.
type Client struct {
	api *httpapi.Client
}

// NewClient constructs an activities client.
func NewClient(config httpapi.Config, httpClient *http.Client) *Client {
	return &Client{api: httpapi.NewClient(config, httpClient)}
}

// List returns activities matching the requested resource and bounds.
func (client *Client) List(
	ctx context.Context, request ListRequest,
) ([]Activity, error) {
	resource, err := listResource(request)
	if err != nil {
		return nil, err
	}
	response, err := client.api.Do(
		ctx, http.MethodGet, resource, nil,
		http.Header{"Accept": {"application/json"}},
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, httpapi.ResponseError(response)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read activities response: %w", err)
	}
	var payload struct {
		Value []Activity `json:"value"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decode activities response: %w", err)
	}
	if payload.Value == nil {
		return []Activity{}, nil
	}
	return payload.Value, nil
}

func listResource(request ListRequest) (string, error) {
	filters := make([]string, 0, 4)
	itemID := strings.TrimSpace(request.ItemID)
	if strings.ContainsAny(itemID, "\"\r\n") {
		return "", errors.New("activity item ID contains unsupported characters")
	}
	if itemID != "" {
		filters = append(filters, `itemid:"`+itemID+`"`)
	}
	if request.Depth != nil {
		if *request.Depth < -1 {
			return "", errors.New("activity depth must be -1 or greater")
		}
		filters = append(filters, "depth:"+strconv.Itoa(*request.Depth))
	}
	if request.Limit != 0 {
		if request.Limit != -1 &&
			(request.Limit < 1 || request.Limit > maxLimit) {
			return "", fmt.Errorf(
				"activity limit must be -1 or between 1 and %d", maxLimit,
			)
		}
		filters = append(filters, "limit:"+strconv.Itoa(request.Limit))
	}
	sortOrder := strings.ToLower(strings.TrimSpace(request.Sort))
	if sortOrder != "" {
		if sortOrder != "asc" && sortOrder != "desc" {
			return "", errors.New("activity sort must be asc or desc")
		}
		filters = append(filters, "sort:"+sortOrder)
	}
	if len(filters) == 0 {
		return endpoint, nil
	}
	query := url.Values{}
	query.Set("kql", strings.Join(filters, " AND "))
	return endpoint + "?" + query.Encode(), nil
}
