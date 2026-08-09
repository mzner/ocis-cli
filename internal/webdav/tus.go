package webdav

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bdragon300/tusgo"

	"github.com/mzner/ocis-cli/internal/retry"
)

const maxTUSResponseBody = 4096

// TUSCapabilities contains the upload policy advertised by the oCIS
// capabilities endpoint.
type TUSCapabilities struct {
	Version            string   `json:"version,omitempty"`
	Resumable          string   `json:"resumable,omitempty"`
	Extensions         []string `json:"extensions,omitempty"`
	MaxChunkSize       int64    `json:"maxChunkSize,omitempty"`
	HTTPMethodOverride bool     `json:"httpMethodOverride,omitempty"`
}

// Enabled reports whether the advertised policy is sufficient for resumable
// uploads.
func (capabilities TUSCapabilities) Enabled() bool {
	return capabilities.Version == "1.0.0" &&
		capabilities.Resumable == "1.0.0" &&
		capabilities.MaxChunkSize > 0 &&
		capabilities.supports("creation")
}

func (capabilities TUSCapabilities) supports(extension string) bool {
	for _, candidate := range capabilities.Extensions {
		if strings.TrimSpace(candidate) == extension {
			return true
		}
	}
	return false
}

// UploadSession is the protected client state needed to resume one upload.
type UploadSession struct {
	UploadURL         string
	SourceFingerprint string
	ExpiresAt         int64
}

// UploadSessionStore persists upload URLs outside regular configuration.
type UploadSessionStore interface {
	Load(key string) (UploadSession, bool, error)
	Save(key string, session UploadSession) error
	Delete(key string) error
}

func (client *Client) uploadTUS(
	ctx context.Context,
	local, remote string,
	info os.FileInfo,
	options TransferOptions,
) error {
	if !info.Mode().IsRegular() {
		return errors.New("upload supports regular files only")
	}
	remote = cleanRemote(remote)
	if path.Base(remote) == "." || path.Base(remote) == "/" {
		return errors.New("upload destination must include a file name")
	}
	baseURL, err := url.Parse(client.endpoint(path.Dir(remote)))
	if err != nil {
		return fmt.Errorf("prepare TUS endpoint: %w", err)
	}
	tusHTTP := *client.http
	transport := tusHTTP.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	tusHTTP.Transport = &tusRoundTripper{
		base: transport, progress: options.Progress,
	}
	tusClient := tusgo.NewClient(&tusHTTP, baseURL).WithContext(ctx)
	tusClient.GetRequest = client.tusRequestBuilder(options.TUS)
	response, err := tusClient.UpdateCapabilities()
	if err != nil {
		return client.wrapTUSError("discover TUS capabilities", response, err)
	}

	sourceFingerprint, err := uploadSourceFingerprint(local, info)
	if err != nil {
		return err
	}
	sessionKey := client.uploadSessionKey(remote)
	upload, err := client.loadOrCreateTUSUpload(
		ctx, tusClient, sessionKey, sourceFingerprint, remote, info,
	)
	if err != nil {
		return err
	}
	if options.Progress != nil {
		options.Progress(upload.RemoteOffset)
	}

	file, err := os.Open(local) //nolint:gosec // local is the user-selected upload path
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	for attempt := 0; ; attempt++ {
		if _, err := file.Seek(upload.RemoteOffset, io.SeekStart); err != nil {
			return fmt.Errorf("seek upload source: %w", err)
		}
		stream := tusgo.NewUploadStream(tusClient, &upload)
		stream.ChunkSize = options.TUS.MaxChunkSize
		_, uploadErr := stream.ReadFrom(file)
		if uploadErr == nil && upload.RemoteOffset == info.Size() {
			break
		}
		if uploadErr == nil {
			uploadErr = fmt.Errorf(
				"incomplete TUS upload: server offset %d of %d bytes",
				upload.RemoteOffset, info.Size(),
			)
		}
		if saveErr := client.saveTUSSession(
			sessionKey, sourceFingerprint, upload,
		); saveErr != nil {
			return fmt.Errorf("%w (save resumable upload: %v)", uploadErr, saveErr)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if attempt >= client.config.Retries ||
			!retryableTUSError(uploadErr, stream.LastResponse) {
			return client.wrapTUSError(
				"upload file with TUS", stream.LastResponse, uploadErr,
			)
		}
		delay := time.Duration(0)
		if stream.LastResponse != nil {
			delay = retry.After(stream.LastResponse)
		}
		if err := client.waitRetry(ctx, attempt, delay); err != nil {
			return err
		}
		var refreshed tusgo.Upload
		response, err := tusClient.GetUpload(&refreshed, upload.Location)
		if err != nil {
			return client.wrapTUSError("resume TUS upload", response, err)
		}
		if refreshed.RemoteSize != 0 && refreshed.RemoteSize != info.Size() {
			return fmt.Errorf(
				"resume TUS upload: remote size changed from %d to %d bytes",
				info.Size(), refreshed.RemoteSize,
			)
		}
		refreshed.RemoteSize = info.Size()
		upload = refreshed
	}

	if err := client.deleteTUSSession(sessionKey); err != nil {
		return fmt.Errorf("remove completed upload session: %w", err)
	}
	if options.Verify {
		return client.verifyUploadedSize(ctx, remote, info.Size(), "")
	}
	return nil
}

func (client *Client) loadOrCreateTUSUpload(
	ctx context.Context,
	tusClient *tusgo.Client,
	sessionKey, sourceFingerprint, remote string,
	info os.FileInfo,
) (tusgo.Upload, error) {
	if client.config.Uploads != nil {
		session, found, err := client.config.Uploads.Load(sessionKey)
		if err != nil {
			return tusgo.Upload{}, fmt.Errorf("load resumable upload: %w", err)
		}
		if found {
			expired := session.ExpiresAt > 0 &&
				time.Now().Unix() >= session.ExpiresAt
			if expired || session.SourceFingerprint != sourceFingerprint {
				if err := client.config.Uploads.Delete(sessionKey); err != nil {
					return tusgo.Upload{}, fmt.Errorf(
						"discard stale resumable upload: %w", err,
					)
				}
			} else {
				location, err := client.resolveTUSURL(session.UploadURL)
				if err != nil {
					return tusgo.Upload{}, fmt.Errorf(
						"validate saved resumable upload: %w", err,
					)
				}
				var upload tusgo.Upload
				response, resumeErr := tusClient.GetUpload(&upload, location)
				switch {
				case resumeErr == nil:
					if upload.RemoteSize != 0 &&
						upload.RemoteSize != info.Size() {
						if err := client.config.Uploads.Delete(
							sessionKey,
						); err != nil {
							return tusgo.Upload{}, err
						}
						break
					}
					if upload.RemoteOffset < 0 ||
						upload.RemoteOffset > info.Size() {
						return tusgo.Upload{}, fmt.Errorf(
							"resume TUS upload: invalid server offset %d",
							upload.RemoteOffset,
						)
					}
					upload.RemoteSize = info.Size()
					return upload, nil
				case errors.Is(resumeErr, tusgo.ErrUploadDoesNotExist):
					if err := client.config.Uploads.Delete(
						sessionKey,
					); err != nil {
						return tusgo.Upload{}, err
					}
				default:
					return tusgo.Upload{}, client.wrapTUSError(
						"inspect resumable upload", response, resumeErr,
					)
				}
			}
		}
	}

	var upload tusgo.Upload
	metadata := map[string]string{
		"filename": path.Base(remote),
		"mtime":    strconv.FormatInt(info.ModTime().Unix(), 10),
	}
	response, err := tusClient.CreateUpload(
		&upload, info.Size(), false, metadata,
	)
	if err != nil {
		return tusgo.Upload{}, client.wrapTUSError(
			"create TUS upload", response, err,
		)
	}
	location, err := client.resolveTUSURL(upload.Location)
	if err != nil {
		return tusgo.Upload{}, fmt.Errorf("validate TUS upload location: %w", err)
	}
	upload.Location = location
	upload.RemoteSize = info.Size()
	if err := client.saveTUSSession(
		sessionKey, sourceFingerprint, upload,
	); err != nil {
		return tusgo.Upload{}, fmt.Errorf("save resumable upload: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return tusgo.Upload{}, err
	}
	return upload, nil
}

func (client *Client) saveTUSSession(
	key, sourceFingerprint string, upload tusgo.Upload,
) error {
	if client.config.Uploads == nil {
		return nil
	}
	var expiresAt int64
	if upload.UploadExpired != nil {
		expiresAt = upload.UploadExpired.Unix()
	}
	return client.config.Uploads.Save(key, UploadSession{
		UploadURL: upload.Location, SourceFingerprint: sourceFingerprint,
		ExpiresAt: expiresAt,
	})
}

func (client *Client) deleteTUSSession(key string) error {
	if client.config.Uploads == nil {
		return nil
	}
	return client.config.Uploads.Delete(key)
}

func (client *Client) tusRequestBuilder(
	capabilities TUSCapabilities,
) tusgo.GetRequestFunc {
	return func(
		method, target string, body io.Reader,
		_ *tusgo.Client, _ *http.Client,
	) (*http.Request, error) {
		resolved, err := client.resolveTUSURL(target)
		if err != nil {
			return nil, err
		}
		actualMethod := method
		if capabilities.HTTPMethodOverride && method == http.MethodPatch {
			actualMethod = http.MethodPost
		}
		request, err := http.NewRequest(actualMethod, resolved, body)
		if err != nil {
			return nil, err
		}
		if actualMethod != method {
			request.Header.Set("X-HTTP-Method-Override", method)
		}
		if client.config.AuthType == "basic" {
			request.SetBasicAuth(client.config.Username, client.config.Password)
		} else {
			request.Header.Set(
				"Authorization", "Bearer "+client.config.AccessToken,
			)
		}
		if client.config.UserAgent != "" {
			request.Header.Set("User-Agent", client.config.UserAgent)
		}
		return request, nil
	}
}

func (client *Client) resolveTUSURL(target string) (string, error) {
	base, err := url.Parse(client.config.Server)
	if err != nil {
		return "", err
	}
	candidate, err := url.Parse(target)
	if err != nil {
		return "", err
	}
	candidate = base.ResolveReference(candidate)
	if candidate.Scheme != "http" && candidate.Scheme != "https" {
		return "", fmt.Errorf(
			"unsupported TUS upload URL scheme %q", candidate.Scheme,
		)
	}
	if !sameOrigin(base, candidate) {
		return "", fmt.Errorf(
			"TUS upload URL origin %s does not match configured server %s",
			candidate.Host, base.Host,
		)
	}
	return candidate.String(), nil
}

func sameOrigin(left, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) &&
		strings.EqualFold(left.Hostname(), right.Hostname()) &&
		effectivePort(left) == effectivePort(right)
}

func effectivePort(value *url.URL) string {
	if port := value.Port(); port != "" {
		return port
	}
	if strings.EqualFold(value.Scheme, "https") {
		return "443"
	}
	return "80"
}

func (client *Client) uploadSessionKey(remote string) string {
	sum := sha256.Sum256([]byte(
		client.config.Server + "\x00" + client.config.AccountID +
			"\x00" + client.config.SpaceID +
			"\x00" + cleanRemote(remote),
	))
	return hex.EncodeToString(sum[:])
}

func uploadSourceFingerprint(local string, info os.FileInfo) (string, error) {
	absolute, err := filepath.Abs(local)
	if err != nil {
		return "", fmt.Errorf("fingerprint upload source: %w", err)
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf(
		"%s\x00%d\x00%d",
		filepath.Clean(absolute), info.Size(), info.ModTime().UnixNano(),
	)))
	return hex.EncodeToString(sum[:]), nil
}

func retryableTUSError(err error, response *http.Response) bool {
	if errors.Is(err, tusgo.ErrOffsetsNotSynced) ||
		errors.Is(err, tusgo.ErrChecksumMismatch) {
		return true
	}
	var networkErr net.Error
	if errors.As(err, &networkErr) {
		return true
	}
	return response != nil && retry.RetryableStatus(response.StatusCode)
}

func (client *Client) wrapTUSError(
	operation string, response *http.Response, err error,
) error {
	if response != nil &&
		(response.StatusCode < 200 || response.StatusCode >= 300) {
		message := http.StatusText(response.StatusCode)
		if message == "" {
			message = err.Error()
		}
		return &HTTPError{
			StatusCode: response.StatusCode,
			Status:     response.Status,
			Message:    operation + ": " + message,
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

type tusRoundTripper struct {
	base     http.RoundTripper
	progress func(int64)
}

func (transport *tusRoundTripper) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	response, err := transport.base.RoundTrip(request)
	if response == nil {
		return response, err
	}
	if expires := response.Header.Get("Upload-Expires"); expires != "" {
		if unix, parseErr := strconv.ParseInt(expires, 10, 64); parseErr == nil {
			response.Header.Set(
				"Upload-Expires", time.Unix(unix, 0).UTC().Format(http.TimeFormat),
			)
		}
	}
	if transport.progress != nil &&
		response.StatusCode >= 200 && response.StatusCode < 300 {
		if offset, parseErr := strconv.ParseInt(
			response.Header.Get("Upload-Offset"), 10, 64,
		); parseErr == nil {
			transport.progress(offset)
		}
	}
	if response.Body != nil {
		response.Body = &limitedReadCloser{
			Reader: io.LimitReader(response.Body, maxTUSResponseBody),
			Closer: response.Body,
		}
	}
	return response, err
}

type limitedReadCloser struct {
	io.Reader
	io.Closer
}
