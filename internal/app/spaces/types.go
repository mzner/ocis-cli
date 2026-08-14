// Package spaces owns project Space lifecycle, details, and membership policy.
package spaces

import (
	"context"
	"io"

	"github.com/mzner/ocis-cli/internal/graph"
	"github.com/mzner/ocis-cli/internal/logging"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

type CreateRequest struct {
	Name, Description string
	Quota             *int64
	DryRun            bool
}
type UpdateRequest struct {
	Identifier               string
	Name, Description, Alias *string
	Quota                    *int64
	DryRun                   bool
}
type LifecycleOperation string

const (
	Disable LifecycleOperation = "disable"
	Restore LifecycleOperation = "restore"
	Delete  LifecycleOperation = "delete"
)

type LifecycleRequest struct {
	Operation         LifecycleOperation
	Identifier        string
	Permanent, DryRun bool
}
type MemberOperation string

const (
	MemberList   MemberOperation = "list"
	MemberAdd    MemberOperation = "add"
	MemberUpdate MemberOperation = "update"
	MemberRemove MemberOperation = "remove"
)

type MemberRequest struct {
	Operation                        MemberOperation
	Space, PermissionID, RecipientID string
	RecipientIsID                    bool
	RecipientType, Role              string
	DryRun                           bool
}

type GraphClient interface {
	ListDrives(context.Context) ([]graph.Drive, error)
	CreateDrive(context.Context, graph.CreateDriveRequest) (graph.Drive, error)
	UpdateDrive(context.Context, string, graph.UpdateDriveRequest) (graph.Drive, error)
	DeleteDrive(context.Context, string, bool) error
	RestoreDrive(context.Context, string) (graph.Drive, error)
	ListSpacePermissions(context.Context, string) (graph.Permissions, error)
	GetMe(context.Context) (graph.Me, error)
	SearchUsers(context.Context, string) ([]graph.DirectoryUser, error)
	SearchGroups(context.Context, string) ([]graph.DirectoryGroup, error)
	AddSpaceMember(context.Context, string, graph.InviteRequest) (graph.Permission, error)
	UpdateSpaceMember(context.Context, string, string, graph.PermissionUpdateRequest) (graph.Permission, error)
	RemoveSpaceMember(context.Context, string, string) error
}
type Client interface {
	Graph() GraphClient
	ClearDefaultSpace(string) error
}
type ClientFactory func(context.Context, string) (Client, error)
type Options struct {
	OutputMode appoutput.Mode
	Out        io.Writer
	Logger     logging.Logger
	NewClient  ClientFactory
}
