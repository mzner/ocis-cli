package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"time"

	adminapp "github.com/mzner/ocis-cli/internal/app/admin"
	filesystemapp "github.com/mzner/ocis-cli/internal/app/filesystem"
	shareapp "github.com/mzner/ocis-cli/internal/app/share"
	spacesapp "github.com/mzner/ocis-cli/internal/app/spaces"
	syncapp "github.com/mzner/ocis-cli/internal/app/sync"
	"github.com/mzner/ocis-cli/internal/apperror"
	"github.com/mzner/ocis-cli/internal/graph"
	"github.com/mzner/ocis-cli/internal/logging"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

// Version is the user-visible CLI version. Release builds inject the tag.
var Version = "dev"

// ServerOperation identifies a server-profile use case.
type ServerOperation string

const (
	ServerAdd    ServerOperation = "add"
	ServerList   ServerOperation = "list"
	ServerUse    ServerOperation = "use"
	ServerRemove ServerOperation = "remove"
)

// ServerRequest describes one server-profile operation.
type ServerRequest struct {
	Operation ServerOperation
	Name      string
	Server    string
	ClientID  string
	Insecure  bool
}

// ConfigOperation identifies a local configuration-inspection use case.
type ConfigOperation string

const (
	ConfigPath  ConfigOperation = "path"
	ConfigPaths ConfigOperation = "paths"
	ConfigShow  ConfigOperation = "show"
)

// ConfigRequest describes one local configuration-inspection operation.
type ConfigRequest struct {
	Operation ConfigOperation
	Profile   string
}

// AuthOperation identifies an authentication use case.
type AuthOperation string

const (
	AuthSetup  AuthOperation = "setup"
	AuthLogin  AuthOperation = "login"
	AuthStatus AuthOperation = "status"
	AuthLogout AuthOperation = "logout"
)

// AuthRequest describes one authentication operation.
type AuthRequest struct {
	Operation AuthOperation
	Profile   string
	Server    string
	Name      string
	Mode      string
	Username  string
	ClientID  string
	ACR       string
	Insecure  bool
	NoBrowser bool
	MFA       bool
}

// SpaceOperation identifies a Spaces use case.
type SpaceOperation string

const (
	SpaceList    SpaceOperation = "list"
	SpaceInfo    SpaceOperation = "info"
	SpaceStat                   = SpaceInfo
	SpaceUse     SpaceOperation = "use"
	SpaceUnset   SpaceOperation = "unset"
	SpaceCurrent SpaceOperation = "current"
)

// SpaceRequest describes one Spaces operation.
type SpaceRequest struct {
	Operation  SpaceOperation
	Identifier string
}

// SpaceCreateRequest describes a project Space to create.
type SpaceCreateRequest struct {
	Name        string
	Description string
	Quota       *int64
	DryRun      bool
}

// SpaceUpdateRequest contains explicitly selected metadata changes.
type SpaceUpdateRequest struct {
	Identifier  string
	Name        *string
	Description *string
	Alias       *string
	Quota       *int64
	DryRun      bool
}

// SpaceLifecycleOperation identifies a Space lifecycle use case.
type SpaceLifecycleOperation string

const (
	SpaceDisable SpaceLifecycleOperation = "disable"
	SpaceRestore SpaceLifecycleOperation = "restore"
	SpaceDelete  SpaceLifecycleOperation = "delete"
)

// SpaceLifecycleRequest describes a disable, restore, or permanent deletion.
type SpaceLifecycleRequest struct {
	Operation  SpaceLifecycleOperation
	Identifier string
	Permanent  bool
	DryRun     bool
}

// SpaceMemberOperation identifies a Space membership use case.
type SpaceMemberOperation string

const (
	SpaceMemberList   SpaceMemberOperation = "list"
	SpaceMemberAdd    SpaceMemberOperation = "add"
	SpaceMemberUpdate SpaceMemberOperation = "update"
	SpaceMemberRemove SpaceMemberOperation = "remove"
)

// SpaceMemberRequest describes one membership operation.
type SpaceMemberRequest struct {
	Operation     SpaceMemberOperation
	Space         string
	PermissionID  string
	RecipientID   string
	RecipientIsID bool
	RecipientType string
	Role          string
	DryRun        bool
}

// ShareOperation identifies a sharing use case.
type ShareOperation = shareapp.Operation

const (
	ShareCreate       = shareapp.Create
	ShareList         = shareapp.List
	ShareRevoke       = shareapp.Revoke
	ShareLinkInfo     = shareapp.LinkInfo
	ShareLinkUpdate   = shareapp.LinkUpdate
	ShareDirectAdd    = shareapp.DirectAdd
	ShareFederatedAdd = shareapp.FederatedAdd
	ShareDirectUpdate = shareapp.DirectUpdate
	ShareRemove       = shareapp.Remove
	ShareOverview     = shareapp.Overview
	ShareReceived     = shareapp.Received
	ShareAccept       = shareapp.Accept
	ShareDecline      = shareapp.Decline
	ShareRoles        = shareapp.Roles
)

// ShareRequest describes one public-link or direct-sharing operation.
type ShareRequest = shareapp.Request

// ShareOverviewItem is one stable outgoing or received share inventory row.
type ShareOverviewItem = shareapp.OverviewItem

// TrashOperation identifies a recycle-bin use case.
type TrashOperation string

const (
	TrashList    TrashOperation = "list"
	TrashRestore TrashOperation = "restore"
	TrashRemove  TrashOperation = "remove"
	TrashEmpty   TrashOperation = "empty"
)

// TrashRequest describes one recycle-bin operation.
type TrashRequest struct {
	Operation TrashOperation
	ItemID    string
	Overwrite bool
	Permanent bool
	DryRun    bool
}

// VersionOperation identifies a historical file-version use case.
type VersionOperation string

const (
	VersionList     VersionOperation = "list"
	VersionInfo     VersionOperation = "info"
	VersionDownload VersionOperation = "download"
	VersionRestore  VersionOperation = "restore"
)

// VersionRequest describes one historical file-version operation.
type VersionRequest struct {
	Operation   VersionOperation
	Path        string
	VersionID   string
	Destination string
	NoClobber   bool
	Verify      bool
	Confirmed   bool
	DryRun      bool
}

// FilesystemOperation identifies a remote-filesystem use case.
type FilesystemOperation string

const (
	FilesystemList     FilesystemOperation = "list"
	FilesystemStat     FilesystemOperation = "stat"
	FilesystemCat      FilesystemOperation = "cat"
	FilesystemTree     FilesystemOperation = "tree"
	FilesystemDU       FilesystemOperation = "du"
	FilesystemUpload   FilesystemOperation = "upload"
	FilesystemDownload FilesystemOperation = "download"
	FilesystemMkdir    FilesystemOperation = "mkdir"
	FilesystemTouch    FilesystemOperation = "touch"
	FilesystemMove     FilesystemOperation = "mv"
	FilesystemCopy     FilesystemOperation = "cp"
	FilesystemRemove   FilesystemOperation = "remove"
)

// FilesystemRequest describes one remote-filesystem operation.
type FilesystemRequest struct {
	Operation   FilesystemOperation
	Source      string
	Destination string
	Recursive   bool
	Overwrite   bool
	NoClobber   bool
	DryRun      bool
	Verify      bool
	Parents     bool
	MaxDepth    int
	MaxEntries  int
}

// FilesystemTreeEntry describes one resource in a bounded remote tree.
type FilesystemTreeEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Type  string `json:"type"`
	Size  int64  `json:"size,omitempty"`
	Depth int    `json:"depth"`
}

// FilesystemUsage summarizes logical WebDAV content below one remote path.
// It does not represent physical or quota usage reported by the server.
type FilesystemUsage struct {
	Path         string `json:"path"`
	LogicalBytes int64  `json:"logicalBytes"`
	Files        int    `json:"files"`
	Directories  int    `json:"directories"`
	Entries      int    `json:"entries"`
	MaxDepth     int    `json:"maxDepth"`
	MaxEntries   int    `json:"maxEntries"`
	Complete     bool   `json:"complete"`
}

// BatchRequest describes sequential, non-atomic file operations read from a
// JSONL stream.
type BatchRequest struct {
	Input           io.Reader
	DryRun          bool
	Confirmed       bool
	ContinueOnError bool
	MaxOperations   int
}

// BatchOperation is one validated JSONL batch record.
type BatchOperation struct {
	Operation   string `json:"operation"`
	Path        string `json:"path,omitempty"`
	Source      string `json:"source,omitempty"`
	Destination string `json:"destination,omitempty"`
	Recursive   bool   `json:"recursive,omitempty"`
	Overwrite   bool   `json:"overwrite,omitempty"`
	NoClobber   bool   `json:"noClobber,omitempty"`
	Parents     bool   `json:"parents,omitempty"`
	Verify      *bool  `json:"verify,omitempty"`
}

// BatchOperationError is the stable per-operation failure representation.
type BatchOperationError struct {
	Code    int    `json:"code"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

// BatchResult reports the outcome of one input record.
type BatchResult struct {
	Index       int                  `json:"index"`
	Line        int                  `json:"line"`
	Operation   string               `json:"operation"`
	Path        string               `json:"path,omitempty"`
	Source      string               `json:"source,omitempty"`
	Destination string               `json:"destination,omitempty"`
	Parents     bool                 `json:"parents,omitempty"`
	Status      string               `json:"status"`
	Error       *BatchOperationError `json:"error,omitempty"`
}

// BatchSummary reports the complete sequential batch outcome.
type BatchSummary struct {
	Total     int           `json:"total"`
	Succeeded int           `json:"succeeded"`
	Failed    int           `json:"failed"`
	Planned   int           `json:"planned"`
	Skipped   int           `json:"skipped"`
	Stopped   bool          `json:"stopped"`
	DryRun    bool          `json:"dryRun"`
	Results   []BatchResult `json:"results"`
}

// SyncDirection identifies a synchronization policy.
type SyncDirection = syncapp.Direction

const (
	// SyncPush copies the local source tree into the remote destination.
	SyncPush = syncapp.Push
	// SyncPull copies the remote source tree into the local destination.
	SyncPull = syncapp.Pull
	// SyncBidirectional reconciles local and remote changes through a baseline.
	SyncBidirectional = syncapp.Bidirectional
)

// SyncRequest describes one deterministic directory reconciliation.
type SyncRequest = syncapp.Request

// SyncStateOperation identifies a local synchronization-state operation.
type SyncStateOperation = syncapp.StateOperation

const (
	SyncStateList   = syncapp.StateList
	SyncStateShow   = syncapp.StateShow
	SyncStateExport = syncapp.StateExport
	SyncStateRemove = syncapp.StateRemove
)

// SyncStateRequest describes inspection, export, or removal of a saved
// synchronization baseline.
type SyncStateRequest = syncapp.StateRequest

// SyncRecoveryOperation identifies an interrupted-run journal operation.
type SyncRecoveryOperation = syncapp.RecoveryOperation

const (
	SyncRecoveryList   = syncapp.RecoveryList
	SyncRecoveryShow   = syncapp.RecoveryShow
	SyncRecoveryRetry  = syncapp.RecoveryRetry
	SyncRecoveryRemove = syncapp.RecoveryRemove
)

// SyncRecoveryRequest describes inspection, safe retry, or removal of a
// bidirectional synchronization recovery journal.
type SyncRecoveryRequest = syncapp.RecoveryRequest

// SyncJobOperation identifies a reusable synchronization-job operation.
type SyncJobOperation = syncapp.JobOperation

const (
	SyncJobAdd    = syncapp.JobAdd
	SyncJobList   = syncapp.JobList
	SyncJobShow   = syncapp.JobShow
	SyncJobRun    = syncapp.JobRun
	SyncJobRemove = syncapp.JobRemove
)

// SyncJobRequest describes creation, inspection, execution, or removal of a
// named synchronization configuration.
type SyncJobRequest = syncapp.JobRequest

// MetadataOperation identifies a file-metadata use case.
type MetadataOperation string

const (
	MetadataTagList        MetadataOperation = "tag-list"
	MetadataTagAdd         MetadataOperation = "tag-add"
	MetadataTagRemove      MetadataOperation = "tag-remove"
	MetadataFavoriteSet    MetadataOperation = "favorite-set"
	MetadataFavoriteUnset  MetadataOperation = "favorite-unset"
	MetadataPropertyGet    MetadataOperation = "property-get"
	MetadataPropertySet    MetadataOperation = "property-set"
	MetadataPropertyRemove MetadataOperation = "property-remove"
)

// MetadataRequest describes one tag, favorite, or custom-property operation.
type MetadataRequest struct {
	Operation MetadataOperation
	Path      string
	Tags      []string
	Namespace string
	Name      string
	Value     string
	DryRun    bool
}

// AdminOperation identifies a read-only administrative use case.
type AdminOperation = adminapp.Operation

const (
	AdminUserList        = adminapp.UserList
	AdminUserInfo        = adminapp.UserInfo
	AdminGroupList       = adminapp.GroupList
	AdminGroupInfo       = adminapp.GroupInfo
	AdminGroupMemberList = adminapp.GroupMemberList
	AdminSpaceList       = adminapp.SpaceList
	AdminSpaceInfo       = adminapp.SpaceInfo
)

// AdminRequest describes one read-only administrative operation.
type AdminRequest = adminapp.Request

// AdminUserCreateRequest describes a new server user.
type AdminUserCreateRequest = adminapp.UserCreateRequest

// AdminUserUpdateRequest contains explicitly selected user changes.
type AdminUserUpdateRequest = adminapp.UserUpdateRequest

// AdminUserStateRequest enables or disables one user account.
type AdminUserStateRequest = adminapp.UserStateRequest

// AdminUserDeleteRequest permanently deletes one user account.
type AdminUserDeleteRequest = adminapp.UserDeleteRequest

// AdminGroupCreateRequest describes a new server group.
type AdminGroupCreateRequest = adminapp.GroupCreateRequest

// AdminGroupUpdateRequest renames one server group.
type AdminGroupUpdateRequest = adminapp.GroupUpdateRequest

// AdminGroupDeleteRequest permanently deletes one server group.
type AdminGroupDeleteRequest = adminapp.GroupDeleteRequest

// AdminGroupMemberMutationRequest adds or removes a direct user member.
type AdminGroupMemberMutationRequest = adminapp.GroupMemberMutationRequest

// AdminRoleOperation identifies one user-role operation.
type AdminRoleOperation = adminapp.RoleOperation

const (
	AdminRoleList      = adminapp.RoleList
	AdminRoleAvailable = adminapp.RoleAvailable
	AdminRoleGrant     = adminapp.RoleGrant
	AdminRoleRevoke    = adminapp.RoleRevoke
)

// AdminRoleRequest lists, grants, or revokes a server-advertised user role.
type AdminRoleRequest = adminapp.RoleRequest

// SearchRequest describes a read-only remote resource search.
type SearchRequest struct {
	Query          string
	Raw            bool
	Content        bool
	AllSpaces      bool
	Path           string
	ResourceType   string
	MediaType      string
	MinSize        *int64
	MaxSize        *int64
	ModifiedAfter  *time.Time
	ModifiedBefore *time.Time
	Limit          int
}

// RunOptions contains process-boundary dependencies and reliability policy.
type RunOptions struct {
	OutputMode   appoutput.Mode
	In           io.Reader
	Out          io.Writer
	Err          io.Writer
	Timeout      time.Duration
	Retries      int
	Concurrency  int
	Quiet        bool
	Space        string
	Logger       logging.Logger
	Dependencies Dependencies
}

func (options RunOptions) normalized() RunOptions {
	if options.OutputMode == "" {
		options.OutputMode = appoutput.Human
	}
	if options.Out == nil {
		options.Out = os.Stdout
	}
	if options.In == nil {
		options.In = os.Stdin
	}
	if options.Err == nil {
		options.Err = os.Stderr
	}
	if options.Timeout <= 0 {
		options.Timeout = 5 * time.Minute
	}
	if options.Retries < 0 {
		options.Retries = 0
	}
	if options.Concurrency <= 0 {
		options.Concurrency = 4
	}
	if options.Logger == nil {
		options.Logger = logging.Nop()
	}
	options.Dependencies = options.Dependencies.normalized()
	return options
}

// RunServerWithOptions executes a server-profile use case with explicit I/O.
func RunServerWithOptions(ctx context.Context, request ServerRequest, options RunOptions) error {
	return runServer(ctx, request, options.normalized())
}

// RunConfigWithOptions inspects effective local, non-secret configuration.
func RunConfigWithOptions(
	ctx context.Context,
	request ConfigRequest,
	options RunOptions,
) error {
	return runConfig(ctx, request, options.normalized())
}

// RunAuthWithOptions executes an authentication use case with explicit I/O.
func RunAuthWithOptions(ctx context.Context, request AuthRequest, selectedProfile string, options RunOptions) error {
	return classifyProtocolError("authenticate", runAuth(ctx, request, selectedProfile, options.normalized()))
}

// RunFilesystemWithOptions executes a remote-filesystem use case with explicit
// I/O and reliability policy.
func RunFilesystemWithOptions(ctx context.Context, request FilesystemRequest, selectedProfile string, options RunOptions) error {
	return classifyProtocolError(
		string(request.Operation),
		filesystemapp.Run(ctx, toFilesystemRequest(request), selectedProfile, filesystemOptions(options.normalized())),
	)
}

// RunBatchWithOptions validates and executes sequential JSONL file operations.
func RunBatchWithOptions(
	ctx context.Context,
	request BatchRequest,
	selectedProfile string,
	options RunOptions,
) error {
	return classifyProtocolError(
		"batch", filesystemapp.RunBatch(ctx, toBatchRequest(request), selectedProfile, filesystemOptions(options.normalized())),
	)
}

// RunSyncWithOptions executes a one-way directory synchronization.
func RunSyncWithOptions(
	ctx context.Context,
	request SyncRequest,
	selectedProfile string,
	options RunOptions,
) error {
	return classifyProtocolError(
		"sync "+string(request.Direction),
		syncapp.Run(ctx, request, selectedProfile, syncOptions(options.normalized())),
	)
}

// RunSyncStateWithOptions manages local, non-secret synchronization state.
func RunSyncStateWithOptions(
	ctx context.Context,
	request SyncStateRequest,
	options RunOptions,
) error {
	return classifyProtocolError(
		"sync state "+string(request.Operation),
		syncapp.RunState(ctx, request, syncOptions(options.normalized())),
	)
}

// RunSyncJobWithOptions manages and executes named synchronization jobs.
func RunSyncJobWithOptions(
	ctx context.Context,
	request SyncJobRequest,
	options RunOptions,
) error {
	return classifyProtocolError(
		"sync job "+string(request.Operation),
		syncapp.RunJob(ctx, request, syncOptions(options.normalized())),
	)
}

// RunSyncRecoveryWithOptions manages interrupted bidirectional runs.
func RunSyncRecoveryWithOptions(
	ctx context.Context,
	request SyncRecoveryRequest,
	options RunOptions,
) error {
	return classifyProtocolError(
		"sync recovery "+string(request.Operation),
		syncapp.RunRecovery(ctx, request, syncOptions(options.normalized())),
	)
}

// RunMetadataWithOptions executes a remote file-metadata use case.
func RunMetadataWithOptions(
	ctx context.Context,
	request MetadataRequest,
	selectedProfile string,
	options RunOptions,
) error {
	return classifyProtocolError(
		string(request.Operation),
		filesystemapp.RunMetadata(ctx, toMetadataRequest(request), selectedProfile, filesystemOptions(options.normalized())),
	)
}

// RunAdminWithOptions executes a read-only administrative use case.
func RunAdminWithOptions(
	ctx context.Context,
	request AdminRequest,
	selectedProfile string,
	options RunOptions,
) error {
	return classifyProtocolError(
		"admin "+string(request.Operation),
		adminapp.Run(ctx, request, selectedProfile, adminOptions(options.normalized())),
	)
}

// RunAdminSpaceMFACheckWithOptions verifies server-side MFA state for the
// separately authorized Space-administration namespace.
func RunAdminSpaceMFACheckWithOptions(
	ctx context.Context,
	selectedProfile string,
	options RunOptions,
) error {
	return classifyProtocolError(
		"admin space MFA",
		checkAdminSpaceMFA(ctx, selectedProfile, options.normalized()),
	)
}

// RunAdminUserCreateWithOptions creates one server user.
func RunAdminUserCreateWithOptions(
	ctx context.Context,
	request AdminUserCreateRequest,
	selectedProfile string,
	options RunOptions,
) error {
	return classifyProtocolError(
		"admin user create",
		adminapp.RunUserCreate(ctx, request, selectedProfile, adminOptions(options.normalized())),
	)
}

// RunAdminUserUpdateWithOptions updates one server user.
func RunAdminUserUpdateWithOptions(
	ctx context.Context,
	request AdminUserUpdateRequest,
	selectedProfile string,
	options RunOptions,
) error {
	return classifyProtocolError(
		"admin user update",
		adminapp.RunUserUpdate(ctx, request, selectedProfile, adminOptions(options.normalized())),
	)
}

// RunAdminUserStateWithOptions enables or disables one server user.
func RunAdminUserStateWithOptions(
	ctx context.Context,
	request AdminUserStateRequest,
	selectedProfile string,
	options RunOptions,
) error {
	return classifyProtocolError(
		"admin user state",
		adminapp.RunUserState(ctx, request, selectedProfile, adminOptions(options.normalized())),
	)
}

// RunAdminUserDeleteWithOptions permanently deletes one server user.
func RunAdminUserDeleteWithOptions(
	ctx context.Context,
	request AdminUserDeleteRequest,
	selectedProfile string,
	options RunOptions,
) error {
	return classifyProtocolError(
		"admin user delete",
		adminapp.RunUserDelete(ctx, request, selectedProfile, adminOptions(options.normalized())),
	)
}

// RunAdminGroupCreateWithOptions creates one server group.
func RunAdminGroupCreateWithOptions(
	ctx context.Context,
	request AdminGroupCreateRequest,
	selectedProfile string,
	options RunOptions,
) error {
	return classifyProtocolError(
		"admin group create",
		adminapp.RunGroupCreate(ctx, request, selectedProfile, adminOptions(options.normalized())),
	)
}

// RunAdminGroupUpdateWithOptions renames one server group.
func RunAdminGroupUpdateWithOptions(
	ctx context.Context,
	request AdminGroupUpdateRequest,
	selectedProfile string,
	options RunOptions,
) error {
	return classifyProtocolError(
		"admin group update",
		adminapp.RunGroupUpdate(ctx, request, selectedProfile, adminOptions(options.normalized())),
	)
}

// RunAdminGroupDeleteWithOptions permanently deletes one server group.
func RunAdminGroupDeleteWithOptions(
	ctx context.Context,
	request AdminGroupDeleteRequest,
	selectedProfile string,
	options RunOptions,
) error {
	return classifyProtocolError(
		"admin group delete",
		adminapp.RunGroupDelete(ctx, request, selectedProfile, adminOptions(options.normalized())),
	)
}

// RunAdminGroupMemberMutationWithOptions changes direct group membership.
func RunAdminGroupMemberMutationWithOptions(
	ctx context.Context,
	request AdminGroupMemberMutationRequest,
	selectedProfile string,
	options RunOptions,
) error {
	return classifyProtocolError(
		"admin group member",
		adminapp.RunGroupMemberMutation(ctx, request, selectedProfile, adminOptions(options.normalized())),
	)
}

// RunAdminRoleWithOptions lists or changes one user's server role.
func RunAdminRoleWithOptions(
	ctx context.Context,
	request AdminRoleRequest,
	selectedProfile string,
	options RunOptions,
) error {
	return classifyProtocolError(
		"admin user role",
		adminapp.RunRole(ctx, request, selectedProfile, adminOptions(options.normalized())),
	)
}

// RunSpaceWithOptions executes a Spaces use case.
func RunSpaceWithOptions(
	ctx context.Context, request SpaceRequest, selectedProfile string, options RunOptions,
) error {
	return classifyProtocolError(
		string(request.Operation),
		runSpace(ctx, request, selectedProfile, options.normalized()),
	)
}

// RunSpaceCreateWithOptions creates a project Space.
func RunSpaceCreateWithOptions(
	ctx context.Context,
	request SpaceCreateRequest,
	selectedProfile string,
	options RunOptions,
) error {
	return classifyProtocolError(
		"create", spacesapp.RunCreate(ctx, toSpaceCreateRequest(request), selectedProfile, spacesOptions(options.normalized())),
	)
}

// RunSpaceUpdateWithOptions updates project Space metadata.
func RunSpaceUpdateWithOptions(
	ctx context.Context,
	request SpaceUpdateRequest,
	selectedProfile string,
	options RunOptions,
) error {
	return classifyProtocolError(
		"update", spacesapp.RunUpdate(ctx, toSpaceUpdateRequest(request), selectedProfile, spacesOptions(options.normalized())),
	)
}

// RunSpaceLifecycleWithOptions changes Space lifecycle state.
func RunSpaceLifecycleWithOptions(
	ctx context.Context,
	request SpaceLifecycleRequest,
	selectedProfile string,
	options RunOptions,
) error {
	return classifyProtocolError(
		string(request.Operation),
		spacesapp.RunLifecycle(ctx, toSpaceLifecycleRequest(request), selectedProfile, spacesOptions(options.normalized())),
	)
}

// RunSpaceMemberWithOptions manages Space membership.
func RunSpaceMemberWithOptions(
	ctx context.Context,
	request SpaceMemberRequest,
	selectedProfile string,
	options RunOptions,
) error {
	return classifyProtocolError(
		"member "+string(request.Operation),
		spacesapp.RunMember(ctx, toSpaceMemberRequest(request), selectedProfile, spacesOptions(options.normalized())),
	)
}

// RunShareWithOptions executes a public-link or direct-sharing use case.
func RunShareWithOptions(
	ctx context.Context, request ShareRequest, selectedProfile string, options RunOptions,
) error {
	options = options.normalized()
	return classifyProtocolError(
		string(request.Operation),
		shareapp.Run(ctx, request, selectedProfile, shareOptions(options)),
	)
}

// RunTrashWithOptions executes a recycle-bin use case.
func RunTrashWithOptions(
	ctx context.Context,
	request TrashRequest,
	selectedProfile string,
	options RunOptions,
) error {
	return classifyProtocolError(
		"trash "+string(request.Operation),
		runTrash(ctx, request, selectedProfile, options.normalized()),
	)
}

// RunVersionWithOptions executes a historical file-version use case.
func RunVersionWithOptions(
	ctx context.Context,
	request VersionRequest,
	selectedProfile string,
	options RunOptions,
) error {
	return classifyProtocolError(
		"version "+string(request.Operation),
		runVersion(ctx, request, selectedProfile, options.normalized()),
	)
}

// RunSearchWithOptions executes a permission-aware remote search.
func RunSearchWithOptions(
	ctx context.Context,
	request SearchRequest,
	selectedProfile string,
	options RunOptions,
) error {
	return classifyProtocolError(
		"search", runSearch(
			ctx, request, selectedProfile, options.normalized(),
		),
	)
}

func classifyProtocolError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return apperror.Wrap(apperror.KindCanceled, operation, err)
	}
	status := protocolStatus(err)
	switch status {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return apperror.Wrap(apperror.KindUsage, operation, err)
	case http.StatusUnauthorized, http.StatusForbidden:
		return apperror.Wrap(apperror.KindAuthentication, operation, err)
	case http.StatusNotFound:
		return apperror.Wrap(apperror.KindNotFound, operation, err)
	case http.StatusConflict, http.StatusPreconditionFailed:
		return apperror.Wrap(apperror.KindConflict, operation, err)
	default:
		return err
	}
}

func protocolStatus(err error) int {
	var statusErr interface{ HTTPStatusCode() int }
	if errors.As(err, &statusErr) {
		return statusErr.HTTPStatusCode()
	}
	return 0
}

// Keep protocol result types at the application boundary without exposing
// concrete clients to command code.
type space = graph.Drive
