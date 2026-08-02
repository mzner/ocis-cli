// Package webdav implements DAV protocol mapping independently of command and
// application orchestration.
package webdav

import (
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"
)

// Item describes a remote DAV resource.
type Item struct {
	Name         string     `json:"name"`
	Path         string     `json:"path"`
	Type         string     `json:"type"`
	ResourceID   string     `json:"resourceId,omitempty"`
	Size         int64      `json:"size,omitempty"`
	LastModified string     `json:"lastModified,omitempty"`
	ETag         string     `json:"etag,omitempty"`
	Tags         []string   `json:"tags,omitempty"`
	Favorite     *bool      `json:"favorite,omitempty"`
	Checksums    []Checksum `json:"checksums,omitempty"`
}

// Checksum is one server-provided content checksum.
type Checksum struct {
	Algorithm string `json:"algorithm"`
	Value     string `json:"value"`
}

type multiStatus struct {
	Responses []response `xml:"response"`
}

type response struct {
	Href     string     `xml:"href"`
	PropStat []propStat `xml:"propstat"`
}

type propStat struct {
	Status string `xml:"status"`
	Prop   prop   `xml:"prop"`
}

type prop struct {
	DisplayName   string  `xml:"displayname"`
	ContentLength string  `xml:"getcontentlength"`
	LastModified  string  `xml:"getlastmodified"`
	ETag          string  `xml:"getetag"`
	FileID        string  `xml:"fileid"`
	ID            string  `xml:"id"`
	Tags          *string `xml:"tags"`
	Favorite      *string `xml:"favorite"`
	Checksums     struct {
		Value string `xml:"checksum"`
	} `xml:"checksums"`
	ResourceType struct {
		Collection *struct{} `xml:"collection"`
	} `xml:"resourcetype"`
}

// DecodeStat decodes metadata for one PROPFIND response.
func DecodeStat(data []byte, remote string) (Item, error) {
	responses, err := decode(data)
	if err != nil {
		return Item{}, err
	}
	if len(responses) == 0 {
		return Item{}, errors.New("WebDAV response contained no resource")
	}
	return mapResponse(responses[0], cleanRemote(remote))
}

// DecodeList decodes a depth-one PROPFIND response.
func DecodeList(data []byte, remote string) ([]Item, error) {
	responses, err := decode(data)
	if err != nil {
		return nil, err
	}
	items := make([]Item, 0, len(responses))
	for index, response := range responses {
		if index == 0 {
			continue
		}
		name, err := resourceName(response.Href)
		if err != nil {
			return nil, err
		}
		item, err := mapResponse(response, path.Join(cleanRemote(remote), name))
		if err != nil {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

func decode(data []byte) ([]response, error) {
	var result multiStatus
	if err := xml.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse WebDAV response: %w", err)
	}
	return result.Responses, nil
}

func mapResponse(response response, remote string) (Item, error) {
	value, ok := successfulProp(response.PropStat)
	if !ok {
		return Item{}, errors.New("resource has no successful WebDAV properties")
	}
	name, err := resourceName(response.Href)
	if err != nil {
		return Item{}, err
	}
	if value.DisplayName != "" {
		name = value.DisplayName
	}
	kind := "file"
	if value.ResourceType.Collection != nil {
		kind = "directory"
	}
	size, _ := strconv.ParseInt(value.ContentLength, 10, 64)
	resourceID := value.FileID
	if resourceID == "" {
		resourceID = value.ID
	}
	var favorite *bool
	if value.Favorite != nil {
		selected := strings.TrimSpace(*value.Favorite) == "1"
		favorite = &selected
	}
	return Item{
		Name: name, Path: remote, Type: kind, ResourceID: resourceID, Size: size,
		LastModified: value.LastModified, ETag: value.ETag,
		Tags: parseTags(value.Tags), Favorite: favorite,
		Checksums: parseChecksums(value.Checksums.Value),
	}, nil
}

func parseTags(value *string) []string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	values := strings.Split(*value, ",")
	result := make([]string, 0, len(values))
	for _, tag := range values {
		if tag = strings.TrimSpace(tag); tag != "" {
			result = append(result, tag)
		}
	}
	return result
}

func parseChecksums(value string) []Checksum {
	fields := strings.Fields(value)
	result := make([]Checksum, 0, len(fields))
	for _, field := range fields {
		algorithm, checksum, ok := strings.Cut(field, ":")
		if !ok || algorithm == "" || checksum == "" {
			continue
		}
		result = append(result, Checksum{
			Algorithm: strings.ToUpper(algorithm),
			Value:     checksum,
		})
	}
	return result
}

func successfulProp(stats []propStat) (prop, bool) {
	for _, value := range stats {
		if strings.Contains(value.Status, " 200 ") {
			return value.Prop, true
		}
	}
	return prop{}, false
}

func resourceName(href string) (string, error) {
	name, err := url.PathUnescape(path.Base(strings.TrimSuffix(href, "/")))
	if err != nil {
		return "", fmt.Errorf("decode WebDAV resource name: %w", err)
	}
	return name, nil
}

func cleanRemote(remote string) string {
	return "/" + strings.TrimPrefix(path.Clean("/"+remote), "/")
}
