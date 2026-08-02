package command

import (
	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newSyncCommand(options *globalOptions) *cobra.Command {
	command := &cobra.Command{
		Use:   "sync",
		Short: "Synchronize local and remote directory trees",
		Long: "Synchronize directory trees using an account- and Space-bound " +
			"baseline. One-way sync never deletes destination-only items or " +
			"overwrites independently changed files by default. Bidirectional " +
			"sync applies only conflict-free plans and never resolves divergent " +
			"changes silently.",
	}
	command.AddCommand(
		newSyncDirectionCommand(options, app.SyncPush),
		newSyncDirectionCommand(options, app.SyncPull),
		newSyncBidirectionalCommand(options),
		newSyncJobCommand(options),
		newSyncStateCommand(options),
		newSyncRecoveryCommand(options),
	)
	return command
}

func newSyncRecoveryCommand(options *globalOptions) *cobra.Command {
	command := &cobra.Command{
		Use: "recovery", Aliases: []string{"recover"},
		Short: "Inspect and retry interrupted bidirectional synchronizations",
		Long: "Recovery journals contain no credentials and never replay stored " +
			"mutations. Retry authenticates, re-scans both trees, and builds a fresh plan.",
	}
	command.AddCommand(
		&cobra.Command{
			Use: "list", Aliases: []string{"ls"},
			Short: "List synchronization recovery journals", Args: noArgs,
			RunE: func(command *cobra.Command, _ []string) error {
				return app.RunSyncRecoveryWithOptions(
					command.Context(), app.SyncRecoveryRequest{
						Operation: app.SyncRecoveryList, Profile: options.profile,
					}, options.runOptions(command),
				)
			},
		},
		&cobra.Command{
			Use: "show RECOVERY_ID", Aliases: []string{"info", "stat"},
			Short: "Show one synchronization recovery journal", Args: exactArgs(1),
			RunE: func(command *cobra.Command, args []string) error {
				return app.RunSyncRecoveryWithOptions(
					command.Context(), app.SyncRecoveryRequest{
						Operation: app.SyncRecoveryShow, ID: args[0],
					}, options.runOptions(command),
				)
			},
		},
		newSyncRecoveryRetryCommand(options),
		newSyncRecoveryRemoveCommand(options),
	)
	return command
}

func newSyncRecoveryRetryCommand(options *globalOptions) *cobra.Command {
	var dryRun bool
	command := &cobra.Command{
		Use: "retry RECOVERY_ID", Short: "Re-scan and retry an interrupted synchronization",
		Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return app.RunSyncRecoveryWithOptions(
				command.Context(), app.SyncRecoveryRequest{
					Operation: app.SyncRecoveryRetry, ID: args[0],
					Profile: options.profile, DryRun: dryRun,
				}, options.runOptions(command),
			)
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"re-scan and print a fresh plan without changing either tree or state",
	)
	return command
}

func newSyncRecoveryRemoveCommand(options *globalOptions) *cobra.Command {
	var dryRun, yes bool
	command := &cobra.Command{
		Use: "remove RECOVERY_ID", Aliases: []string{"rm", "delete"},
		Short: "Remove one synchronization recovery journal", Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if !yes && !dryRun {
				confirmed, err := confirmAction(
					command,
					"Remove sync recovery journal "+args[0]+
						"? Local and remote files will not be changed.",
				)
				if err != nil || !confirmed {
					return err
				}
			}
			return app.RunSyncRecoveryWithOptions(
				command.Context(), app.SyncRecoveryRequest{
					Operation: app.SyncRecoveryRemove, ID: args[0],
					Confirmed: yes || !dryRun, DryRun: dryRun,
				}, options.runOptions(command),
			)
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false, "resolve the journal without removing it",
	)
	command.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	return command
}

func newSyncBidirectionalCommand(options *globalOptions) *cobra.Command {
	var (
		dryRun           bool
		includes         []string
		excludes         []string
		maxEntries       int
		conflictStrategy string
		prefer           string
	)
	command := &cobra.Command{
		Use:     "bidirectional LOCAL_DIRECTORY REMOTE_DIRECTORY",
		Aliases: []string{"bi"},
		Short:   "Synchronize changes in both directions",
		Long: "Build a three-way plan from the local tree, remote tree, and " +
			"last successful baseline. Conflicts stop the complete run before " +
			"mutation. The baseline advances only after successful application " +
			"and verification.",
		Args: exactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			return app.RunSyncWithOptions(
				command.Context(),
				app.SyncRequest{
					Direction: app.SyncBidirectional,
					LocalRoot: args[0], RemoteRoot: args[1],
					Includes: includes, Excludes: excludes,
					DryRun: dryRun, MaxEntries: maxEntries,
					ConflictStrategy: conflictStrategy, Prefer: prefer,
				},
				options.profile,
				options.runOptions(command),
			)
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"print the deterministic plan without changing either tree or state",
	)
	command.Flags().StringArrayVar(
		&includes, "include", nil,
		"include a slash-based glob (repeatable)",
	)
	command.Flags().StringArrayVar(
		&excludes, "exclude", nil,
		"exclude a slash-based glob or subtree (repeatable)",
	)
	command.Flags().IntVar(
		&maxEntries, "max-entries", 100000,
		"stop when either scanned tree exceeds this entry count",
	)
	command.Flags().StringVar(
		&conflictStrategy, "conflict-strategy", "abort",
		"conflict handling: abort or keep-both",
	)
	command.Flags().StringVar(
		&prefer, "prefer", "",
		"version kept at the original path with keep-both: local or remote",
	)
	return command
}

func newSyncJobCommand(options *globalOptions) *cobra.Command {
	command := &cobra.Command{
		Use:   "job",
		Short: "Manage reusable synchronization configurations",
		Long: "Save and run account- and Space-bound synchronization settings. " +
			"Job management is local; adding and running a job authenticate to " +
			"verify its exact binding.",
	}
	command.AddCommand(
		newSyncJobAddCommand(options),
		&cobra.Command{
			Use: "list", Aliases: []string{"ls"},
			Short: "List named synchronization jobs", Args: noArgs,
			RunE: func(command *cobra.Command, _ []string) error {
				return app.RunSyncJobWithOptions(
					command.Context(),
					app.SyncJobRequest{
						Operation: app.SyncJobList, Profile: options.profile,
					},
					options.runOptions(command),
				)
			},
		},
		&cobra.Command{
			Use: "show NAME", Aliases: []string{"info"},
			Short: "Show one named synchronization job", Args: exactArgs(1),
			RunE: func(command *cobra.Command, args []string) error {
				return app.RunSyncJobWithOptions(
					command.Context(),
					app.SyncJobRequest{
						Operation: app.SyncJobShow, Name: args[0],
					},
					options.runOptions(command),
				)
			},
		},
		newSyncJobRunCommand(options),
		newSyncJobRemoveCommand(options),
	)
	return command
}

func newSyncJobAddCommand(options *globalOptions) *cobra.Command {
	var (
		direction         string
		localRoot         string
		remoteRoot        string
		includes          []string
		excludes          []string
		deleteDestination bool
		overwrite         bool
		maxEntries        int
	)
	command := &cobra.Command{
		Use: "add NAME", Short: "Save a reusable synchronization job",
		Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return app.RunSyncJobWithOptions(
				command.Context(),
				app.SyncJobRequest{
					Operation: app.SyncJobAdd, Name: args[0],
					Profile: options.profile, Space: options.space,
					Direction: app.SyncDirection(direction),
					LocalRoot: localRoot, RemoteRoot: remoteRoot,
					Includes: includes, Excludes: excludes,
					DeleteDestination: deleteDestination,
					Overwrite:         overwrite, MaxEntries: maxEntries,
				},
				options.runOptions(command),
			)
		},
	}
	command.Flags().StringVar(
		&direction, "direction", "",
		"synchronization direction: push, pull, or bidirectional",
	)
	command.Flags().StringVar(
		&localRoot, "local", "", "local directory root",
	)
	command.Flags().StringVar(
		&remoteRoot, "remote", "", "remote directory root",
	)
	command.Flags().StringArrayVar(
		&includes, "include", nil,
		"include a slash-based glob (repeatable)",
	)
	command.Flags().StringArrayVar(
		&excludes, "exclude", nil,
		"exclude a slash-based glob or subtree (repeatable)",
	)
	command.Flags().BoolVar(
		&deleteDestination, "delete", false,
		"delete destination-only items when the job runs",
	)
	command.Flags().BoolVar(
		&overwrite, "overwrite", false,
		"let the source replace destination conflicts when the job runs",
	)
	command.Flags().IntVar(
		&maxEntries, "max-entries", 100000,
		"stop when either scanned tree exceeds this entry count",
	)
	_ = command.MarkFlagRequired("direction")
	_ = command.MarkFlagRequired("local")
	_ = command.MarkFlagRequired("remote")
	return command
}

func newSyncJobRunCommand(options *globalOptions) *cobra.Command {
	var dryRun bool
	command := &cobra.Command{
		Use: "run NAME", Short: "Run a named synchronization job",
		Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return app.RunSyncJobWithOptions(
				command.Context(),
				app.SyncJobRequest{
					Operation: app.SyncJobRun, Name: args[0],
					Profile: options.profile, Space: options.space,
					DryRun: dryRun,
				},
				options.runOptions(command),
			)
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"print the deterministic plan without changing either tree",
	)
	return command
}

func newSyncJobRemoveCommand(options *globalOptions) *cobra.Command {
	var dryRun, yes bool
	command := &cobra.Command{
		Use: "remove NAME", Aliases: []string{"rm", "delete"},
		Short: "Remove one named synchronization job", Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if !yes && !dryRun {
				confirmed, err := confirmAction(
					command,
					"Remove sync job "+args[0]+
						"? Saved state and files will not be deleted.",
				)
				if err != nil || !confirmed {
					return err
				}
			}
			return app.RunSyncJobWithOptions(
				command.Context(),
				app.SyncJobRequest{
					Operation: app.SyncJobRemove, Name: args[0],
					Confirmed: yes || !dryRun, DryRun: dryRun,
				},
				options.runOptions(command),
			)
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"resolve the job without removing it",
	)
	command.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	return command
}

func newSyncStateCommand(options *globalOptions) *cobra.Command {
	command := &cobra.Command{
		Use:   "state",
		Short: "Inspect, export, and remove saved synchronization state",
		Long: "Manage the versioned non-secret baselines saved after successful " +
			"synchronization runs. These operations are local and do not contact " +
			"an oCIS server.",
	}
	command.AddCommand(
		&cobra.Command{
			Use: "list", Aliases: []string{"ls"},
			Short: "List saved synchronization states", Args: noArgs,
			RunE: func(command *cobra.Command, _ []string) error {
				return app.RunSyncStateWithOptions(
					command.Context(),
					app.SyncStateRequest{
						Operation: app.SyncStateList, Profile: options.profile,
					},
					options.runOptions(command),
				)
			},
		},
		&cobra.Command{
			Use: "show STATE_ID", Aliases: []string{"info", "stat"},
			Short: "Show one synchronization-state binding", Args: exactArgs(1),
			RunE: func(command *cobra.Command, args []string) error {
				return app.RunSyncStateWithOptions(
					command.Context(),
					app.SyncStateRequest{
						Operation: app.SyncStateShow, ID: args[0],
					},
					options.runOptions(command),
				)
			},
		},
		&cobra.Command{
			Use:   "export STATE_ID",
			Short: "Export one complete state document as JSON",
			Args:  exactArgs(1),
			RunE: func(command *cobra.Command, args []string) error {
				if options.json || options.jsonl {
					return usageError(
						"sync state export",
						"--json and --jsonl are unnecessary because export already writes JSON",
					)
				}
				return app.RunSyncStateWithOptions(
					command.Context(),
					app.SyncStateRequest{
						Operation: app.SyncStateExport, ID: args[0],
					},
					options.runOptions(command),
				)
			},
		},
		newSyncStateRemoveCommand(options),
	)
	return command
}

func newSyncStateRemoveCommand(options *globalOptions) *cobra.Command {
	var dryRun, yes bool
	command := &cobra.Command{
		Use: "remove STATE_ID", Aliases: []string{"rm", "delete"},
		Short: "Remove one saved synchronization baseline", Args: exactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if !yes && !dryRun {
				confirmed, err := confirmAction(
					command,
					"Remove synchronization state "+args[0]+
						"? Local and remote files will not be deleted.",
				)
				if err != nil || !confirmed {
					return err
				}
			}
			return app.RunSyncStateWithOptions(
				command.Context(),
				app.SyncStateRequest{
					Operation: app.SyncStateRemove, ID: args[0],
					Confirmed: yes || !dryRun, DryRun: dryRun,
				},
				options.runOptions(command),
			)
		},
	}
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"resolve the state without removing it",
	)
	command.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	return command
}

func newSyncDirectionCommand(
	options *globalOptions,
	direction app.SyncDirection,
) *cobra.Command {
	var (
		deleteDestination bool
		overwrite         bool
		dryRun            bool
		includes          []string
		excludes          []string
		maxEntries        int
	)
	use := "push LOCAL_DIRECTORY REMOTE_DIRECTORY"
	short := "Synchronize a local directory into a remote directory"
	if direction == app.SyncPull {
		use = "pull REMOTE_DIRECTORY LOCAL_DIRECTORY"
		short = "Synchronize a remote directory into a local directory"
	}
	command := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  exactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			localRoot, remoteRoot := args[0], args[1]
			if direction == app.SyncPull {
				remoteRoot, localRoot = args[0], args[1]
			}
			return app.RunSyncWithOptions(
				command.Context(),
				app.SyncRequest{
					Direction: direction, LocalRoot: localRoot,
					RemoteRoot: remoteRoot, Includes: includes,
					Excludes: excludes, Delete: deleteDestination,
					Overwrite: overwrite, DryRun: dryRun,
					MaxEntries: maxEntries,
				},
				options.profile,
				options.runOptions(command),
			)
		},
	}
	command.Flags().BoolVar(
		&deleteDestination, "delete", false,
		"delete destination-only items",
	)
	command.Flags().BoolVar(
		&overwrite, "overwrite", false,
		"let the source replace destination conflicts",
	)
	command.Flags().BoolVar(
		&dryRun, "dry-run", false,
		"print the deterministic plan without changing either tree",
	)
	command.Flags().StringArrayVar(
		&includes, "include", nil,
		"include a slash-based glob (repeatable)",
	)
	command.Flags().StringArrayVar(
		&excludes, "exclude", nil,
		"exclude a slash-based glob or subtree (repeatable)",
	)
	command.Flags().IntVar(
		&maxEntries, "max-entries", 100000,
		"stop when either scanned tree exceeds this entry count",
	)
	return command
}
