package app

import (
	"context"
	"fmt"

	spacesapp "github.com/mzner/ocis-cli/internal/app/spaces"
	"github.com/mzner/ocis-cli/internal/graph"
)

type spacesClientAdapter struct {
	client       *client
	dependencies Dependencies
}

func (a spacesClientAdapter) Graph() spacesapp.GraphClient { return a.client.graphClient() }
func (a spacesClientAdapter) ClearDefaultSpace(id string) error {
	p := a.client.store.Profiles[a.client.name]
	if p.DefaultSpace != id {
		return nil
	}
	p.DefaultSpace, p.DefaultSpaceOwner = "", ""
	a.client.store.Profiles[a.client.name] = p
	if err := saveStore(a.dependencies, a.client.store); err != nil {
		return fmt.Errorf("save profile: %w", err)
	}
	return nil
}
func spacesOptions(options RunOptions) spacesapp.Options {
	return spacesapp.Options{OutputMode: options.OutputMode, Out: options.Out, Logger: options.Logger, NewClient: func(ctx context.Context, p string) (spacesapp.Client, error) {
		c, err := newClientWithOptions(ctx, p, options)
		if err != nil {
			return nil, err
		}
		return spacesClientAdapter{client: c, dependencies: options.Dependencies}, nil
	}}
}
func toSpaceCreateRequest(r SpaceCreateRequest) spacesapp.CreateRequest {
	return spacesapp.CreateRequest{Name: r.Name, Description: r.Description, Quota: r.Quota, DryRun: r.DryRun}
}
func toSpaceUpdateRequest(r SpaceUpdateRequest) spacesapp.UpdateRequest {
	return spacesapp.UpdateRequest{Identifier: r.Identifier, Name: r.Name, Description: r.Description, Alias: r.Alias, Quota: r.Quota, DryRun: r.DryRun}
}
func toSpaceLifecycleRequest(r SpaceLifecycleRequest) spacesapp.LifecycleRequest {
	return spacesapp.LifecycleRequest{Operation: spacesapp.LifecycleOperation(r.Operation), Identifier: r.Identifier, Permanent: r.Permanent, DryRun: r.DryRun}
}
func toSpaceMemberRequest(r SpaceMemberRequest) spacesapp.MemberRequest {
	return spacesapp.MemberRequest{Operation: spacesapp.MemberOperation(r.Operation), Space: r.Space, PermissionID: r.PermissionID, RecipientID: r.RecipientID, RecipientIsID: r.RecipientIsID, RecipientType: r.RecipientType, Role: r.Role, DryRun: r.DryRun}
}
func loadSpaceDetailsThroughDomain(ctx context.Context, c *client, d graph.Drive, o RunOptions) (spacesapp.Details, error) {
	return spacesapp.LoadDetails(ctx, spacesClientAdapter{client: c, dependencies: o.Dependencies}, d)
}
