package app

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
	searchapi "github.com/mzner/ocis-cli/internal/search"
)

const defaultSearchLimit = 100

var searchControlPattern = regexp.MustCompile(
	`(?i)(?:^|[\s(])(?:scope|vault)\s*:`,
)

// SearchResult is the stable application and machine-output representation of
// one remote search match.
type SearchResult struct {
	Name         string  `json:"name"`
	Path         string  `json:"path"`
	Type         string  `json:"type"`
	SpaceID      string  `json:"spaceId"`
	SpaceName    string  `json:"spaceName,omitempty"`
	ResourceID   string  `json:"resourceId,omitempty"`
	ParentID     string  `json:"parentId,omitempty"`
	MIMEType     string  `json:"mimeType,omitempty"`
	Size         int64   `json:"size,omitempty"`
	LastModified string  `json:"lastModified,omitempty"`
	ETag         string  `json:"etag,omitempty"`
	Permissions  string  `json:"permissions,omitempty"`
	Highlights   string  `json:"highlights,omitempty"`
	Tags         string  `json:"tags,omitempty"`
	Score        float64 `json:"score,omitempty"`
}

// SearchResults includes the returned items and the server-reported total.
type SearchResults struct {
	Items []SearchResult `json:"items"`
	Total int            `json:"total"`
}

func runSearch(
	ctx context.Context,
	request SearchRequest,
	selectedProfile string,
	options RunOptions,
) error {
	if err := validateSearchRequest(request, options); err != nil {
		return err
	}
	client, err := newClientWithOptions(ctx, selectedProfile, options)
	if err != nil {
		return err
	}
	capabilities, err := client.sharingClient().Capabilities(ctx)
	if err != nil {
		return fmt.Errorf("discover search capability: %w", err)
	}
	if !containsFold(capabilities.DAV.Reports, "search-files") {
		return apperror.Wrap(
			apperror.KindConflict, "search",
			errors.New("server does not advertise the WebDAV search-files report"),
		)
	}

	spaces, err := client.graphClient().ListMyDrives(ctx)
	if err != nil {
		return fmt.Errorf("list accessible spaces: %w", err)
	}
	spaceNames := make(map[string]string, len(spaces))
	for _, candidate := range spaces {
		spaceNames[candidate.ID] = candidate.Name
	}

	var selected *space
	if !request.AllSpaces {
		if err := client.selectSpace(options.Space); err != nil {
			return err
		}
		if client.space != nil {
			selected = client.space
		} else {
			personal, err := personalSpace(spaces)
			if err != nil {
				return err
			}
			selected = &personal
		}
	}

	pattern, err := buildSearchPattern(request, selected)
	if err != nil {
		return err
	}
	limit := request.Limit
	if limit == 0 {
		limit = defaultSearchLimit
	}
	response, err := client.searchClient().Search(ctx, searchapi.Request{
		Pattern: pattern, Limit: limit,
	})
	if err != nil {
		if request.Content {
			return fmt.Errorf(
				"content search failed; the server must enable content extraction: %w",
				err,
			)
		}
		return err
	}
	result := SearchResults{
		Items: make([]SearchResult, 0, len(response.Items)),
		Total: response.Total,
	}
	for _, item := range response.Items {
		result.Items = append(result.Items, mapSearchResult(
			item, spaceNames[item.SpaceID],
		))
	}
	if options.OutputMode == appoutput.JSONL {
		return writeOutput(options, "search-result", result.Items)
	}
	if options.OutputMode == appoutput.JSON {
		return writeOutput(options, "search-results", result)
	}
	for _, item := range result.Items {
		size := "-"
		if item.Type == "file" {
			size = strconv.FormatInt(item.Size, 10)
		}
		spaceLabel := item.SpaceName
		if spaceLabel == "" {
			spaceLabel = item.SpaceID
		}
		_, _ = fmt.Fprintf(
			options.Out, "%-10s %10s  %-24s  %s\n",
			item.Type, size, spaceLabel, item.Path,
		)
	}
	if result.Total > len(result.Items) {
		_, _ = fmt.Fprintf(
			options.Out, "Showing %d of %d matches; increase --limit to return more.\n",
			len(result.Items), result.Total,
		)
	}
	return nil
}

func validateSearchRequest(request SearchRequest, options RunOptions) error {
	if strings.TrimSpace(request.Query) == "" {
		return apperror.Wrap(
			apperror.KindUsage, "search", errors.New("query cannot be empty"),
		)
	}
	if request.AllSpaces && options.Space != "" {
		return apperror.Wrap(
			apperror.KindUsage, "search",
			errors.New("--all-spaces and --space are mutually exclusive"),
		)
	}
	if request.AllSpaces && request.Path != "" {
		return apperror.Wrap(
			apperror.KindUsage, "search",
			errors.New("--all-spaces and --path are mutually exclusive"),
		)
	}
	if request.Raw && request.Content {
		return apperror.Wrap(
			apperror.KindUsage, "search",
			errors.New("--raw and --content are mutually exclusive"),
		)
	}
	if request.Limit < 0 || request.Limit > 1000 {
		return apperror.Wrap(
			apperror.KindUsage, "search",
			errors.New("--limit must be between 1 and 1000"),
		)
	}
	if request.MinSize != nil && request.MaxSize != nil &&
		*request.MinSize > *request.MaxSize {
		return apperror.Wrap(
			apperror.KindUsage, "search",
			errors.New("--min-size cannot exceed --max-size"),
		)
	}
	if request.ModifiedAfter != nil && request.ModifiedBefore != nil &&
		request.ModifiedAfter.After(*request.ModifiedBefore) {
		return apperror.Wrap(
			apperror.KindUsage, "search",
			errors.New("--modified-after cannot be later than --modified-before"),
		)
	}
	switch request.ResourceType {
	case "", "file", "directory":
	default:
		return apperror.Wrap(
			apperror.KindUsage, "search",
			fmt.Errorf("invalid type %q; use file or directory", request.ResourceType),
		)
	}
	if containsSearchControl(request.Query) {
		return apperror.Wrap(
			apperror.KindUsage, "search",
			errors.New("query cannot contain scope: or vault:; use --path, --space, or --all-spaces"),
		)
	}
	return nil
}

func buildSearchPattern(request SearchRequest, selected *space) (string, error) {
	query := strings.TrimSpace(request.Query)
	if !request.Raw {
		field := "name"
		value := "*" + escapeKQLString(query) + "*"
		if request.Content {
			field = "content"
			value = escapeKQLString(query)
		}
		query = field + `:"` + value + `"`
	} else if request.Content {
		return "", apperror.Wrap(
			apperror.KindUsage, "search",
			errors.New("--raw and --content are mutually exclusive"),
		)
	}
	filters := make([]string, 0, 7)
	switch request.ResourceType {
	case "file":
		filters = append(filters, "mediatype:file")
	case "directory":
		filters = append(filters, "mediatype:folder")
	}
	if request.MediaType != "" {
		filters = append(filters, `mediatype:"`+escapeKQLString(request.MediaType)+`"`)
	}
	if request.MinSize != nil {
		filters = append(filters, "size>="+strconv.FormatInt(*request.MinSize, 10))
	}
	if request.MaxSize != nil {
		filters = append(filters, "size<="+strconv.FormatInt(*request.MaxSize, 10))
	}
	if request.ModifiedAfter != nil {
		filters = append(filters, "mtime>="+formatSearchTime(*request.ModifiedAfter))
	}
	if request.ModifiedBefore != nil {
		filters = append(filters, "mtime<="+formatSearchTime(*request.ModifiedBefore))
	}
	if selected != nil {
		scope, err := searchScope(*selected, request.Path)
		if err != nil {
			return "", err
		}
		filters = append(filters, "scope:"+scope)
	} else if request.Path != "" && cleanRemote(request.Path) != "/" {
		return "", apperror.Wrap(
			apperror.KindUsage, "search",
			errors.New("--path requires one Space; omit --all-spaces or use --space"),
		)
	}
	if len(filters) == 0 {
		return query, nil
	}
	return "(" + query + ") AND " + strings.Join(filters, " AND "), nil
}

func searchScope(selected space, remote string) (string, error) {
	id := selected.ID
	if !strings.Contains(id, "!") {
		_, opaque, found := strings.Cut(id, "$")
		if !found || opaque == "" {
			return "", fmt.Errorf("space %q has an unsupported ID %q", selected.Name, id)
		}
		id += "!" + opaque
	}
	cleaned := cleanRemote(remote)
	if remote == "" || cleaned == "/" {
		return id, nil
	}
	return id + "/" + strings.TrimPrefix(path.Clean(cleaned), "/"), nil
}

func personalSpace(spaces []space) (space, error) {
	for _, candidate := range spaces {
		if candidate.DriveType == "personal" {
			return candidate, nil
		}
	}
	return space{}, errors.New(
		"personal Space was not returned by the server; select one with --space or use --all-spaces",
	)
}

func mapSearchResult(item searchapi.Item, spaceName string) SearchResult {
	return SearchResult{
		Name: item.Name, Path: item.Path, Type: item.Type,
		SpaceID: item.SpaceID, SpaceName: spaceName,
		ResourceID: item.ResourceID, ParentID: item.ParentID,
		MIMEType: item.MIMEType, Size: item.Size,
		LastModified: item.LastModified, ETag: item.ETag,
		Permissions: item.Permissions, Highlights: item.Highlights,
		Tags: item.Tags, Score: item.Score,
	}
}

func formatSearchTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339)
}

func escapeKQLString(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

func containsSearchControl(query string) bool {
	return searchControlPattern.MatchString(query)
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}
