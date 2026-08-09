// Package httpapi provides authenticated JSON and form HTTP transport for
// oCIS APIs other than WebDAV.
package httpapi

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mzner/ocis-cli/internal/logging"
	"github.com/mzner/ocis-cli/internal/retry"
)

// Config contains shared authentication and retry settings.
type Config struct {
	Server      string
	Username    string
	AuthType    string
	Password    string
	AccessToken string
	UserAgent   string
	Retries     int
	RetryWait   time.Duration
	Logger      logging.Logger
}

// HTTPError reports a non-successful API response.
type HTTPError struct {
	StatusCode  int
	Status      string
	Message     string
	MFARequired bool
}

// Error implements error.
func (err *HTTPError) Error() string {
	return fmt.Sprintf("%s: %s", err.Status, err.Message)
}

// HTTPStatusCode exposes the response status to the application boundary.
func (err *HTTPError) HTTPStatusCode() int {
	return err.StatusCode
}

// RequiresMFA reports whether oCIS requested step-up authentication.
func (err *HTTPError) RequiresMFA() bool {
	return err.MFARequired
}

// Client sends authenticated requests with bounded retries.
type Client struct {
	config Config
	http   *http.Client
}

// NewClient constructs an API client.
func NewClient(config Config, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Minute}
	}
	if config.RetryWait <= 0 {
		config.RetryWait = 200 * time.Millisecond
	}
	if config.Logger == nil {
		config.Logger = logging.Nop()
	}
	return &Client{config: config, http: httpClient}
}

// Do sends one request. Body bytes are replayed for retry attempts.
func (client *Client) Do(
	ctx context.Context, method, resource string, body []byte, headers http.Header,
) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		request, err := http.NewRequestWithContext(
			ctx, method, client.endpoint(resource), bytes.NewReader(body),
		)
		if err != nil {
			return nil, err
		}
		for name, values := range headers {
			for _, value := range values {
				request.Header.Add(name, value)
			}
		}
		client.authenticate(request)
		response, err := client.http.Do(request)
		if err == nil && (!retry.RetryableStatus(response.StatusCode) || attempt >= client.config.Retries) {
			return response, nil
		}
		if err != nil && attempt >= client.config.Retries {
			return nil, err
		}
		delay, reason := time.Duration(0), "transport_error"
		if response != nil {
			delay, reason = retry.After(response), response.Status
			_ = response.Body.Close()
		}
		client.config.Logger.Debug(
			"retrying API request",
			"method", method, "attempt", attempt+2, "reason", reason,
		)
		if err := retry.Wait(ctx, client.config.RetryWait, attempt, delay); err != nil {
			return nil, err
		}
	}
}

// ResponseError reads a bounded response body and returns a typed error.
func ResponseError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	message := strings.TrimSpace(string(body))
	mfaRequired := strings.EqualFold(
		strings.TrimSpace(response.Header.Get("X-Ocis-Mfa-Required")), "true",
	)
	if mfaRequired {
		message = "multi-factor authentication is required by the server"
	}
	var davError struct {
		Message string `xml:"message"`
	}
	if strings.HasPrefix(message, "<") &&
		xml.Unmarshal(body, &davError) == nil &&
		strings.TrimSpace(davError.Message) != "" {
		message = strings.TrimSpace(davError.Message)
	}
	if message == "" {
		message = http.StatusText(response.StatusCode)
	}
	return &HTTPError{
		StatusCode: response.StatusCode, Status: response.Status, Message: message,
		MFARequired: mfaRequired,
	}
}

// Server returns the configured API origin.
func (client *Client) Server() string {
	return strings.TrimRight(client.config.Server, "/")
}

func (client *Client) endpoint(resource string) string {
	return strings.TrimRight(client.config.Server, "/") + "/" + strings.TrimLeft(resource, "/")
}

func (client *Client) authenticate(request *http.Request) {
	if client.config.AuthType == "basic" {
		request.SetBasicAuth(client.config.Username, client.config.Password)
	} else {
		request.Header.Set("Authorization", "Bearer "+client.config.AccessToken)
	}
	if client.config.UserAgent != "" {
		request.Header.Set("User-Agent", client.config.UserAgent)
	}
}
