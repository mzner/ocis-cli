// Package sharing implements OCS share discovery and public-link management.
// Direct permission mutations use the focused LibreGraph adapter.
package sharing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/mzner/ocis-cli/internal/httpapi"
)

// Link describes one public-link share.
type Link struct {
	ID                string `json:"id"`
	URL               string `json:"url"`
	Token             string `json:"token,omitempty"`
	Path              string `json:"path"`
	Name              string `json:"name,omitempty"`
	LinkType          string `json:"linkType,omitempty"`
	Permissions       int    `json:"permissions"`
	Expiration        string `json:"expiration,omitempty"`
	PasswordProtected bool   `json:"passwordProtected"`
	SpaceID           string `json:"spaceId,omitempty"`
	ResourceID        string `json:"resourceId,omitempty"`
}

// CreateRequest describes a new public link.
type CreateRequest struct {
	Path        string
	SpaceID     string
	Name        string
	Password    string
	Expiration  string
	Permissions int
}

// ListRequest filters public links.
type ListRequest struct {
	Path    string
	SpaceID string
}

// Capabilities reports server support relevant to direct sharing, Spaces, and
// public links.
type Capabilities struct {
	Auth struct {
		MFA struct {
			Enabled         bool     `json:"enabled"`
			LevelNames      []string `json:"levelNames,omitempty"`
			SessionDuration int      `json:"sessionDuration,omitempty"`
		} `json:"mfa"`
	} `json:"auth"`
	DAV struct {
		Reports []string `json:"reports,omitempty"`
	} `json:"dav"`
	Files struct {
		TUS TUSCapabilities `json:"tus"`
	} `json:"files"`
	Sharing struct {
		APIEnabled   bool `json:"apiEnabled"`
		GroupEnabled bool `json:"groupEnabled"`
		SharingRoles bool `json:"sharingRoles"`
		Public       struct {
			Enabled  bool `json:"enabled"`
			Password struct {
				Enforced bool `json:"enforced"`
			} `json:"password"`
			ExpireDate struct {
				Enabled bool `json:"enabled"`
			} `json:"expireDate"`
		} `json:"public"`
		Federation struct {
			Outgoing bool `json:"outgoing"`
			Incoming bool `json:"incoming"`
		} `json:"federation"`
	} `json:"sharing"`
	Spaces struct {
		Enabled  bool   `json:"enabled"`
		Projects bool   `json:"projects"`
		Version  string `json:"version,omitempty"`
	} `json:"spaces"`
	Graph struct {
		Users struct {
			ReadOnlyAttributes []string `json:"readOnlyAttributes,omitempty"`
			CreateDisabled     bool     `json:"createDisabled"`
			DeleteDisabled     bool     `json:"deleteDisabled"`
		} `json:"users"`
	} `json:"graph"`
}

// TUSCapabilities contains resumable-upload policy advertised by oCIS.
type TUSCapabilities struct {
	Version            string   `json:"version,omitempty"`
	Resumable          string   `json:"resumable,omitempty"`
	Extensions         []string `json:"extensions,omitempty"`
	MaxChunkSize       int64    `json:"maxChunkSize,omitempty"`
	HTTPMethodOverride bool     `json:"httpMethodOverride,omitempty"`
}

// Client manages OCS shares and public links.
type Client struct {
	api *httpapi.Client
}

// NewClient constructs an OCS sharing client.
func NewClient(config httpapi.Config, httpClient *http.Client) *Client {
	return &Client{api: httpapi.NewClient(config, httpClient)}
}

// CreateLink creates a public link.
func (client *Client) CreateLink(
	ctx context.Context, request CreateRequest,
) (Link, error) {
	values := url.Values{
		"shareType":   {"3"},
		"permissions": {strconv.Itoa(request.Permissions)},
	}
	if request.SpaceID != "" {
		values.Set("space_ref", spaceReference(request.SpaceID, request.Path))
	} else {
		values.Set("path", cleanPath(request.Path))
	}
	if request.Name != "" {
		values.Set("name", request.Name)
	}
	if request.Password != "" {
		values.Set("password", request.Password)
	}
	if request.Expiration != "" {
		values.Set("expireDate", request.Expiration)
	}
	response, err := client.api.Do(
		ctx, http.MethodPost, sharesEndpoint()+"?format=json",
		[]byte(values.Encode()), ocsHeaders("application/x-www-form-urlencoded"),
	)
	if err != nil {
		return Link{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Link{}, httpapi.ResponseError(response)
	}
	var raw rawLink
	if err := decodeOCS(response.Body, &raw); err != nil {
		return Link{}, err
	}
	return raw.link(), nil
}

// ListLinks returns public links created by the authenticated user.
func (client *Client) ListLinks(
	ctx context.Context, request ListRequest,
) ([]Link, error) {
	query := url.Values{"format": {"json"}, "reshares": {"true"}}
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
	var raw []rawLink
	if err := decodeOCS(response.Body, &raw); err != nil {
		return nil, err
	}
	links := make([]Link, 0, len(raw))
	for _, value := range raw {
		if value.publicLink() {
			links = append(links, value.link())
		}
	}
	return links, nil
}

// GetLink returns one public link by its stable share ID.
func (client *Client) GetLink(ctx context.Context, id string) (Link, error) {
	response, err := client.api.Do(
		ctx, http.MethodGet,
		sharesEndpoint()+"/"+url.PathEscape(id)+"?format=json",
		nil, ocsHeaders(""),
	)
	if err != nil {
		return Link{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Link{}, httpapi.ResponseError(response)
	}
	var values []rawLink
	if err := decodeOCS(response.Body, &values); err != nil {
		return Link{}, err
	}
	if len(values) != 1 {
		return Link{}, fmt.Errorf(
			"decode public link %q: expected one share, received %d",
			id, len(values),
		)
	}
	if !values[0].publicLink() {
		return Link{}, fmt.Errorf("share %q is not a public link", id)
	}
	return values[0].link(), nil
}

// RevokeLink removes one public link without deleting its resource.
func (client *Client) RevokeLink(ctx context.Context, id string) error {
	response, err := client.api.Do(
		ctx, http.MethodDelete, sharesEndpoint()+"/"+url.PathEscape(id)+"?format=json",
		nil, ocsHeaders(""),
	)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return httpapi.ResponseError(response)
	}
	return decodeOCS(response.Body, nil)
}

// Capabilities returns OCS feature flags for sharing and Spaces.
func (client *Client) Capabilities(ctx context.Context) (Capabilities, error) {
	response, err := client.api.Do(
		ctx, http.MethodGet, "/ocs/v2.php/cloud/capabilities?format=json",
		nil, ocsHeaders(""),
	)
	if err != nil {
		return Capabilities{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Capabilities{}, httpapi.ResponseError(response)
	}
	var raw struct {
		Capabilities struct {
			Auth struct {
				MFA struct {
					Enabled         bool     `json:"enabled"`
					LevelNames      []string `json:"levelnames"`
					SessionDuration int      `json:"session_duration"`
				} `json:"mfa"`
			} `json:"auth"`
			DAV struct {
				Reports []string `json:"reports"`
			} `json:"dav"`
			Files struct {
				TUSSupport struct {
					Version            string          `json:"version"`
					Resumable          string          `json:"resumable"`
					Extension          string          `json:"extension"`
					MaxChunkSize       int64           `json:"max_chunk_size"`
					HTTPMethodOverride json.RawMessage `json:"http_method_override"`
				} `json:"tus_support"`
			} `json:"files"`
			FilesSharing struct {
				APIEnabled   bool `json:"api_enabled"`
				GroupEnabled bool `json:"group_sharing"`
				SharingRoles bool `json:"sharing_roles"`
				Public       struct {
					Enabled  bool `json:"enabled"`
					Password struct {
						Enforced bool `json:"enforced"`
					} `json:"password"`
					ExpireDate struct {
						Enabled bool `json:"enabled"`
					} `json:"expire_date"`
				} `json:"public"`
				Federation struct {
					Outgoing bool `json:"outgoing"`
					Incoming bool `json:"incoming"`
				} `json:"federation"`
			} `json:"files_sharing"`
			Spaces struct {
				Enabled  bool   `json:"enabled"`
				Projects bool   `json:"projects"`
				Version  string `json:"version"`
			} `json:"spaces"`
			Graph struct {
				Users struct {
					ReadOnlyAttributes []string `json:"read_only_attributes"`
					CreateDisabled     bool     `json:"create_disabled"`
					DeleteDisabled     bool     `json:"delete_disabled"`
				} `json:"users"`
			} `json:"graph"`
		} `json:"capabilities"`
	}
	if err := decodeOCS(response.Body, &raw); err != nil {
		return Capabilities{}, err
	}
	var result Capabilities
	result.Auth.MFA.Enabled = raw.Capabilities.Auth.MFA.Enabled
	result.Auth.MFA.LevelNames = raw.Capabilities.Auth.MFA.LevelNames
	result.Auth.MFA.SessionDuration =
		raw.Capabilities.Auth.MFA.SessionDuration
	result.DAV.Reports = raw.Capabilities.DAV.Reports
	result.Files.TUS.Version = raw.Capabilities.Files.TUSSupport.Version
	result.Files.TUS.Resumable = raw.Capabilities.Files.TUSSupport.Resumable
	result.Files.TUS.Extensions = splitCapabilityList(
		raw.Capabilities.Files.TUSSupport.Extension,
	)
	result.Files.TUS.MaxChunkSize =
		raw.Capabilities.Files.TUSSupport.MaxChunkSize
	result.Files.TUS.HTTPMethodOverride = capabilityBool(
		raw.Capabilities.Files.TUSSupport.HTTPMethodOverride,
	)
	result.Sharing.APIEnabled = raw.Capabilities.FilesSharing.APIEnabled
	result.Sharing.GroupEnabled = raw.Capabilities.FilesSharing.GroupEnabled
	result.Sharing.SharingRoles = raw.Capabilities.FilesSharing.SharingRoles
	result.Sharing.Public.Enabled = raw.Capabilities.FilesSharing.Public.Enabled
	result.Sharing.Public.Password.Enforced =
		raw.Capabilities.FilesSharing.Public.Password.Enforced
	result.Sharing.Public.ExpireDate.Enabled =
		raw.Capabilities.FilesSharing.Public.ExpireDate.Enabled
	result.Sharing.Federation.Outgoing =
		raw.Capabilities.FilesSharing.Federation.Outgoing
	result.Sharing.Federation.Incoming =
		raw.Capabilities.FilesSharing.Federation.Incoming
	result.Spaces.Enabled = raw.Capabilities.Spaces.Enabled
	result.Spaces.Projects = raw.Capabilities.Spaces.Projects
	result.Spaces.Version = raw.Capabilities.Spaces.Version
	result.Graph.Users.ReadOnlyAttributes =
		raw.Capabilities.Graph.Users.ReadOnlyAttributes
	result.Graph.Users.CreateDisabled =
		raw.Capabilities.Graph.Users.CreateDisabled
	result.Graph.Users.DeleteDisabled =
		raw.Capabilities.Graph.Users.DeleteDisabled
	return result, nil
}

func splitCapabilityList(value string) []string {
	result := make([]string, 0)
	for item := range strings.SplitSeq(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func capabilityBool(value json.RawMessage) bool {
	normalized := strings.Trim(strings.TrimSpace(string(value)), `"`)
	switch strings.ToLower(normalized) {
	case "1", "true", "post":
		return true
	default:
		return false
	}
}

func sharesEndpoint() string {
	return "/ocs/v2.php/apps/files_sharing/api/v1/shares"
}

func ocsHeaders(contentType string) http.Header {
	headers := http.Header{
		"Accept":         {"application/json"},
		"OCS-APIRequest": {"true"},
	}
	if contentType != "" {
		headers.Set("Content-Type", contentType)
	}
	return headers
}

type ocsEnvelope struct {
	OCS struct {
		Meta struct {
			Status     string   `json:"status"`
			StatusCode intValue `json:"statuscode"`
			Message    string   `json:"message"`
		} `json:"meta"`
		Data json.RawMessage `json:"data"`
	} `json:"ocs"`
}

func decodeOCS(reader io.Reader, target any) error {
	var envelope ocsEnvelope
	if err := json.NewDecoder(io.LimitReader(reader, 8<<20)).Decode(&envelope); err != nil {
		return fmt.Errorf("decode OCS response: %w", err)
	}
	code := int(envelope.OCS.Meta.StatusCode)
	if code != 100 && code != 200 {
		status := code
		if code == 997 {
			status = http.StatusUnauthorized
		}
		return &httpapi.HTTPError{
			StatusCode: status, Status: envelope.OCS.Meta.Status,
			Message: envelope.OCS.Meta.Message,
		}
	}
	if target == nil || len(envelope.OCS.Data) == 0 ||
		string(envelope.OCS.Data) == "null" {
		return nil
	}
	if err := json.Unmarshal(envelope.OCS.Data, target); err != nil {
		return fmt.Errorf("decode OCS data: %w", err)
	}
	return nil
}

type rawLink struct {
	ID          stringValue `json:"id"`
	URL         stringValue `json:"url"`
	Token       stringValue `json:"token"`
	Path        stringValue `json:"path"`
	FileTarget  stringValue `json:"file_target"`
	Name        stringValue `json:"name"`
	Permissions intValue    `json:"permissions"`
	Expiration  stringValue `json:"expiration"`
	SpaceID     stringValue `json:"space_id"`
	StorageID   stringValue `json:"storage_id"`
	FileSource  stringValue `json:"file_source"`
	ShareWith   stringValue `json:"share_with"`
	ShareType   stringValue `json:"share_type"`
}

func (raw rawLink) publicLink() bool {
	return raw.ShareType == "3" || raw.ShareType == "public_link"
}

func (raw rawLink) link() Link {
	linkPath := string(raw.Path)
	if linkPath == "" {
		linkPath = string(raw.FileTarget)
	}
	spaceID := string(raw.SpaceID)
	if spaceID == "" {
		spaceID = string(raw.StorageID)
	}
	return Link{
		ID: string(raw.ID), URL: string(raw.URL), Token: string(raw.Token),
		Path: linkPath, Name: string(raw.Name), Permissions: int(raw.Permissions),
		Expiration:        string(raw.Expiration),
		PasswordProtected: string(raw.ShareWith) == "***redacted***",
		SpaceID:           spaceID, ResourceID: string(raw.FileSource),
	}
}

type stringValue string

func (value *stringValue) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*value = ""
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*value = stringValue(text)
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		return err
	}
	*value = stringValue(number.String())
	return nil
}

type intValue int

func (value *intValue) UnmarshalJSON(data []byte) error {
	var number int
	if err := json.Unmarshal(data, &number); err == nil {
		*value = intValue(number)
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	number, err := strconv.Atoi(text)
	if err != nil {
		return err
	}
	*value = intValue(number)
	return nil
}

func cleanPath(value string) string {
	cleaned := "/" + strings.Trim(strings.TrimSpace(value), "/")
	if cleaned == "/" {
		return cleaned
	}
	return strings.TrimSuffix(cleaned, "/")
}

func spaceReference(spaceID, resourcePath string) string {
	if cleanPath(resourcePath) == "/" {
		return spaceID
	}
	return spaceID + cleanPath(resourcePath)
}
