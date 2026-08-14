// Package share contains direct-share, received-share, and public-link
// application policy behind a narrow authenticated client port.
package share

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/mzner/ocis-cli/internal/apperror"
	"github.com/mzner/ocis-cli/internal/graph"
	"github.com/mzner/ocis-cli/internal/logging"
	appoutput "github.com/mzner/ocis-cli/internal/output"
	"github.com/mzner/ocis-cli/internal/sharing"
	"github.com/mzner/ocis-cli/internal/webdav"
)

// Operation identifies one sharing use case.
type Operation string

const (
	Create       Operation = "create"
	List         Operation = "list"
	Revoke       Operation = "revoke"
	LinkInfo     Operation = "link-info"
	LinkUpdate   Operation = "link-update"
	DirectAdd    Operation = "direct-add"
	FederatedAdd Operation = "federated-add"
	DirectUpdate Operation = "direct-update"
	Remove       Operation = "remove"
	Overview     Operation = "overview"
	Received     Operation = "received"
	Accept       Operation = "accept"
	Decline      Operation = "decline"
	Roles        Operation = "roles"
)

// Request describes one public-link or direct-sharing operation.
type Request struct {
	Operation        Operation
	Path             string
	ID               string
	Name             string
	Password         string
	UpdateName       bool
	UpdateExpiration bool
	UpdateAccess     bool
	UpdatePassword   bool
	RemovePassword   bool
	Expiration       string
	Permissions      int
	Recipient        string
	RecipientType    string
	RecipientIsID    bool
	Role             string
	Direction        string
	State            string
	LinksOnly        bool
	Confirmed        bool
	DryRun           bool
	Federated        bool
}

// OverviewItem is one stable outgoing or received share inventory row.
type OverviewItem struct {
	ShareID       string `json:"shareId"`
	Direction     string `json:"direction"`
	State         string `json:"state"`
	SpaceID       string `json:"spaceId,omitempty"`
	SpaceName     string `json:"spaceName"`
	Path          string `json:"path,omitempty"`
	Type          string `json:"type"`
	PartyID       string `json:"partyId,omitempty"`
	PartyName     string `json:"partyName"`
	Permissions   int    `json:"permissions"`
	Permission    string `json:"permission"`
	Expiration    string `json:"expiration,omitempty"`
	PublicLinkURL string `json:"publicLinkUrl,omitempty"`
}

// GraphClient is the LibreGraph functionality used by sharing use cases.
// Keeping this port small makes additions to the protocol client independent
// from the application-domain boundary.
type GraphClient interface {
	ListMyDrives(context.Context) ([]graph.Drive, error)
	SearchUsers(context.Context, string) ([]graph.DirectoryUser, error)
	SearchFederatedUsers(context.Context, string) ([]graph.DirectoryUser, error)
	SearchGroups(context.Context, string) ([]graph.DirectoryGroup, error)
	ListItemPermissions(context.Context, string) (graph.Permissions, error)
	ListFederatedItemPermissions(context.Context, string) (graph.Permissions, error)
	InviteItem(
		context.Context, string, graph.InviteRequest,
	) (graph.Permission, error)
	UpdateItemPermission(
		context.Context, string, string, graph.PermissionUpdateRequest,
	) (graph.Permission, error)
	GetItemPermission(
		context.Context, string, string,
	) (graph.Permission, error)
	UpdateLinkPermission(
		context.Context, string, string, graph.LinkPermissionUpdateRequest,
	) (graph.Permission, error)
	SetItemPermissionPassword(
		context.Context, string, string, string,
	) (graph.Permission, error)
	RemoveItemPermission(context.Context, string, string) error
}

// SharingClient is the OCS sharing functionality used by sharing use cases.
type SharingClient interface {
	CreateLink(context.Context, sharing.CreateRequest) (sharing.Link, error)
	ListLinks(context.Context, sharing.ListRequest) ([]sharing.Link, error)
	GetLink(context.Context, string) (sharing.Link, error)
	RevokeLink(context.Context, string) error
	Capabilities(context.Context) (sharing.Capabilities, error)
	ListShares(
		context.Context, sharing.ShareListRequest,
	) ([]sharing.Share, error)
	AcceptShare(context.Context, string) error
	DeclineShare(context.Context, string) error
}

// Client is the authenticated server functionality used by this domain.
type Client interface {
	SelectSpace(string) error
	SelectedSpaceID() string
	Stat(string) (webdav.Item, error)
	Graph() GraphClient
	Sharing() SharingClient
}

// ClientFactory creates an account-bound client without exposing parent app
// runtime internals.
type ClientFactory func(context.Context, string) (Client, error)

// Options contains the process-boundary values used by share operations.
type Options struct {
	OutputMode appoutput.Mode
	Out        io.Writer
	Space      string
	Logger     logging.Logger
	NewClient  ClientFactory
}

func output(
	options Options, kind string, value any, format string, args ...any,
) error {
	return (appoutput.Renderer{
		Writer: options.Out, Mode: options.OutputMode, Type: kind,
	}).Write(value, format, args...)
}

func writeOutput(options Options, kind string, value any) error {
	return (appoutput.Renderer{
		Writer: options.Out, Mode: options.OutputMode, Type: kind,
	}).Write(value, "")
}

func cleanRemote(remote string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" || remote == "/" {
		return "/"
	}
	return "/" + strings.Trim(remote, "/")
}

func fallback(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func resolveSpace(
	spaces []graph.Drive, identifier string,
) (graph.Drive, error) {
	var matches []graph.Drive
	for _, value := range spaces {
		if value.ID == identifier || strings.EqualFold(value.Name, identifier) ||
			strings.EqualFold(value.DriveAlias, identifier) {
			matches = append(matches, value)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return graph.Drive{}, apperror.Wrap(
			apperror.KindUsage, "space",
			fmt.Errorf("unknown space %q; run ocis space list", identifier),
		)
	default:
		return graph.Drive{}, apperror.Wrap(
			apperror.KindUsage, "space",
			fmt.Errorf("space name %q is ambiguous; use its ID", identifier),
		)
	}
}
