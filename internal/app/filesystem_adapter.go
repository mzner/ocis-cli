package app

import (
	"context"
	"io"

	filesystemapp "github.com/mzner/ocis-cli/internal/app/filesystem"
	"github.com/mzner/ocis-cli/internal/graph"
	"github.com/mzner/ocis-cli/internal/sharing"
	"github.com/mzner/ocis-cli/internal/webdav"
)

type filesystemClientAdapter struct{ client *client }

func (a filesystemClientAdapter) SelectSpace(v string) error           { return a.client.selectSpace(v) }
func (a filesystemClientAdapter) Context() context.Context             { return a.client.context() }
func (a filesystemClientAdapter) List(v string) ([]webdav.Item, error) { return a.client.list(v) }
func (a filesystemClientAdapter) Stat(v string) (webdav.Item, error)   { return a.client.stat(v) }
func (a filesystemClientAdapter) Stream(v string, w io.Writer) error   { return a.client.stream(v, w) }
func (a filesystemClientAdapter) Capabilities() (webdav.Capabilities, error) {
	return a.client.capabilities()
}
func (a filesystemClientAdapter) GetProperty(v string, p webdav.PropertyName) (webdav.PropertyValue, error) {
	return a.client.getProperty(v, p)
}
func (a filesystemClientAdapter) SetProperty(v string, p webdav.PropertyName, s string) error {
	return a.client.setProperty(v, p, s)
}
func (a filesystemClientAdapter) RemoveProperty(v string, p webdav.PropertyName) error {
	return a.client.removeProperty(v, p)
}
func (a filesystemClientAdapter) EnsureCollection(v string) error {
	return a.client.ensureCollection(v)
}
func (a filesystemClientAdapter) Move(s, d string, o bool) error { return a.client.move(s, d, o) }
func (a filesystemClientAdapter) Copy(s, d string, o bool) error { return a.client.copy(s, d, o) }
func (a filesystemClientAdapter) Remove(v string, r bool) error  { return a.client.remove(v, r) }
func (a filesystemClientAdapter) Upload(ctx context.Context, l, r string, o webdav.TransferOptions) error {
	return a.client.davClient().UploadWithOptions(ctx, l, r, o)
}
func (a filesystemClientAdapter) Download(ctx context.Context, r, l string, o webdav.TransferOptions) error {
	return a.client.davClient().DownloadWithOptions(ctx, r, l, o)
}
func (a filesystemClientAdapter) ListMyDrives(ctx context.Context) ([]graph.Drive, error) {
	return a.client.graphClient().ListMyDrives(ctx)
}
func (a filesystemClientAdapter) SharingCapabilities(ctx context.Context) (sharing.Capabilities, error) {
	return a.client.sharingClient().Capabilities(ctx)
}
func (a filesystemClientAdapter) AddTags(ctx context.Context, id string, t []string) error {
	return a.client.graphClient().AddTags(ctx, id, t)
}
func (a filesystemClientAdapter) RemoveTags(ctx context.Context, id string, t []string) error {
	return a.client.graphClient().RemoveTags(ctx, id, t)
}

func filesystemOptions(options RunOptions) filesystemapp.Options {
	return filesystemapp.Options{OutputMode: options.OutputMode, In: options.In, Out: options.Out, Err: options.Err, Concurrency: options.Concurrency, Quiet: options.Quiet, Space: options.Space, Logger: options.Logger, NewClient: func(ctx context.Context, p string) (filesystemapp.Client, error) {
		c, err := newClientWithOptions(ctx, p, options)
		if err != nil {
			return nil, err
		}
		return filesystemClientAdapter{c}, nil
	}}
}

func toFilesystemRequest(r FilesystemRequest) filesystemapp.Request {
	return filesystemapp.Request{Operation: filesystemapp.Operation(r.Operation), Source: r.Source, Destination: r.Destination, Recursive: r.Recursive, Overwrite: r.Overwrite, NoClobber: r.NoClobber, DryRun: r.DryRun, Verify: r.Verify, Parents: r.Parents, MaxDepth: r.MaxDepth, MaxEntries: r.MaxEntries}
}
func toBatchRequest(r BatchRequest) filesystemapp.BatchRequest {
	return filesystemapp.BatchRequest{Input: r.Input, DryRun: r.DryRun, Confirmed: r.Confirmed, ContinueOnError: r.ContinueOnError, MaxOperations: r.MaxOperations}
}
func toMetadataRequest(r MetadataRequest) filesystemapp.MetadataRequest {
	return filesystemapp.MetadataRequest{Operation: filesystemapp.MetadataOperation(r.Operation), Path: r.Path, Tags: r.Tags, Namespace: r.Namespace, Name: r.Name, Value: r.Value, DryRun: r.DryRun}
}
