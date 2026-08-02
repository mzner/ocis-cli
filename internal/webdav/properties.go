package webdav

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
)

// ErrPropertyNotFound indicates that the server did not return a successful
// value for the requested WebDAV property.
var ErrPropertyNotFound = errors.New("WebDAV property is unsupported or not set")

// PropertyName identifies one namespaced WebDAV property.
type PropertyName struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// PropertyValue contains one scalar WebDAV property value.
type PropertyValue struct {
	PropertyName
	Value string `json:"value"`
}

// GetProperty reads one explicitly named scalar property.
func (client *Client) GetProperty(
	ctx context.Context, remote string, property PropertyName,
) (PropertyValue, error) {
	body, err := propertyFindXML(property)
	if err != nil {
		return PropertyValue{}, err
	}
	data, err := client.request(
		ctx, "PROPFIND", remote, bytes.NewReader(body), "0",
	)
	if err != nil {
		return PropertyValue{}, err
	}
	value, err := decodeProperty(data, property)
	if err != nil {
		return PropertyValue{}, err
	}
	return PropertyValue{PropertyName: property, Value: value}, nil
}

// SetProperty sets one scalar WebDAV property.
func (client *Client) SetProperty(
	ctx context.Context, remote string, property PropertyName, value string,
) error {
	body, err := propertyUpdateXML(property, value, false)
	if err != nil {
		return err
	}
	data, err := client.request(
		ctx, "PROPPATCH", remote, bytes.NewReader(body), "",
	)
	if err != nil {
		return err
	}
	return decodePropertyUpdate(data, property, false)
}

// RemoveProperty removes one WebDAV property.
func (client *Client) RemoveProperty(
	ctx context.Context, remote string, property PropertyName,
) error {
	body, err := propertyUpdateXML(property, "", true)
	if err != nil {
		return err
	}
	data, err := client.request(
		ctx, "PROPPATCH", remote, bytes.NewReader(body), "",
	)
	if err != nil {
		return err
	}
	return decodePropertyUpdate(data, property, true)
}

func propertyFindXML(property PropertyName) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := xml.NewEncoder(&buffer)
	propfind := xml.StartElement{Name: xml.Name{Space: "DAV:", Local: "propfind"}}
	prop := xml.StartElement{Name: xml.Name{Space: "DAV:", Local: "prop"}}
	target := xml.StartElement{Name: xml.Name{
		Space: property.Namespace, Local: property.Name,
	}}
	for _, token := range []xml.Token{
		propfind, prop, target, target.End(), prop.End(), propfind.End(),
	} {
		if err := encoder.EncodeToken(token); err != nil {
			return nil, fmt.Errorf("encode property request: %w", err)
		}
	}
	if err := encoder.Flush(); err != nil {
		return nil, fmt.Errorf("encode property request: %w", err)
	}
	return buffer.Bytes(), nil
}

func propertyUpdateXML(
	property PropertyName, value string, remove bool,
) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := xml.NewEncoder(&buffer)
	update := xml.StartElement{Name: xml.Name{
		Space: "DAV:", Local: "propertyupdate",
	}}
	operation := xml.StartElement{Name: xml.Name{
		Space: "DAV:", Local: "set",
	}}
	if remove {
		operation.Name.Local = "remove"
	}
	prop := xml.StartElement{Name: xml.Name{Space: "DAV:", Local: "prop"}}
	target := xml.StartElement{Name: xml.Name{
		Space: property.Namespace, Local: property.Name,
	}}
	for _, token := range []xml.Token{update, operation, prop, target} {
		if err := encoder.EncodeToken(token); err != nil {
			return nil, fmt.Errorf("encode property update: %w", err)
		}
	}
	if !remove {
		if err := encoder.EncodeToken(xml.CharData(value)); err != nil {
			return nil, fmt.Errorf("encode property value: %w", err)
		}
	}
	for _, token := range []xml.Token{
		target.End(), prop.End(), operation.End(), update.End(),
	} {
		if err := encoder.EncodeToken(token); err != nil {
			return nil, fmt.Errorf("encode property update: %w", err)
		}
	}
	if err := encoder.Flush(); err != nil {
		return nil, fmt.Errorf("encode property update: %w", err)
	}
	return buffer.Bytes(), nil
}

type propertyMultiStatus struct {
	Responses []propertyResponse `xml:"response"`
}

type propertyResponse struct {
	PropStats []propertyStatus `xml:"propstat"`
}

type propertyStatus struct {
	Status     string            `xml:"status"`
	Properties propertyContainer `xml:"prop"`
}

type propertyContainer struct {
	Values []rawProperty `xml:",any"`
}

type rawProperty struct {
	XMLName xml.Name
	Value   string `xml:",chardata"`
}

func decodeProperty(data []byte, property PropertyName) (string, error) {
	statuses, err := propertyStatuses(data)
	if err != nil {
		return "", err
	}
	for _, status := range statuses {
		for _, candidate := range status.Properties.Values {
			if candidate.XMLName.Space != property.Namespace ||
				candidate.XMLName.Local != property.Name {
				continue
			}
			if strings.Contains(status.Status, " 200 ") {
				return candidate.Value, nil
			}
			return "", fmt.Errorf(
				"%w: {%s}%s returned %s",
				ErrPropertyNotFound, property.Namespace, property.Name,
				strings.TrimSpace(status.Status),
			)
		}
	}
	return "", fmt.Errorf(
		"%w: {%s}%s", ErrPropertyNotFound,
		property.Namespace, property.Name,
	)
}

func decodePropertyUpdate(
	data []byte, property PropertyName, remove bool,
) error {
	statuses, err := propertyStatuses(data)
	if err != nil {
		return err
	}
	for _, status := range statuses {
		for _, candidate := range status.Properties.Values {
			if candidate.XMLName.Space != property.Namespace ||
				candidate.XMLName.Local != property.Name {
				continue
			}
			if strings.Contains(status.Status, " 200 ") ||
				remove && strings.Contains(status.Status, " 204 ") {
				return nil
			}
			return fmt.Errorf(
				"WebDAV property update failed: {%s}%s returned %s",
				property.Namespace, property.Name,
				strings.TrimSpace(status.Status),
			)
		}
	}
	return fmt.Errorf(
		"WebDAV property update returned no status for {%s}%s",
		property.Namespace, property.Name,
	)
}

func propertyStatuses(data []byte) ([]propertyStatus, error) {
	var response propertyMultiStatus
	if err := xml.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("parse WebDAV property response: %w", err)
	}
	if len(response.Responses) == 0 {
		return nil, errors.New("WebDAV property response contained no resource")
	}
	return response.Responses[0].PropStats, nil
}
