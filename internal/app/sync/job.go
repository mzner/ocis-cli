package sync

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"text/tabwriter"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
	syncmodel "github.com/mzner/ocis-cli/internal/sync"
	"github.com/mzner/ocis-cli/internal/syncjob"
)

type syncJobRemoval struct {
	Name    string `json:"name"`
	Removed bool   `json:"removed"`
	DryRun  bool   `json:"dryRun"`
}

func RunJob(
	ctx context.Context,
	request JobRequest,
	options Options,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store, err := options.SyncJobs.Load()
	if err != nil {
		return fmt.Errorf("load sync jobs: %w", err)
	}
	switch request.Operation {
	case JobAdd:
		return addSyncJob(ctx, request, store, options)
	case JobList:
		return listSyncJobs(request.Profile, store, options)
	case JobShow:
		job, err := findSyncJob(request.Name, store)
		if err != nil {
			return err
		}
		return writeSyncJob(job, options)
	case JobRun:
		job, err := findSyncJob(request.Name, store)
		if err != nil {
			return err
		}
		return executeSyncJob(ctx, request, job, options)
	case JobRemove:
		return removeSyncJob(request, store, options)
	default:
		return apperror.Wrap(
			apperror.KindUsage, "sync job",
			fmt.Errorf("unknown sync job command %q", request.Operation),
		)
	}
}

func addSyncJob(
	ctx context.Context,
	request JobRequest,
	store syncjob.Store,
	options Options,
) error {
	if err := syncjob.ValidateName(request.Name); err != nil {
		return apperror.Wrap(apperror.KindUsage, "sync job add", err)
	}
	if _, exists := store.Jobs[request.Name]; exists {
		return apperror.Wrap(
			apperror.KindConflict, "sync job add",
			fmt.Errorf(
				"sync job %q already exists; remove it before recreating it",
				request.Name,
			),
		)
	}
	prepared, err := prepareSyncRequest(Request{
		Direction: request.Direction,
		LocalRoot: request.LocalRoot, RemoteRoot: request.RemoteRoot,
		Includes: request.Includes, Excludes: request.Excludes,
		Delete: request.DeleteDestination, Overwrite: request.Overwrite,
		MaxEntries: request.MaxEntries,
	})
	if err != nil {
		return err
	}
	client, err := options.NewClient(ctx, request.Profile)
	if err != nil {
		return err
	}
	if err := client.SelectSpace(request.Space); err != nil {
		return err
	}
	accountID := client.AccountID()
	if accountID == "" {
		return errors.New("cannot bind a sync job to an unauthenticated account")
	}
	job := syncjob.Job{
		Name: request.Name, Profile: client.ProfileName(), AccountID: accountID,
		SpaceID: client.SelectedSpaceID(), Direction: prepared.direction,
		LocalRoot: prepared.localRoot, RemoteRoot: prepared.remoteRoot,
		Includes: append([]string(nil), prepared.request.Includes...),
		Excludes: append([]string(nil), prepared.request.Excludes...),
		Delete:   request.DeleteDestination, Overwrite: request.Overwrite,
		MaxEntries: request.MaxEntries,
	}
	if err := syncjob.Validate(job); err != nil {
		return apperror.Wrap(apperror.KindUsage, "sync job add", err)
	}
	store = cloneSyncJobStore(store)
	store.Jobs[job.Name] = job
	if err := options.SyncJobs.Save(store); err != nil {
		return fmt.Errorf("save sync jobs: %w", err)
	}
	return output(
		options, "sync-job", job,
		"Added sync job %s (%s: %s)\n",
		job.Name, job.Direction, syncJobRoots(job),
	)
}

func listSyncJobs(
	profile string,
	store syncjob.Store,
	options Options,
) error {
	names := make([]string, 0, len(store.Jobs))
	for name, job := range store.Jobs {
		if profile == "" || job.Profile == profile {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	jobs := make([]syncjob.Job, 0, len(names))
	for _, name := range names {
		jobs = append(jobs, store.Jobs[name])
	}
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "sync-job", jobs)
	}
	if len(jobs) == 0 {
		_, err := fmt.Fprintln(options.Out, "No synchronization jobs found.")
		return err
	}
	writer := tabwriter.NewWriter(options.Out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(
		writer,
		"NAME\tPROFILE\tDIRECTION\tSPACE\tDELETE\tOVERWRITE\tROOTS",
	); err != nil {
		return err
	}
	for _, job := range jobs {
		space := job.SpaceID
		if space == "" {
			space = "personal"
		}
		if _, err := fmt.Fprintf(
			writer, "%s\t%s\t%s\t%s\t%t\t%t\t%s\n",
			job.Name, job.Profile, job.Direction, space,
			job.Delete, job.Overwrite, syncJobRoots(job),
		); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func writeSyncJob(job syncjob.Job, options Options) error {
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "sync-job", job)
	}
	space := job.SpaceID
	if space == "" {
		space = "personal"
	}
	if _, err := fmt.Fprintf(
		options.Out,
		"Name: %s\nProfile: %s\nAccount: %s\nSpace: %s\n"+
			"Direction: %s\nLocal root: %s\nRemote root: %s\n"+
			"Delete destination-only items: %t\nOverwrite conflicts: %t\n"+
			"Maximum entries: %d\n",
		job.Name, job.Profile, job.AccountID, space, job.Direction,
		job.LocalRoot, job.RemoteRoot, job.Delete, job.Overwrite,
		job.MaxEntries,
	); err != nil {
		return err
	}
	if len(job.Includes) > 0 {
		if _, err := fmt.Fprintf(
			options.Out, "Includes: %v\n", job.Includes,
		); err != nil {
			return err
		}
	}
	if len(job.Excludes) > 0 {
		_, err := fmt.Fprintf(options.Out, "Excludes: %v\n", job.Excludes)
		return err
	}
	return nil
}

func executeSyncJob(
	ctx context.Context,
	request JobRequest,
	job syncjob.Job,
	options Options,
) error {
	if request.Profile != "" && request.Profile != job.Profile {
		return apperror.Wrap(
			apperror.KindUsage, "sync job run",
			fmt.Errorf(
				"job %q is bound to profile %q, not %q",
				job.Name, job.Profile, request.Profile,
			),
		)
	}
	if request.Space != "" {
		return apperror.Wrap(
			apperror.KindUsage, "sync job run",
			errors.New(
				"a named job is already bound to an exact Space; remove --space",
			),
		)
	}
	prepared, err := prepareSyncRequest(Request{
		Direction: Direction(job.Direction),
		LocalRoot: job.LocalRoot, RemoteRoot: job.RemoteRoot,
		Includes: job.Includes, Excludes: job.Excludes,
		Delete: job.Delete, Overwrite: job.Overwrite,
		DryRun: request.DryRun, MaxEntries: job.MaxEntries,
	})
	if err != nil {
		return fmt.Errorf("invalid saved sync job %q: %w", job.Name, err)
	}
	client, err := options.NewClient(ctx, job.Profile)
	if err != nil {
		return err
	}
	currentIdentity := client.AccountID()
	if currentIdentity != job.AccountID {
		return apperror.Wrap(
			apperror.KindAuthentication, "sync job run",
			fmt.Errorf(
				"job %q belongs to a different account on profile %q; "+
					"log in as the original account or recreate the job",
				job.Name, job.Profile,
			),
		)
	}
	if job.SpaceID != "" {
		if err := client.SelectSpace(job.SpaceID); err != nil {
			return fmt.Errorf(
				"sync job %q Space %q is unavailable: %w",
				job.Name, job.SpaceID, err,
			)
		}
	}
	if client.SelectedSpaceID() != job.SpaceID {
		return errors.New("resolved sync-job Space does not match its binding")
	}
	return runPreparedSync(ctx, prepared, client, options)
}

func removeSyncJob(
	request JobRequest,
	store syncjob.Store,
	options Options,
) error {
	if !request.Confirmed && !request.DryRun {
		return apperror.Wrap(
			apperror.KindUsage, "sync job remove",
			errors.New("removing a sync job requires explicit confirmation"),
		)
	}
	job, err := findSyncJob(request.Name, store)
	if err != nil {
		return err
	}
	result := syncJobRemoval{Name: job.Name, DryRun: request.DryRun}
	if !request.DryRun {
		store = cloneSyncJobStore(store)
		delete(store.Jobs, job.Name)
		if err := options.SyncJobs.Save(store); err != nil {
			return fmt.Errorf("save sync jobs: %w", err)
		}
		result.Removed = true
	}
	format := "Removed sync job %s\n"
	if request.DryRun {
		format = "Would remove sync job %s\n"
	}
	return output(options, "sync-job-removal", result, format, job.Name)
}

func findSyncJob(name string, store syncjob.Store) (syncjob.Job, error) {
	if err := syncjob.ValidateName(name); err != nil {
		return syncjob.Job{}, apperror.Wrap(
			apperror.KindUsage, "sync job", err,
		)
	}
	job, found := store.Jobs[name]
	if !found {
		return syncjob.Job{}, apperror.Wrap(
			apperror.KindNotFound, "sync job",
			fmt.Errorf("unknown sync job %q; run `ocis sync job list`", name),
		)
	}
	return job, nil
}

func syncJobRoots(job syncjob.Job) string {
	if job.Direction == syncmodel.Bidirectional {
		return job.LocalRoot + " <-> " + job.RemoteRoot
	}
	if job.Direction == syncmodel.Pull {
		return job.RemoteRoot + " -> " + job.LocalRoot
	}
	return job.LocalRoot + " -> " + job.RemoteRoot
}

func cloneSyncJobStore(store syncjob.Store) syncjob.Store {
	cloned := syncjob.Store{
		Version: store.Version,
		Jobs:    make(map[string]syncjob.Job, len(store.Jobs)),
	}
	for name, job := range store.Jobs {
		job.Includes = append([]string(nil), job.Includes...)
		job.Excludes = append([]string(nil), job.Excludes...)
		cloned.Jobs[name] = job
	}
	return cloned
}
