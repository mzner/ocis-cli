package versions

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type multiStatus struct {
	Responses []response `xml:"response"`
}

type response struct {
	Href      string     `xml:"href"`
	PropStats []propStat `xml:"propstat"`
}

type propStat struct {
	Status string     `xml:"status"`
	Prop   properties `xml:"prop"`
}

type properties struct {
	ContentLength string `xml:"getcontentlength"`
	LastModified  string `xml:"getlastmodified"`
	ETag          string `xml:"getetag"`
	ResourceType  struct {
		Collection *struct{} `xml:"collection"`
	} `xml:"resourcetype"`
}

// DecodeList decodes a file-version PROPFIND response.
func DecodeList(data []byte, rootResource string) ([]Version, error) {
	var result multiStatus
	if err := xml.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse versions WebDAV response: %w", err)
	}
	available := make([]Version, 0, len(result.Responses))
	for _, response := range result.Responses {
		value, ok := successfulProperties(response.PropStats)
		if !ok || value.ResourceType.Collection != nil {
			continue
		}
		versionID, err := versionIDFromHref(response.Href, rootResource)
		if err != nil {
			return nil, err
		}
		size, err := strconv.ParseInt(value.ContentLength, 10, 64)
		if err != nil {
			return nil, fmt.Errorf(
				"parse version size %q: %w", value.ContentLength, err,
			)
		}
		var modifiedTimestamp int64
		if modified, err := http.ParseTime(value.LastModified); err == nil {
			modifiedTimestamp = modified.Unix()
		}
		available = append(available, Version{
			ID: versionID, Modified: value.LastModified,
			ModifiedTimestamp: modifiedTimestamp, Size: size, ETag: value.ETag,
		})
	}
	return available, nil
}

func successfulProperties(stats []propStat) (properties, bool) {
	for _, value := range stats {
		if strings.Contains(value.Status, " 200 ") {
			return value.Prop, true
		}
	}
	return properties{}, false
}

func versionIDFromHref(href string, rootResource string) (string, error) {
	parsed, err := url.Parse(href)
	if err != nil {
		return "", fmt.Errorf("parse version href: %w", err)
	}
	decoded, err := url.PathUnescape(parsed.EscapedPath())
	if err != nil {
		return "", fmt.Errorf("decode version href: %w", err)
	}
	root, err := url.PathUnescape(rootResource)
	if err != nil {
		return "", fmt.Errorf("decode versions root: %w", err)
	}
	prefix := strings.TrimRight(root, "/") + "/"
	if !strings.HasPrefix(decoded, prefix) {
		return "", fmt.Errorf(
			"version href %q is outside expected root %q", href, rootResource,
		)
	}
	versionID := strings.Trim(strings.TrimPrefix(decoded, prefix), "/")
	if versionID == "" {
		return "", fmt.Errorf("version href %q has no version ID", href)
	}
	return versionID, nil
}
