package trash

import (
	"encoding/xml"
	"fmt"
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
	ContentLength    string `xml:"getcontentlength"`
	Size             string `xml:"size"`
	OriginalName     string `xml:"trashbin-original-filename"`
	OriginalLocation string `xml:"trashbin-original-location"`
	DeletedAt        string `xml:"trashbin-delete-datetime"`
	DeletedTimestamp string `xml:"trashbin-delete-timestamp"`
	SpaceID          string `xml:"spaceid"`
	ResourceType     struct {
		Collection *struct{} `xml:"collection"`
	} `xml:"resourcetype"`
}

// DecodeList decodes a trash-bin PROPFIND response.
func DecodeList(data []byte, rootResource string) ([]Item, error) {
	var result multiStatus
	if err := xml.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse trash WebDAV response: %w", err)
	}
	items := make([]Item, 0, len(result.Responses))
	for _, response := range result.Responses {
		properties, ok := successfulProperties(response.PropStats)
		if !ok || properties.OriginalLocation == "" {
			continue
		}
		itemID, err := itemIDFromHref(response.Href, rootResource)
		if err != nil {
			return nil, err
		}
		item, err := mapItem(itemID, properties)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func successfulProperties(stats []propStat) (properties, bool) {
	for _, value := range stats {
		if strings.Contains(value.Status, " 200 ") {
			return value.Prop, true
		}
	}
	return properties{}, false
}

func itemIDFromHref(href string, rootResource string) (string, error) {
	parsed, err := url.Parse(href)
	if err != nil {
		return "", fmt.Errorf("parse trash item href: %w", err)
	}
	decoded, err := url.PathUnescape(parsed.EscapedPath())
	if err != nil {
		return "", fmt.Errorf("decode trash item href: %w", err)
	}
	root, err := url.PathUnescape(rootResource)
	if err != nil {
		return "", fmt.Errorf("decode trash root: %w", err)
	}
	prefix := strings.TrimRight(root, "/") + "/"
	if !strings.HasPrefix(decoded, prefix) {
		return "", fmt.Errorf(
			"trash item href %q is outside expected root %q", href, rootResource,
		)
	}
	return strings.TrimSuffix(strings.TrimPrefix(decoded, prefix), "/"), nil
}

func mapItem(itemID string, value properties) (Item, error) {
	sizeValue := value.ContentLength
	if sizeValue == "" {
		sizeValue = value.Size
	}
	size, err := parseOptionalInt(sizeValue, "trash item size")
	if err != nil {
		return Item{}, err
	}
	deletedTimestamp, err := parseOptionalInt(
		value.DeletedTimestamp, "trash deletion timestamp",
	)
	if err != nil {
		return Item{}, err
	}
	itemType := "file"
	if value.ResourceType.Collection != nil {
		itemType = "directory"
	}
	return Item{
		ID: itemID, OriginalName: value.OriginalName,
		OriginalPath: "/" + strings.TrimPrefix(value.OriginalLocation, "/"),
		Type:         itemType, Size: size, DeletedAt: value.DeletedAt,
		DeletedTimestamp: deletedTimestamp, SpaceID: value.SpaceID,
	}, nil
}

func parseOptionalInt(value string, label string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	result, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s %q: %w", label, value, err)
	}
	return result, nil
}
