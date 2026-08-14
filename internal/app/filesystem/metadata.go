package filesystem

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
	"github.com/mzner/ocis-cli/internal/webdav"
)

const ownCloudNamespace = "http://owncloud.org/ns"

var customPropertyName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9._-]*$`)

type tagResult struct {
	Path       string   `json:"path"`
	ResourceID string   `json:"resourceId"`
	Tags       []string `json:"tags"`
	Operation  string   `json:"operation,omitempty"`
	DryRun     bool     `json:"dryRun,omitempty"`
}

type favoriteResult struct {
	Path      string `json:"path"`
	Favorite  bool   `json:"favorite"`
	Operation string `json:"operation,omitempty"`
	DryRun    bool   `json:"dryRun,omitempty"`
}

type propertyResult struct {
	Path      string `json:"path"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Value     string `json:"value,omitempty"`
	Operation string `json:"operation,omitempty"`
	DryRun    bool   `json:"dryRun,omitempty"`
}

func RunMetadata(
	ctx context.Context,
	request MetadataRequest,
	selected string,
	options Options,
) error {
	if err := validateMetadataRequest(request); err != nil {
		return apperror.Wrap(apperror.KindUsage, "metadata", err)
	}
	client, err := options.NewClient(ctx, selected)
	if err != nil {
		return err
	}
	if err := client.SelectSpace(options.Space); err != nil {
		return err
	}

	switch request.Operation {
	case TagList:
		return listResourceTags(client, request.Path, options)
	case TagAdd, TagRemove:
		return mutateResourceTags(ctx, client, request, options)
	case FavoriteSet, FavoriteUnset:
		return mutateFavorite(client, request, options)
	case PropertyGet:
		return getCustomProperty(client, request, options)
	case PropertySet, PropertyRemove:
		return mutateCustomProperty(client, request, options)
	default:
		return apperror.Wrap(
			apperror.KindUsage, "metadata",
			fmt.Errorf("unknown metadata operation %q", request.Operation),
		)
	}
}

func validateMetadataRequest(request MetadataRequest) error {
	if strings.TrimSpace(request.Path) == "" {
		return errors.New("remote path is required")
	}
	switch request.Operation {
	case TagList, FavoriteSet, FavoriteUnset:
		return nil
	case TagAdd, TagRemove:
		if len(normalizeTags(request.Tags)) == 0 {
			return errors.New("at least one non-empty tag is required")
		}
		return nil
	case PropertyGet, PropertySet, PropertyRemove:
		return validateCustomProperty(request.Namespace, request.Name)
	default:
		return fmt.Errorf("unknown metadata operation %q", request.Operation)
	}
}

func validateCustomProperty(namespace, name string) error {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return errors.New("property namespace is required")
	}
	parsed, err := url.Parse(namespace)
	if err != nil || parsed.Scheme == "" {
		return fmt.Errorf("property namespace must be an absolute URI: %q", namespace)
	}
	if strings.EqualFold(namespace, "DAV:") ||
		strings.TrimRight(namespace, "/") == ownCloudNamespace {
		return fmt.Errorf(
			"namespace %q is reserved; use the dedicated metadata commands",
			namespace,
		)
	}
	if !customPropertyName.MatchString(name) {
		return fmt.Errorf(
			"property name %q must start with a letter or underscore and "+
				"contain only letters, digits, dot, underscore, or hyphen",
			name,
		)
	}
	return nil
}

func normalizeTags(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		for tag := range strings.SplitSeq(value, ",") {
			tag = strings.TrimSpace(tag)
			if tag == "" {
				continue
			}
			if _, ok := seen[tag]; ok {
				continue
			}
			seen[tag] = struct{}{}
			result = append(result, tag)
		}
	}
	return result
}

func listResourceTags(
	client Client, remote string, options Options,
) error {
	item, err := client.Stat(remote)
	if err != nil {
		return err
	}
	result := tagResult{
		Path: item.Path, ResourceID: item.ResourceID,
		Tags: slices.Clone(item.Tags),
	}
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "tags", result)
	}
	if len(result.Tags) == 0 {
		_, err = fmt.Fprintf(options.Out, "No tags on %s\n", item.Path)
		return err
	}
	for _, tag := range result.Tags {
		if _, err := fmt.Fprintln(options.Out, tag); err != nil {
			return err
		}
	}
	return nil
}

func mutateResourceTags(
	ctx context.Context,
	client Client,
	request MetadataRequest,
	options Options,
) error {
	item, err := client.Stat(request.Path)
	if err != nil {
		return err
	}
	if item.ResourceID == "" {
		return errors.New(
			"server did not return a stable resource ID; tag management is unsupported",
		)
	}
	tags := normalizeTags(request.Tags)
	operation := "add"
	if request.Operation == TagRemove {
		operation = "remove"
	}
	if request.DryRun {
		return output(
			options, "tags",
			tagResult{
				Path: item.Path, ResourceID: item.ResourceID, Tags: tags,
				Operation: operation, DryRun: true,
			},
			"Would %s tags %s on %s\n",
			operation, strings.Join(tags, ", "), item.Path,
		)
	}
	if request.Operation == TagAdd {
		err = client.AddTags(ctx, item.ResourceID, tags)
	} else {
		err = client.RemoveTags(ctx, item.ResourceID, tags)
	}
	if err != nil {
		if status := protocolStatus(err); status == http.StatusNotFound ||
			status == http.StatusMethodNotAllowed {
			return fmt.Errorf(
				"server does not expose LibreGraph tag management: %w", err,
			)
		}
		return err
	}
	updated, err := client.Stat(request.Path)
	if err != nil {
		return fmt.Errorf("tags changed but refreshed metadata failed: %w", err)
	}
	return output(
		options, "tags",
		tagResult{
			Path: updated.Path, ResourceID: updated.ResourceID,
			Tags: updated.Tags, Operation: operation,
		},
		"Tags on %s: %s\n", updated.Path, strings.Join(updated.Tags, ", "),
	)
}

func mutateFavorite(
	client Client, request MetadataRequest, options Options,
) error {
	item, err := client.Stat(request.Path)
	if err != nil {
		return err
	}
	selected := request.Operation == FavoriteSet
	operation := "set"
	if !selected {
		operation = "unset"
	}
	if err := requirePropertyWrites(client); err != nil {
		return err
	}
	if request.DryRun {
		return output(
			options, "favorite",
			favoriteResult{
				Path: item.Path, Favorite: selected,
				Operation: operation, DryRun: true,
			},
			"Would %s favorite on %s\n", operation, item.Path,
		)
	}
	if selected {
		err = client.SetProperty(
			request.Path,
			webdav.PropertyName{
				Namespace: ownCloudNamespace, Name: "favorite",
			},
			"1",
		)
	} else {
		err = client.RemoveProperty(
			request.Path,
			webdav.PropertyName{
				Namespace: ownCloudNamespace, Name: "favorite",
			},
		)
	}
	if err != nil {
		return err
	}
	return output(
		options, "favorite",
		favoriteResult{
			Path: item.Path, Favorite: selected, Operation: operation,
		},
		"Favorite on %s: %t\n", item.Path, selected,
	)
}

func getCustomProperty(
	client Client, request MetadataRequest, options Options,
) error {
	property := webdav.PropertyName{
		Namespace: strings.TrimSpace(request.Namespace), Name: request.Name,
	}
	value, err := client.GetProperty(request.Path, property)
	if err != nil {
		if errors.Is(err, webdav.ErrPropertyNotFound) {
			return fmt.Errorf(
				"property {%s}%s is unsupported or not set: %w",
				property.Namespace, property.Name, err,
			)
		}
		return err
	}
	result := propertyResult{
		Path: cleanRemote(request.Path), Namespace: value.Namespace,
		Name: value.Name, Value: value.Value,
	}
	return output(
		options, "property", result, "%s\n", result.Value,
	)
}

func mutateCustomProperty(
	client Client, request MetadataRequest, options Options,
) error {
	item, err := client.Stat(request.Path)
	if err != nil {
		return err
	}
	property := webdav.PropertyName{
		Namespace: strings.TrimSpace(request.Namespace), Name: request.Name,
	}
	operation := "set"
	if request.Operation == PropertyRemove {
		operation = "remove"
	}
	result := propertyResult{
		Path: item.Path, Namespace: property.Namespace, Name: property.Name,
		Value: request.Value, Operation: operation, DryRun: request.DryRun,
	}
	if err := requirePropertyWrites(client); err != nil {
		return err
	}
	if request.DryRun {
		return output(
			options, "property", result,
			"Would %s property {%s}%s on %s\n",
			operation, property.Namespace, property.Name, item.Path,
		)
	}
	if request.Operation == PropertySet {
		err = client.SetProperty(request.Path, property, request.Value)
	} else {
		err = client.RemoveProperty(request.Path, property)
	}
	if err != nil {
		return err
	}
	return output(
		options, "property", result,
		"%s property {%s}%s on %s\n",
		strings.ToUpper(operation[:1])+operation[1:],
		property.Namespace, property.Name, item.Path,
	)
}

func requirePropertyWrites(client Client) error {
	capabilities, err := client.Capabilities()
	if err != nil {
		return fmt.Errorf("discover WebDAV property support: %w", err)
	}
	for _, method := range capabilities.Allow {
		if strings.EqualFold(method, "PROPPATCH") {
			return nil
		}
	}
	return errors.New(
		"server does not advertise PROPPATCH; WebDAV property updates are unsupported",
	)
}
