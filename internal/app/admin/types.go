// Package admin contains account and global Space administration policy.
package admin

import (
	"context"
	"io"

	"github.com/mzner/ocis-cli/internal/graph"
	"github.com/mzner/ocis-cli/internal/logging"
	appoutput "github.com/mzner/ocis-cli/internal/output"
	"github.com/mzner/ocis-cli/internal/sharing"
)

type Operation string

const (
	UserList        Operation = "user-list"
	UserInfo        Operation = "user-info"
	GroupList       Operation = "group-list"
	GroupInfo       Operation = "group-info"
	GroupMemberList Operation = "group-member-list"
	SpaceList       Operation = "space-list"
	SpaceInfo       Operation = "space-info"
)

type Request struct {
	Operation  Operation
	Identifier string
	Search     string
	RawSearch  string
}

type UserCreateRequest struct {
	Username, DisplayName, Mail, GivenName, Surname, Password string
	Disabled, DryRun                                          bool
}
type UserUpdateRequest struct {
	Identifier                                      string
	Username, DisplayName, Mail, GivenName, Surname *string
	Password                                        string
	SetPassword, DryRun                             bool
}
type UserStateRequest struct {
	Identifier      string
	Enabled, DryRun bool
}
type UserDeleteRequest struct {
	Identifier string
	DryRun     bool
}
type GroupCreateRequest struct {
	Name   string
	DryRun bool
}
type GroupUpdateRequest struct {
	Identifier, Name string
	DryRun           bool
}
type GroupDeleteRequest struct {
	Identifier string
	DryRun     bool
}
type GroupMemberMutationRequest struct {
	Group, User    string
	Remove, DryRun bool
}

type RoleOperation string

const (
	RoleList      RoleOperation = "list"
	RoleAvailable RoleOperation = "available"
	RoleGrant     RoleOperation = "grant"
	RoleRevoke    RoleOperation = "revoke"
)

type RoleRequest struct {
	Operation  RoleOperation
	User, Role string
	DryRun     bool
}

type GraphClient interface {
	CheckAdminMFA(context.Context) error
	ListUsers(context.Context, graph.DirectorySearch) ([]graph.DirectoryUser, error)
	GetUser(context.Context, string) (graph.DirectoryUser, error)
	ListGroups(context.Context, graph.DirectorySearch) ([]graph.DirectoryGroup, error)
	GetGroup(context.Context, string) (graph.DirectoryGroup, error)
	ListGroupMembers(context.Context, string) ([]graph.DirectoryUser, error)
	ListDrives(context.Context) ([]graph.Drive, error)
	ListSpacePermissions(context.Context, string) (graph.Permissions, error)
	GetMe(context.Context) (graph.Me, error)
	CreateUser(context.Context, graph.CreateUserRequest) (graph.DirectoryUser, error)
	UpdateUser(context.Context, string, graph.UpdateUserRequest) (graph.DirectoryUser, error)
	DeleteUser(context.Context, string) error
	CreateGroup(context.Context, graph.CreateGroupRequest) (graph.DirectoryGroup, error)
	UpdateGroup(context.Context, string, graph.UpdateGroupRequest) error
	DeleteGroup(context.Context, string) error
	AddGroupMember(context.Context, string, string) error
	RemoveGroupMember(context.Context, string, string) error
	ListApplications(context.Context) ([]graph.Application, error)
	ListAppRoleAssignments(context.Context, string) ([]graph.AppRoleAssignment, error)
	AssignAppRole(context.Context, graph.AppRoleAssignment) (graph.AppRoleAssignment, error)
	RemoveAppRoleAssignment(context.Context, string, string) error
}

type SharingClient interface {
	Capabilities(context.Context) (sharing.Capabilities, error)
}

type Client interface {
	Graph() GraphClient
	Sharing() SharingClient
	ProfileName() string
}

type ClientFactory func(context.Context, string) (Client, error)
type AccountAdminGuard func(context.Context, Client) error
type SpaceDetailsWriter func(context.Context, Client, graph.Drive, Options) error

type Options struct {
	OutputMode          appoutput.Mode
	Out                 io.Writer
	Space               string
	Logger              logging.Logger
	NewClient           ClientFactory
	RequireAccountAdmin AccountAdminGuard
	WriteSpaceDetails   SpaceDetailsWriter
}
