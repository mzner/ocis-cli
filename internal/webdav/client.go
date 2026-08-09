package webdav

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mzner/ocis-cli/internal/logging"
	"github.com/mzner/ocis-cli/internal/retry"
	"github.com/mzner/ocis-cli/internal/transfer"
)

// ErrRemoteIsDirectory indicates that a guarded delete targeted a collection.
var ErrRemoteIsDirectory = errors.New("remote path is a directory")

// RemoveOptions controls recursive and conditional remote deletion.
type RemoveOptions struct {
	Recursive    bool
	ExpectedETag string
}

// MoveOptions controls a fail-closed remote move.
type MoveOptions struct {
	Overwrite    bool
	ExpectedETag string
}

// Config contains the connection and authentication values used by Client.
type Config struct {
	Server      string
	Username    string
	AccountID   string
	AuthType    string
	Password    string
	AccessToken string
	SpaceID     string
	UserAgent   string
	Retries     int
	RetryWait   time.Duration
	Logger      logging.Logger
	Uploads     UploadSessionStore
}

// TransferOptions controls overwrite, resume, and integrity behavior for one
// uploaded or downloaded file.
type TransferOptions struct {
	NoClobber    bool
	Resume       bool
	Verify       bool
	Progress     func(int64)
	TUS          TUSCapabilities
	ExpectedETag string
}

// HTTPError reports a non-successful WebDAV HTTP response.
type HTTPError struct {
	StatusCode int
	Status     string
	Message    string
}

// Capabilities contains DAV compliance tokens and allowed HTTP methods.
type Capabilities struct {
	DAV   []string `json:"dav"`
	Allow []string `json:"allow"`
}

func (err *HTTPError) Error() string {
	return fmt.Sprintf("%s: %s", err.Status, err.Message)
}

// HTTPStatusCode exposes the response status to the application boundary.
func (err *HTTPError) HTTPStatusCode() int {
	return err.StatusCode
}

// StatusCode returns the HTTP status embedded in err, or zero.
func StatusCode(err error) int {
	var responseErr *HTTPError
	if errors.As(err, &responseErr) {
		return responseErr.StatusCode
	}
	return 0
}

// Client performs WebDAV operations.
type Client struct {
	config Config
	http   *http.Client
}

// NewClient constructs a WebDAV client.
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

// List returns the direct children of remote.
func (client *Client) List(ctx context.Context, remote string) ([]Item, error) {
	body := strings.NewReader(metadataPropfind)
	data, err := client.request(ctx, "PROPFIND", remote, body, "1")
	if err != nil {
		return nil, err
	}
	return DecodeList(data, remote)
}

// Stat returns metadata for one remote resource.
func (client *Client) Stat(ctx context.Context, remote string) (Item, error) {
	body := strings.NewReader(metadataPropfind)
	data, err := client.request(ctx, "PROPFIND", remote, body, "0")
	if err != nil {
		return Item{}, err
	}
	return DecodeStat(data, remote)
}

const metadataPropfind = `<?xml version="1.0"?>
<d:propfind xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns">
  <d:prop>
    <d:displayname/>
    <d:resourcetype/>
    <d:getcontentlength/>
    <d:getlastmodified/>
    <d:getetag/>
    <oc:fileid/>
    <oc:id/>
    <oc:tags/>
    <oc:favorite/>
    <oc:checksums/>
  </d:prop>
</d:propfind>`

// Capabilities discovers the DAV compliance classes and methods advertised for
// the user's files endpoint.
func (client *Client) Capabilities(ctx context.Context) (Capabilities, error) {
	response, err := client.doWithRetry(ctx, func() (*http.Request, error) {
		return client.newRequest(ctx, http.MethodOptions, "/", nil)
	})
	if err != nil {
		return Capabilities{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Capabilities{}, responseError(response)
	}
	return Capabilities{
		DAV:   splitHeader(response.Header.Values("DAV")),
		Allow: splitHeader(response.Header.Values("Allow")),
	}, nil
}

func splitHeader(values []string) []string {
	result := make([]string, 0)
	for _, value := range values {
		for token := range strings.SplitSeq(value, ",") {
			if token = strings.TrimSpace(token); token != "" {
				result = append(result, token)
			}
		}
	}
	return result
}

// Upload uploads one regular local file.
func (client *Client) Upload(ctx context.Context, local, remote string) error {
	return client.UploadWithOptions(ctx, local, remote, TransferOptions{})
}

// UploadWithOptions uploads one regular file according to the transfer policy.
func (client *Client) UploadWithOptions(ctx context.Context, local, remote string, options TransferOptions) error {
	if options.NoClobber {
		return client.uploadWithoutClobber(ctx, local, remote, options)
	}
	if options.TUS.Enabled() && options.ExpectedETag == "" {
		info, err := os.Stat(local)
		if err != nil {
			return err
		}
		if info.Size() > 0 {
			return client.uploadTUS(ctx, local, remote, info, options)
		}
	}
	return client.uploadDirect(ctx, local, remote, options)
}

func (client *Client) uploadDirect(
	ctx context.Context, local, remote string, options TransferOptions,
) error {
	info, err := os.Stat(local)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("upload supports regular files only")
	}
	response, err := client.doWithRetry(ctx, func() (*http.Request, error) {
		if options.Progress != nil {
			options.Progress(0)
		}
		var file *os.File
		body := io.Reader(http.NoBody)
		if info.Size() > 0 {
			var openErr error
			file, openErr = os.Open(local) //nolint:gosec // local is the user-selected upload path
			if openErr != nil {
				return nil, openErr
			}
			body = file
			if options.Progress != nil {
				body = &progressReader{reader: file, report: options.Progress}
			}
		}
		request, requestErr := client.newRequest(ctx, http.MethodPut, remote, body)
		if requestErr != nil {
			if file != nil {
				_ = file.Close()
			}
			return nil, requestErr
		}
		request.ContentLength = info.Size()
		if options.ExpectedETag != "" {
			request.Header.Set("If-Match", options.ExpectedETag)
		}
		return request, nil
	})
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return responseError(response)
	}
	if options.Verify {
		return client.verifyUploadedSize(
			ctx, remote, info.Size(), response.Header.Get("ETag"),
		)
	}
	return nil
}

func (client *Client) verifyUploadedSize(
	ctx context.Context, remote string, localSize int64, uploadedETag string,
) error {
	remoteInfo, err := client.Stat(ctx, remote)
	if err != nil {
		return fmt.Errorf("verify upload: %w", err)
	}
	if remoteInfo.Size != localSize {
		return fmt.Errorf(
			"verify upload: size mismatch: local %d bytes, remote %d bytes",
			localSize, remoteInfo.Size,
		)
	}
	if uploadedETag != "" && remoteInfo.ETag != "" &&
		uploadedETag != remoteInfo.ETag {
		return fmt.Errorf(
			"verify upload: ETag changed from %s to %s",
			uploadedETag, remoteInfo.ETag,
		)
	}
	return nil
}

func (client *Client) uploadWithoutClobber(
	ctx context.Context, local, remote string, options TransferOptions,
) error {
	temporary, err := temporaryUploadPath(remote)
	if err != nil {
		return err
	}
	options.NoClobber = false
	if err := client.uploadDirect(ctx, local, temporary, options); err != nil {
		return err
	}
	if err := client.Move(ctx, temporary, remote, false); err != nil {
		cleanupCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx), 30*time.Second,
		)
		defer cancel()
		if cleanupErr := client.Remove(
			cleanupCtx, temporary, false,
		); cleanupErr != nil {
			return fmt.Errorf(
				"%w (cleanup temporary upload %s: %v)",
				err, temporary, cleanupErr,
			)
		}
		return err
	}
	return nil
}

func temporaryUploadPath(remote string) (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate temporary upload name: %w", err)
	}
	cleaned := cleanRemote(remote)
	return path.Join(
		path.Dir(cleaned),
		"."+path.Base(cleaned)+".ocis-cli-"+hex.EncodeToString(random)+".part",
	), nil
}

// Download downloads one remote file atomically.
func (client *Client) Download(ctx context.Context, remote, local string) error {
	return client.DownloadWithOptions(ctx, remote, local, TransferOptions{Resume: true})
}

// DownloadToWriter streams one remote file to destination. Requests may be
// retried before a successful response is returned, but a response body is
// never replayed after bytes have been written.
func (client *Client) DownloadToWriter(
	ctx context.Context, remote string, destination io.Writer,
) error {
	response, err := client.doWithRetry(ctx, func() (*http.Request, error) {
		return client.newRequest(ctx, http.MethodGet, remote, nil)
	})
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return responseError(response)
	}
	written, err := io.Copy(destination, response.Body)
	if err != nil {
		return err
	}
	if response.ContentLength >= 0 && written != response.ContentLength {
		return fmt.Errorf(
			"verify download: size mismatch: expected %d bytes, received %d bytes",
			response.ContentLength, written,
		)
	}
	return nil
}

// DownloadWithOptions downloads one remote file atomically. A retained .part
// file is resumed with a byte range on the next attempt when enabled.
//
// Resuming is only safe while the remote entity that produced the .part file is
// unchanged, because the retained prefix is never re-read. Every ranged request
// therefore carries an If-Range validator, and a 200 response to that request
// means the validator did not match: the .part prefix is discarded and the
// current entity is written from offset zero.
//
// The recorded validator must never describe bytes it did not produce, so it is
// invalidated before a restart truncates the prefix and recorded again only
// once the file is empty; an interruption anywhere in between leaves an
// unlabelled prefix, which the next attempt discards rather than trusts.
func (client *Client) DownloadWithOptions(ctx context.Context, remote, local string, options TransferOptions) error {
	if info, err := os.Stat(local); err == nil && info.IsDir() {
		local = filepath.Join(local, path.Base(cleanRemote(remote)))
	}
	if options.NoClobber {
		if _, err := os.Stat(local); err == nil {
			return &HTTPError{StatusCode: http.StatusPreconditionFailed, Status: "destination exists", Message: local}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	temporary := local + ".part"
	if !options.Resume {
		if err := discardPartial(temporary); err != nil {
			return err
		}
	}
	var expectedSize int64 = -1
	var downloadedETag string
	for attempt := 0; attempt <= client.config.Retries; attempt++ {
		offset := int64(0)
		validator := ""
		if options.Resume {
			if info, err := os.Stat(temporary); err == nil {
				// A retained prefix may only be reused when its originating
				// entity validator is known; otherwise it is not provably the
				// same remote content and must be discarded.
				saved, err := readPartValidator(temporary)
				if err != nil {
					return err
				}
				if saved == "" {
					if err := discardPartial(temporary); err != nil {
						return err
					}
				} else {
					offset, validator = info.Size(), saved
				}
			}
		}
		request, err := client.newRequest(ctx, http.MethodGet, remote, nil)
		if err != nil {
			return err
		}
		if offset > 0 {
			request.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
			request.Header.Set("If-Range", validator)
		}
		if options.ExpectedETag != "" {
			request.Header.Set("If-Match", options.ExpectedETag)
		}
		response, err := client.http.Do(request)
		if err != nil {
			if attempt < client.config.Retries {
				if waitErr := client.waitRetry(ctx, attempt, 0); waitErr != nil {
					return waitErr
				}
				continue
			}
			return err
		}
		if retry.RetryableStatus(response.StatusCode) && attempt < client.config.Retries {
			delay := retry.After(response)
			_ = response.Body.Close()
			if err := client.waitRetry(ctx, attempt, delay); err != nil {
				return err
			}
			continue
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			defer func() { _ = response.Body.Close() }()
			return responseError(response)
		}
		responseETag := response.Header.Get("ETag")
		flags := os.O_CREATE | os.O_WRONLY
		restart := true
		if response.StatusCode == http.StatusPartialContent && offset > 0 {
			rangeStart, rangeSize, ok := parseContentRange(response.Header.Get("Content-Range"))
			if !ok || rangeStart != offset {
				_ = response.Body.Close()
				return fmt.Errorf(
					"resume download: invalid Content-Range %q for offset %d",
					response.Header.Get("Content-Range"), offset,
				)
			}
			// The continuation was granted for the saved validator, so an ETag
			// naming a different entity would splice two entities together. The
			// saved validator is kept: it still describes the retained prefix.
			if responseETag != "" && responseETag != validator {
				_ = response.Body.Close()
				return fmt.Errorf(
					"resume download: server returned validator %s for a range "+
						"requested with validator %s",
					responseETag, validator,
				)
			}
			restart = false
			flags |= os.O_APPEND
			expectedSize = rangeSize
			downloadedETag = validator
		} else {
			flags |= os.O_TRUNC
			offset = 0
			expectedSize = response.ContentLength
			downloadedETag = responseETag
		}
		// A restart replaces the retained prefix, so the validator describing it
		// is invalidated before the stale bytes are removed and only recorded
		// again once the file is empty. Publishing it earlier would let an
		// interruption in between leave stale bytes labelled with the new
		// entity, which is exactly the pairing a later resume must never trust.
		if restart {
			if err := discardPartValidator(temporary); err != nil {
				_ = response.Body.Close()
				return err
			}
		}
		file, openErr := os.OpenFile(temporary, flags, 0600) //nolint:gosec // temporary is derived from the user-selected download destination
		if openErr != nil {
			_ = response.Body.Close()
			return openErr
		}
		if restart {
			// Recorded after truncation, so the validator can only ever describe
			// bytes that came from this response.
			if err := writePartValidator(temporary, downloadedETag); err != nil {
				_ = file.Close()
				_ = response.Body.Close()
				return err
			}
		}
		if options.Progress != nil {
			options.Progress(offset)
		}
		destination := io.Writer(file)
		if options.Progress != nil {
			destination = &progressWriter{writer: file, completed: offset, report: options.Progress}
		}
		_, copyErr := io.Copy(destination, response.Body)
		closeErr := file.Close()
		_ = response.Body.Close()
		if copyErr == nil && closeErr == nil {
			break
		}
		if attempt >= client.config.Retries {
			if copyErr != nil {
				return copyErr
			}
			return closeErr
		}
		if err := client.waitRetry(ctx, attempt, 0); err != nil {
			return err
		}
	}
	info, err := os.Stat(temporary)
	if err != nil {
		return err
	}
	if expectedSize >= 0 && info.Size() != expectedSize {
		return fmt.Errorf("verify download: size mismatch: expected %d bytes, received %d bytes", expectedSize, info.Size())
	}
	if options.Verify {
		remoteInfo, statErr := client.Stat(ctx, remote)
		if statErr != nil {
			return fmt.Errorf("verify download: %w", statErr)
		}
		if remoteInfo.Size != info.Size() {
			return fmt.Errorf("verify download: size mismatch: remote %d bytes, local %d bytes", remoteInfo.Size, info.Size())
		}
		if downloadedETag != "" && remoteInfo.ETag != "" && downloadedETag != remoteInfo.ETag {
			return fmt.Errorf("verify download: remote ETag changed during transfer")
		}
	}
	// The sidecar is dropped before the destination is committed, so the only
	// fallible step left is the commit itself. Once the destination has changed
	// the transfer has succeeded, and reporting a later tidy-up failure as a
	// transfer failure would make a sync caller skip its baseline update for a
	// file that is already in place.
	if err := discardPartValidator(temporary); err != nil {
		client.config.Logger.Debug(
			"download sidecar cleanup failed", "path", partValidatorPath(temporary),
			"reason", err,
		)
	}
	return transfer.ReplaceFile(temporary, local)
}

// partValidatorPath returns the sidecar holding the entity validator for a
// retained partial download.
func partValidatorPath(temporary string) string {
	return temporary + ".etag"
}

// readPartValidator returns the validator recorded for a retained partial
// download, or an empty value when none is known.
func readPartValidator(temporary string) (string, error) {
	data, err := os.ReadFile(partValidatorPath(temporary)) //nolint:gosec // derived from the user-selected download destination
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// writePartValidator records the validator of the entity currently being
// written, removing any stale value when the server supplies none.
func writePartValidator(temporary, validator string) error {
	if strings.TrimSpace(validator) == "" {
		return discardPartValidator(temporary)
	}
	return os.WriteFile(partValidatorPath(temporary), []byte(validator), 0600)
}

func discardPartValidator(temporary string) error {
	if err := os.Remove(partValidatorPath(temporary)); err != nil &&
		!errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// discardPartial removes a partial download and its recorded validator.
func discardPartial(temporary string) error {
	if err := os.Remove(temporary); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return discardPartValidator(temporary)
}

type progressReader struct {
	reader    io.ReadCloser
	completed int64
	report    func(int64)
}

func (reader *progressReader) Close() error {
	return reader.reader.Close()
}

func (reader *progressReader) Read(data []byte) (int, error) {
	count, err := reader.reader.Read(data)
	reader.completed += int64(count)
	reader.report(reader.completed)
	return count, err
}

type progressWriter struct {
	writer    io.Writer
	completed int64
	report    func(int64)
}

func (writer *progressWriter) Write(data []byte) (int, error) {
	count, err := writer.writer.Write(data)
	writer.completed += int64(count)
	writer.report(writer.completed)
	return count, err
}

// Mkdir creates a remote collection and accepts an already-existing one.
func (client *Client) Mkdir(ctx context.Context, remote string) error {
	response, err := client.doWithRetry(ctx, func() (*http.Request, error) {
		return client.newRequest(ctx, "MKCOL", remote, nil)
	})
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusCreated || response.StatusCode == http.StatusMethodNotAllowed ||
		response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	return responseError(response)
}

// Move moves a remote resource.
func (client *Client) Move(ctx context.Context, source, destination string, overwrite bool) error {
	return client.MoveWithOptions(ctx, source, destination, MoveOptions{
		Overwrite: overwrite,
	})
}

// MoveWithOptions moves a remote resource with an optional source ETag
// precondition.
func (client *Client) MoveWithOptions(
	ctx context.Context,
	source string,
	destination string,
	options MoveOptions,
) error {
	return client.copyOrMoveWithETag(
		ctx, "MOVE", source, destination, options.Overwrite,
		options.ExpectedETag,
	)
}

// Copy copies a remote resource.
func (client *Client) Copy(ctx context.Context, source, destination string, overwrite bool) error {
	return client.copyOrMove(ctx, "COPY", source, destination, overwrite)
}

// CopyWithOptions copies a remote resource with an optional source ETag
// precondition.
func (client *Client) CopyWithOptions(
	ctx context.Context,
	source string,
	destination string,
	options MoveOptions,
) error {
	return client.copyOrMoveWithETag(
		ctx, "COPY", source, destination, options.Overwrite,
		options.ExpectedETag,
	)
}

func (client *Client) copyOrMove(ctx context.Context, method, source, destination string, overwrite bool) error {
	return client.copyOrMoveWithETag(
		ctx, method, source, destination, overwrite, "",
	)
}

func (client *Client) copyOrMoveWithETag(
	ctx context.Context,
	method string,
	source string,
	destination string,
	overwrite bool,
	expectedETag string,
) error {
	response, err := client.doWithRetry(ctx, func() (*http.Request, error) {
		request, requestErr := client.newRequest(ctx, method, source, nil)
		if requestErr != nil {
			return nil, requestErr
		}
		request.Header.Set("Destination", client.endpoint(destination))
		if expectedETag != "" {
			request.Header.Set("If-Match", expectedETag)
		}
		if overwrite {
			request.Header.Set("Overwrite", "T")
		} else {
			request.Header.Set("Overwrite", "F")
		}
		return request, nil
	})
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return responseError(response)
	}
	return nil
}

// Remove deletes a remote resource after enforcing the recursive guard.
func (client *Client) Remove(ctx context.Context, remote string, recursive bool) error {
	return client.RemoveWithOptions(ctx, remote, RemoveOptions{
		Recursive: recursive,
	})
}

// RemoveWithOptions deletes a remote resource with an optional ETag
// precondition.
func (client *Client) RemoveWithOptions(
	ctx context.Context,
	remote string,
	options RemoveOptions,
) error {
	meta, err := client.Stat(ctx, remote)
	if err != nil {
		return err
	}
	if meta.Type == "directory" && !options.Recursive {
		return fmt.Errorf("%w: use --recursive to delete %s", ErrRemoteIsDirectory, cleanRemote(remote))
	}
	response, err := client.doWithRetry(ctx, func() (*http.Request, error) {
		request, requestErr := client.newRequest(
			ctx, http.MethodDelete, remote, nil,
		)
		if requestErr == nil && options.ExpectedETag != "" {
			request.Header.Set("If-Match", options.ExpectedETag)
		}
		return request, requestErr
	})
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return responseError(response)
	}
	return nil
}

func (client *Client) request(ctx context.Context, method, remote string, body io.Reader, depth string) ([]byte, error) {
	bodyBytes, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	response, err := client.doWithRetry(ctx, func() (*http.Request, error) {
		request, requestErr := client.newRequest(ctx, method, remote, strings.NewReader(string(bodyBytes)))
		if requestErr != nil {
			return nil, requestErr
		}
		if depth != "" {
			request.Header.Set("Depth", depth)
		}
		if method == "PROPFIND" || method == "PROPPATCH" {
			request.Header.Set("Content-Type", "application/xml")
		}
		return request, nil
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, responseError(response)
	}
	return io.ReadAll(response.Body)
}

func (client *Client) newRequest(ctx context.Context, method, remote string, body io.Reader) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, client.endpoint(remote), body)
	if err != nil {
		return nil, err
	}
	if client.config.AuthType == "basic" {
		request.SetBasicAuth(client.config.Username, client.config.Password)
	} else {
		request.Header.Set("Authorization", "Bearer "+client.config.AccessToken)
	}
	if client.config.UserAgent != "" {
		request.Header.Set("User-Agent", client.config.UserAgent)
	}
	return request, nil
}

func (client *Client) doWithRetry(ctx context.Context, build func() (*http.Request, error)) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		request, err := build()
		if err != nil {
			return nil, err
		}
		response, err := client.http.Do(request)
		if err == nil && (!retry.RetryableStatus(response.StatusCode) || attempt >= client.config.Retries) {
			return response, nil
		}
		if err != nil && attempt >= client.config.Retries {
			return nil, err
		}
		delay := time.Duration(0)
		status := "transport_error"
		if response != nil {
			delay = retry.After(response)
			status = response.Status
			_ = response.Body.Close()
		}
		client.config.Logger.Debug(
			"retrying WebDAV request",
			"method", request.Method, "attempt", attempt+2, "reason", status,
		)
		if err := client.waitRetry(ctx, attempt, delay); err != nil {
			return nil, err
		}
	}
}

// waitRetry pauses before the next attempt using the configured base wait and
// the shared bounded-delay policy.
func (client *Client) waitRetry(ctx context.Context, attempt int, delay time.Duration) error {
	return retry.Wait(ctx, client.config.RetryWait, attempt, delay)
}

func parseContentRange(value string) (start, size int64, ok bool) {
	units, rangeAndSize, found := strings.Cut(value, " ")
	if !found || !strings.EqualFold(units, "bytes") {
		return 0, 0, false
	}
	byteRange, total, found := strings.Cut(rangeAndSize, "/")
	if !found {
		return 0, 0, false
	}
	first, _, found := strings.Cut(byteRange, "-")
	if !found {
		return 0, 0, false
	}
	start, startErr := strconv.ParseInt(first, 10, 64)
	size, sizeErr := strconv.ParseInt(total, 10, 64)
	return start, size, startErr == nil && sizeErr == nil && start >= 0 && size >= 0
}

func (client *Client) endpoint(remote string) string {
	if client.config.SpaceID != "" {
		return client.config.Server + "/dav/spaces/" +
			url.PathEscape(client.config.SpaceID) + escapeRemote(remote)
	}
	return client.config.Server + "/remote.php/dav/files/" +
		url.PathEscape(client.config.Username) + escapeRemote(remote)
}

func escapeRemote(remote string) string {
	parts := strings.Split(strings.TrimPrefix(cleanRemote(remote), "/"), "/")
	for index := range parts {
		parts[index] = url.PathEscape(parts[index])
	}
	if len(parts) == 1 && parts[0] == "" {
		return "/"
	}
	return "/" + strings.Join(parts, "/")
}

func responseError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	message := strings.TrimSpace(string(body))
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
	return &HTTPError{StatusCode: response.StatusCode, Status: response.Status, Message: message}
}
