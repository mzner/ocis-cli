// Package notifications implements the authenticated oCIS userlog API used
// for in-app notifications.
package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/mzner/ocis-cli/internal/httpapi"
)

const (
	endpoint         = "/ocs/v2.php/apps/notifications/api/v1/notifications"
	maxResponseBytes = 8 << 20
)

// Notification is one unread in-app notification returned by oCIS.
type Notification struct {
	ID             string         `json:"id"`
	App            string         `json:"app,omitempty"`
	User           string         `json:"user,omitempty"`
	DateTime       string         `json:"dateTime,omitempty"`
	ObjectID       string         `json:"objectId,omitempty"`
	ObjectType     string         `json:"objectType,omitempty"`
	Subject        string         `json:"subject"`
	SubjectRich    string         `json:"subjectRich,omitempty"`
	Message        string         `json:"message"`
	MessageRich    string         `json:"messageRich,omitempty"`
	MessageDetails map[string]any `json:"messageDetails,omitempty"`
}

// Client manages unread in-app notifications for the authenticated user.
type Client struct {
	api *httpapi.Client
}

// NewClient constructs a notifications client.
func NewClient(config httpapi.Config, httpClient *http.Client) *Client {
	return &Client{api: httpapi.NewClient(config, httpClient)}
}

// List returns the authenticated user's unread notifications.
func (client *Client) List(ctx context.Context) ([]Notification, error) {
	response, err := client.api.Do(
		ctx, http.MethodGet, endpoint+"?format=json", nil, headers(""),
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, httpapi.ResponseError(response)
	}
	var envelope rawEnvelope
	if err := json.NewDecoder(
		io.LimitReader(response.Body, maxResponseBytes),
	).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode notifications response: %w", err)
	}
	if err := envelope.check(); err != nil {
		return nil, err
	}
	result := make([]Notification, 0, len(envelope.OCS.Data))
	for _, value := range envelope.OCS.Data {
		result = append(result, value.notification())
	}
	return result, nil
}

// Dismiss removes one notification from the unread userlog.
func (client *Client) Dismiss(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("notification ID must not be empty")
	}
	return client.delete(
		ctx, endpoint+"/"+url.PathEscape(id)+"?format=json", nil,
	)
}

// DismissMany removes multiple notifications from the unread userlog in one
// request.
func (client *Client) DismissMany(ctx context.Context, ids []string) error {
	normalized := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			return errors.New("notification IDs must not be empty")
		}
		normalized = append(normalized, id)
	}
	if len(normalized) == 0 {
		return errors.New("at least one notification ID is required")
	}
	payload, err := json.Marshal(struct {
		IDs []string `json:"ids"`
	}{IDs: normalized})
	if err != nil {
		return fmt.Errorf("encode notification dismissal request: %w", err)
	}
	return client.delete(ctx, endpoint+"?format=json", payload)
}

func (client *Client) delete(
	ctx context.Context, resource string, payload []byte,
) error {
	contentType := ""
	if payload != nil {
		contentType = "application/json"
	}
	response, err := client.api.Do(
		ctx, http.MethodDelete, resource, payload, headers(contentType),
	)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return httpapi.ResponseError(response)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
	return nil
}

func headers(contentType string) http.Header {
	result := http.Header{
		"Accept":         {"application/json"},
		"OCS-APIRequest": {"true"},
	}
	if contentType != "" {
		result.Set("Content-Type", contentType)
	}
	return result
}

type rawEnvelope struct {
	OCS struct {
		Meta struct {
			Status     string `json:"status"`
			StatusCode int    `json:"statuscode"`
			Message    string `json:"message"`
		} `json:"meta"`
		Data []rawNotification `json:"data"`
	} `json:"ocs"`
}

func (value rawEnvelope) check() error {
	code := value.OCS.Meta.StatusCode
	if code == 100 || code == 200 {
		return nil
	}
	if code == 997 {
		code = http.StatusUnauthorized
	}
	return &httpapi.HTTPError{
		StatusCode: code, Status: value.OCS.Meta.Status,
		Message: value.OCS.Meta.Message,
	}
}

type rawNotification struct {
	ID             string         `json:"notification_id"`
	App            string         `json:"app"`
	User           string         `json:"user"`
	DateTime       string         `json:"datetime"`
	ObjectID       string         `json:"object_id"`
	ObjectType     string         `json:"object_type"`
	Subject        string         `json:"subject"`
	SubjectRich    string         `json:"subjectRich"`
	Message        string         `json:"message"`
	MessageRich    string         `json:"messageRich"`
	MessageDetails map[string]any `json:"messageRichParameters"`
}

func (value rawNotification) notification() Notification {
	return Notification(value)
}
