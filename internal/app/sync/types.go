// Package sync owns synchronization planning, execution, jobs, state, and
// interrupted-run recovery policy.
package sync

import (
	"context"
	"io"

	"github.com/mzner/ocis-cli/internal/logging"
	appoutput "github.com/mzner/ocis-cli/internal/output"
	syncmodel "github.com/mzner/ocis-cli/internal/sync"
	"github.com/mzner/ocis-cli/internal/syncjob"
	"github.com/mzner/ocis-cli/internal/syncrecovery"
	"github.com/mzner/ocis-cli/internal/webdav"
)

type Direction string

const (
	Push          Direction = "push"
	Pull          Direction = "pull"
	Bidirectional Direction = "bidirectional"
)

type Request struct {
	Direction                 Direction
	LocalRoot, RemoteRoot     string
	Includes, Excludes        []string
	Delete, Overwrite, DryRun bool
	MaxEntries                int
	ConflictStrategy, Prefer  string
}

type StateOperation string

const (
	StateList   StateOperation = "list"
	StateShow   StateOperation = "show"
	StateExport StateOperation = "export"
	StateRemove StateOperation = "remove"
)

type StateRequest struct {
	Operation         StateOperation
	ID, Profile       string
	Confirmed, DryRun bool
}

type RecoveryOperation string

const (
	RecoveryList   RecoveryOperation = "list"
	RecoveryShow   RecoveryOperation = "show"
	RecoveryRetry  RecoveryOperation = "retry"
	RecoveryRemove RecoveryOperation = "remove"
)

type RecoveryRequest struct {
	Operation         RecoveryOperation
	ID, Profile       string
	Confirmed, DryRun bool
}

type JobOperation string

const (
	JobAdd    JobOperation = "add"
	JobList   JobOperation = "list"
	JobShow   JobOperation = "show"
	JobRun    JobOperation = "run"
	JobRemove JobOperation = "remove"
)

type JobRequest struct {
	Operation                    JobOperation
	Name, Profile, Space         string
	Direction                    Direction
	LocalRoot, RemoteRoot        string
	Includes, Excludes           []string
	DeleteDestination, Overwrite bool
	MaxEntries                   int
	Confirmed, DryRun            bool
}

type StateRepository interface {
	Keys() ([]string, error)
	Load(string) (syncmodel.State, bool, error)
	Save(string, syncmodel.State) error
	Delete(string) (bool, error)
}
type JobRepository interface {
	Load() (syncjob.Store, error)
	Save(syncjob.Store) error
}
type RecoveryRepository interface {
	Keys() ([]string, error)
	Load(string) (syncrecovery.Journal, bool, error)
	Save(syncrecovery.Journal) error
	Delete(string) (bool, error)
}

type DAVClient interface {
	RemoveWithOptions(context.Context, string, webdav.RemoveOptions) error
	UploadWithOptions(context.Context, string, string, webdav.TransferOptions) error
	DownloadWithOptions(context.Context, string, string, webdav.TransferOptions) error
	CopyWithOptions(context.Context, string, string, webdav.MoveOptions) error
	MoveWithOptions(context.Context, string, string, webdav.MoveOptions) error
}

type Client interface {
	ProfileName() string
	AccountID() string
	SelectSpace(string) error
	SelectedSpaceID() string
	Context() context.Context
	List(string) ([]webdav.Item, error)
	Stat(string) (webdav.Item, error)
	EnsureCollection(string) error
	DiscoverUploadCapabilities(context.Context) webdav.TUSCapabilities
	DAV() DAVClient
}

type ClientFactory func(context.Context, string) (Client, error)

type Options struct {
	OutputMode     appoutput.Mode
	Out            io.Writer
	Space          string
	Logger         logging.Logger
	NewClient      ClientFactory
	SyncStates     StateRepository
	SyncJobs       JobRepository
	SyncRecoveries RecoveryRepository
}
