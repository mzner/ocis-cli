package search

import (
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Item describes one search result.
type Item struct {
	Name         string  `json:"name"`
	Path         string  `json:"path"`
	Type         string  `json:"type"`
	SpaceID      string  `json:"spaceId"`
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

type multiStatus struct {
	Responses []davResponse `xml:"response"`
}

type davResponse struct {
	Href     string        `xml:"href"`
	PropStat []davPropStat `xml:"propstat"`
}

type davPropStat struct {
	Status string  `xml:"status"`
	Prop   davProp `xml:"prop"`
}

type davProp struct {
	FileID        string `xml:"fileid"`
	ParentID      string `xml:"file-parent"`
	Name          string `xml:"name"`
	LastModified  string `xml:"getlastmodified"`
	ContentType   string `xml:"getcontenttype"`
	ContentLength string `xml:"getcontentlength"`
	Size          string `xml:"size"`
	Permissions   string `xml:"permissions"`
	Highlights    string `xml:"highlights"`
	SpaceID       string `xml:"spaceid"`
	Tags          string `xml:"tags"`
	ETag          string `xml:"getetag"`
	Score         string `xml:"score"`
	ResourceType  struct {
		Collection *struct{} `xml:"collection"`
	} `xml:"resourcetype"`
}

var contentRangePattern = regexp.MustCompile(
	`(?i)^rows\s+\d+-\d+/(\d+)$`,
)

// Decode maps a WebDAV multistatus search response.
func Decode(data []byte) ([]Item, error) {
	var result multiStatus
	if err := xml.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse search response: %w", err)
	}
	items := make([]Item, 0, len(result.Responses))
	for _, response := range result.Responses {
		value, ok := successfulProp(response.PropStat)
		if !ok {
			continue
		}
		itemPath, hrefSpaceID, err := responseLocation(response.Href)
		if err != nil {
			return nil, err
		}
		sizeText := value.ContentLength
		if sizeText == "" {
			sizeText = value.Size
		}
		size, err := parseInt(sizeText, "size")
		if err != nil {
			return nil, err
		}
		score, err := parseFloat(value.Score, "score")
		if err != nil {
			return nil, err
		}
		kind := "file"
		if value.ResourceType.Collection != nil {
			kind = "directory"
		}
		name := value.Name
		if name == "" {
			name = path.Base(itemPath)
		}
		spaceID := value.SpaceID
		if spaceID == "" {
			spaceID = hrefSpaceID
		}
		items = append(items, Item{
			Name: name, Path: itemPath, Type: kind, SpaceID: spaceID,
			ResourceID: value.FileID, ParentID: value.ParentID,
			MIMEType: value.ContentType, Size: size,
			LastModified: value.LastModified, ETag: value.ETag,
			Permissions: value.Permissions, Highlights: value.Highlights,
			Tags: value.Tags, Score: score,
		})
	}
	return items, nil
}

// ParseContentRange returns the total number of matches advertised by oCIS.
func ParseContentRange(value string) (int, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	match := contentRangePattern.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return 0, fmt.Errorf("invalid search Content-Range %q", value)
	}
	total, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, fmt.Errorf("invalid search total %q: %w", match[1], err)
	}
	return total, nil
}

func successfulProp(stats []davPropStat) (davProp, bool) {
	for _, value := range stats {
		if strings.Contains(value.Status, " 200 ") {
			return value.Prop, true
		}
	}
	return davProp{}, false
}

func responseLocation(href string) (string, string, error) {
	parsed, err := url.Parse(href)
	if err != nil {
		return "", "", fmt.Errorf("parse search result href: %w", err)
	}
	decoded, err := url.PathUnescape(parsed.EscapedPath())
	if err != nil {
		return "", "", fmt.Errorf("decode search result href: %w", err)
	}
	const marker = "/dav/spaces/"
	index := strings.Index(decoded, marker)
	if index < 0 {
		return "", "", fmt.Errorf("unexpected search result href %q", href)
	}
	remainder := strings.TrimPrefix(decoded[index+len(marker):], "/")
	spaceID, itemPath, found := strings.Cut(remainder, "/")
	if spaceID == "" {
		return "", "", fmt.Errorf("search result href contains no Space ID: %q", href)
	}
	if !found || itemPath == "" {
		return "/", spaceID, nil
	}
	return "/" + strings.TrimPrefix(path.Clean("/"+itemPath), "/"), spaceID, nil
}

func parseInt(value, field string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	result, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid search %s %q: %w", field, value, err)
	}
	return result, nil
}

func parseFloat(value, field string) (float64, error) {
	if value == "" {
		return 0, nil
	}
	result, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid search %s %q: %w", field, value, err)
	}
	if result < 0 {
		return 0, errors.New("search score cannot be negative")
	}
	return result, nil
}
