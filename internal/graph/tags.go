package graph

import (
	"context"
	"net/http"
)

const tagsResource = "/graph/v1.0/extensions/org.libregraph/tags"

type tagMutationRequest struct {
	ResourceID string   `json:"resourceId"`
	Tags       []string `json:"tags"`
}

// AddTags assigns tags to a resource by its stable oCIS resource ID.
func (client *Client) AddTags(
	ctx context.Context, resourceID string, tags []string,
) error {
	return client.doJSON(
		ctx, http.MethodPut, tagsResource,
		tagMutationRequest{ResourceID: resourceID, Tags: tags},
		nil, nil, "add tags",
	)
}

// RemoveTags unassigns tags from a resource by its stable oCIS resource ID.
func (client *Client) RemoveTags(
	ctx context.Context, resourceID string, tags []string,
) error {
	return client.doJSON(
		ctx, http.MethodDelete, tagsResource,
		tagMutationRequest{ResourceID: resourceID, Tags: tags},
		nil, nil, "remove tags",
	)
}
