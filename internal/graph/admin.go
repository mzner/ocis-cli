package graph

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// UpdateDriveRequest contains only properties explicitly changed by the user.
type UpdateDriveRequest struct {
	Name        *string      `json:"name,omitempty"`
	Description *string      `json:"description,omitempty"`
	DriveAlias  *string      `json:"driveAlias,omitempty"`
	Quota       *CreateQuota `json:"quota,omitempty"`
}

// GetDrive returns one space by its stable ID.
func (client *Client) GetDrive(ctx context.Context, driveID string) (Drive, error) {
	var drive Drive
	if err := client.doJSON(
		ctx, http.MethodGet, driveResource(driveID), nil, nil,
		&drive, "get space",
	); err != nil {
		return Drive{}, err
	}
	return drive, nil
}

// UpdateDrive changes selected space metadata.
func (client *Client) UpdateDrive(
	ctx context.Context, driveID string, request UpdateDriveRequest,
) (Drive, error) {
	var drive Drive
	if err := client.doJSON(
		ctx, http.MethodPatch, driveResource(driveID), request, nil,
		&drive, "update space",
	); err != nil {
		return Drive{}, err
	}
	return drive, nil
}

// RestoreDrive makes a disabled space available again.
func (client *Client) RestoreDrive(ctx context.Context, driveID string) (Drive, error) {
	var drive Drive
	if err := client.doJSON(
		ctx, http.MethodPatch, driveResource(driveID), struct{}{},
		http.Header{"Restore": {"true"}}, &drive, "restore space",
	); err != nil {
		return Drive{}, err
	}
	return drive, nil
}

// DeleteDrive disables a space, or permanently deletes it when purge is true.
func (client *Client) DeleteDrive(
	ctx context.Context, driveID string, purge bool,
) error {
	headers := http.Header{}
	operation := "disable space"
	if purge {
		headers.Set("Purge", "true")
		operation = "permanently delete space"
	}
	return client.doJSON(
		ctx, http.MethodDelete, driveResource(driveID), nil, headers,
		nil, operation,
	)
}

func driveResource(driveID string) string {
	return fmt.Sprintf("/graph/v1.0/drives/%s", url.PathEscape(driveID))
}
