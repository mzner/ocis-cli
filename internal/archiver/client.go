// Package archiver implements authenticated downloads from the oCIS archive
// service. Capability selection and local-file policy belong to the
// application layer.
package archiver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/mzner/ocis-cli/internal/httpapi"
)

// DownloadRequest identifies resources and the requested archive container.
type DownloadRequest struct {
	ResourceIDs []string
	Format      string
}

// DownloadResult reports bytes received from the archive service.
type DownloadResult struct {
	Bytes int64
}

// Client downloads archives from one same-origin advertised endpoint.
type Client struct {
	api      *httpapi.Client
	resource string
}

// NewClient validates endpoint before constructing an authenticated client.
// Credentials are never sent to a cross-origin capability URL.
func NewClient(
	config httpapi.Config, endpoint string, httpClient *http.Client,
) (*Client, error) {
	resource, err := sameOriginResource(config.Server, endpoint)
	if err != nil {
		return nil, err
	}
	return &Client{
		api: httpapi.NewClient(config, httpClient), resource: resource,
	}, nil
}

// Download streams one archive and reports cumulative bytes written.
func (client *Client) Download(
	ctx context.Context,
	request DownloadRequest,
	destination io.Writer,
	progress func(int64),
) (DownloadResult, error) {
	if len(request.ResourceIDs) == 0 {
		return DownloadResult{}, errors.New("archive resource list cannot be empty")
	}
	format := strings.ToLower(strings.TrimSpace(request.Format))
	if format != "zip" && format != "tar" {
		return DownloadResult{}, fmt.Errorf("unsupported archive format %q", request.Format)
	}
	resourceURL, err := url.Parse(client.resource)
	if err != nil {
		return DownloadResult{}, fmt.Errorf("parse archive endpoint: %w", err)
	}
	query := resourceURL.Query()
	for _, resourceID := range request.ResourceIDs {
		if strings.TrimSpace(resourceID) == "" {
			return DownloadResult{}, errors.New("archive resource ID cannot be empty")
		}
		query.Add("id", resourceID)
	}
	query.Set("output-format", format)
	resourceURL.RawQuery = query.Encode()
	headers := make(http.Header)
	if format == "zip" {
		headers.Set("Accept", "application/zip")
	} else {
		headers.Set("Accept", "application/x-tar")
	}
	response, err := client.api.Do(
		ctx, http.MethodGet, resourceURL.String(), nil, headers,
	)
	if err != nil {
		return DownloadResult{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK ||
		response.StatusCode >= http.StatusMultipleChoices {
		return DownloadResult{}, httpapi.ResponseError(response)
	}
	counter := &progressWriter{destination: destination, progress: progress}
	if _, err := io.Copy(counter, response.Body); err != nil {
		if ctx.Err() != nil {
			return DownloadResult{Bytes: counter.written}, ctx.Err()
		}
		return DownloadResult{Bytes: counter.written}, fmt.Errorf("download archive: %w", err)
	}
	if progress != nil {
		progress(counter.written)
	}
	return DownloadResult{Bytes: counter.written}, nil
}

type progressWriter struct {
	destination io.Writer
	progress    func(int64)
	written     int64
}

func (writer *progressWriter) Write(data []byte) (int, error) {
	written, err := writer.destination.Write(data)
	writer.written += int64(written)
	if writer.progress != nil {
		writer.progress(writer.written)
	}
	return written, err
}

func sameOriginResource(server, endpoint string) (string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", errors.New("server advertised an empty archive URL")
	}
	base, err := url.Parse(server)
	if err != nil {
		return "", fmt.Errorf("parse server URL: %w", err)
	}
	advertised, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse advertised archive URL: %w", err)
	}
	resolved := base.ResolveReference(advertised)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return "", fmt.Errorf(
			"archive URL uses unsupported scheme %q", resolved.Scheme,
		)
	}
	if resolved.User != nil || resolved.Fragment != "" {
		return "", errors.New("archive URL must not contain user information or a fragment")
	}
	if !strings.EqualFold(resolved.Scheme, base.Scheme) ||
		!strings.EqualFold(resolved.Host, base.Host) {
		return "", fmt.Errorf(
			"refusing cross-origin archive URL %s; authenticated archive endpoints must use %s://%s",
			resolved.Redacted(), base.Scheme, base.Host,
		)
	}
	resource := resolved.EscapedPath()
	if resource == "" {
		resource = "/"
	}
	if resolved.RawQuery != "" {
		resource += "?" + resolved.RawQuery
	}
	return resource, nil
}
