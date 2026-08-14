// Package filesystem owns remote filesystem, batch, and metadata policy.
package filesystem

import (
	"context"
	"io"

	"github.com/mzner/ocis-cli/internal/graph"
	"github.com/mzner/ocis-cli/internal/logging"
	appoutput "github.com/mzner/ocis-cli/internal/output"
	"github.com/mzner/ocis-cli/internal/sharing"
	"github.com/mzner/ocis-cli/internal/webdav"
)

type Operation string

const (
	List     Operation = "list"
	Stat     Operation = "stat"
	Cat      Operation = "cat"
	Tree     Operation = "tree"
	DU       Operation = "du"
	Upload   Operation = "upload"
	Download Operation = "download"
	Mkdir    Operation = "mkdir"
	Touch    Operation = "touch"
	Move     Operation = "mv"
	Copy     Operation = "cp"
	Remove   Operation = "remove"
)

type Request struct {
	Operation                                                Operation
	Source, Destination                                      string
	Recursive, Overwrite, NoClobber, DryRun, Verify, Parents bool
	MaxDepth, MaxEntries                                     int
}
type TreeEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Type  string `json:"type"`
	Size  int64  `json:"size,omitempty"`
	Depth int    `json:"depth"`
}
type Usage struct {
	Path         string `json:"path"`
	LogicalBytes int64  `json:"logicalBytes"`
	Files        int    `json:"files"`
	Directories  int    `json:"directories"`
	Entries      int    `json:"entries"`
	MaxDepth     int    `json:"maxDepth"`
	MaxEntries   int    `json:"maxEntries"`
	Complete     bool   `json:"complete"`
}

type BatchRequest struct {
	Input                              io.Reader
	DryRun, Confirmed, ContinueOnError bool
	MaxOperations                      int
}
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
type BatchOperationError struct {
	Code    int    `json:"code"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
}
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

type MetadataOperation string

const (
	TagList        MetadataOperation = "tag-list"
	TagAdd         MetadataOperation = "tag-add"
	TagRemove      MetadataOperation = "tag-remove"
	FavoriteSet    MetadataOperation = "favorite-set"
	FavoriteUnset  MetadataOperation = "favorite-unset"
	PropertyGet    MetadataOperation = "property-get"
	PropertySet    MetadataOperation = "property-set"
	PropertyRemove MetadataOperation = "property-remove"
)

type MetadataRequest struct {
	Operation              MetadataOperation
	Path                   string
	Tags                   []string
	Namespace, Name, Value string
	DryRun                 bool
}

type Client interface {
	SelectSpace(string) error
	Context() context.Context
	List(string) ([]webdav.Item, error)
	Stat(string) (webdav.Item, error)
	Stream(string, io.Writer) error
	Capabilities() (webdav.Capabilities, error)
	GetProperty(string, webdav.PropertyName) (webdav.PropertyValue, error)
	SetProperty(string, webdav.PropertyName, string) error
	RemoveProperty(string, webdav.PropertyName) error
	EnsureCollection(string) error
	Move(string, string, bool) error
	Copy(string, string, bool) error
	Remove(string, bool) error
	Upload(context.Context, string, string, webdav.TransferOptions) error
	Download(context.Context, string, string, webdav.TransferOptions) error
	ListMyDrives(context.Context) ([]graph.Drive, error)
	SharingCapabilities(context.Context) (sharing.Capabilities, error)
	AddTags(context.Context, string, []string) error
	RemoveTags(context.Context, string, []string) error
}
type ClientFactory func(context.Context, string) (Client, error)
type Options struct {
	OutputMode  appoutput.Mode
	In          io.Reader
	Out, Err    io.Writer
	Concurrency int
	Quiet       bool
	Space       string
	Logger      logging.Logger
	NewClient   ClientFactory
}
