package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/mzner/ocis-cli/internal/httpapi"
)

const maxResponseBytes = 8 << 20

func (client *Client) doJSON(
	ctx context.Context,
	method string,
	resource string,
	payload any,
	headers http.Header,
	result any,
	operation string,
) error {
	var body []byte
	var err error
	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode %s request: %w", operation, err)
		}
	}
	requestHeaders := http.Header{"Accept": {"application/json"}}
	if payload != nil {
		requestHeaders.Set("Content-Type", "application/json")
	}
	for name, values := range headers {
		for _, value := range values {
			requestHeaders.Add(name, value)
		}
	}
	response, err := client.api.Do(ctx, method, resource, body, requestHeaders)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return httpapi.ResponseError(response)
	}
	if result == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
		return nil
	}
	if err := json.NewDecoder(
		io.LimitReader(response.Body, maxResponseBytes),
	).Decode(result); err != nil {
		return fmt.Errorf("decode %s response: %w", operation, err)
	}
	return nil
}
