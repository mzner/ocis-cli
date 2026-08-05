# Architecture

Authentication and credential boundaries are documented separately, with
sequence and storage diagrams, in [AUTHENTICATION.md](AUTHENTICATION.md).

The repository follows the standard Go command layout:

```text
cmd/
  ocis/               executable entrypoint only
internal/
  command/            Cobra command tree and input validation
  app/                application use-case orchestration
  apperror/           stable error categories and exit-code mapping
  auth/               OIDC protocol implementation
  config/             persisted profile model and atomic storage
  credentials/        OS credential-service adapter
  graph/              LibreGraph Spaces, directory, and permission client
  httpapi/            authenticated retrying HTTP transport
  logging/            opt-in diagnostic logging abstraction
  output/             terminal and JSON/JSONL rendering
  search/             WebDAV search-files REPORT client and response mapping
  sharing/            OCS share discovery, public-link, and capability client
  sync/               deterministic one-way and bidirectional planning model
  syncjob/            versioned named-sync configuration persistence
  syncstate/          versioned, atomic non-secret sync-state persistence
  trash/              WebDAV recycle-bin protocol client
  transfer/           parallel upload/download traversal and progress
  versions/           WebDAV historical-version protocol client
  webdav/             DAV HTTP, retry, resume, and integrity adapter
test/
  integration/        opt-in compiled-CLI and pinned-oCIS compatibility tests
```

## Dependency direction

`cmd/ocis` depends on `internal/command`, which depends on `internal/app`.
The application layer may depend on focused infrastructure packages under `internal`,
but application and infrastructure packages never depend on Cobra or the
command package.

The executable entrypoint is intentionally small. It translates an application
error to a message and non-zero exit code; business behavior remains testable
without starting a subprocess.

## Responsibilities

- `cmd/ocis`: create the signal-aware root context, invoke the application, and
  map errors to exit codes.
- `internal/command`: define Cobra commands, flags, aliases, help, completion,
  and syntactic validation.
- `internal/app`: expose typed use-case requests, select profiles, and coordinate
  authentication and protocol operations. Focused services such as
  `bidirectional_sync_service.go`, `config_service.go`,
  `batch_service.go`, `filesystem_service.go`, `filesystem_tree_service.go`,
  `filesystem_du_service.go`, `filesystem_touch_service.go`,
  `filesystem_walk.go`, `metadata_service.go`,
  `share_overview_service.go`,
  `space_member_service.go`, `space_update_service.go`,
  `space_lifecycle_service.go`, and
  the split `admin_*_service.go` files keep each use case independent;
  `admin_guard.go` owns account-admin and MFA preflights, while `runtime.go`
  contains shared application wiring.
- `internal/apperror`: classify usage, authentication, not-found, and conflict
  errors without coupling application services to Cobra.
- `internal/auth`: implement OIDC discovery, dynamic native-client
  registration, token exchange, refresh, and userinfo.
- `internal/config`: validate server URLs and atomically load/save non-secret
  profile settings, and resolve the effective profile-config path.
- `internal/credentials`: store passwords, OAuth tokens, client secrets, and
  protected resumable-upload locations in separate size-bounded entries in
  macOS Keychain, Linux Secret Service, or Windows Credential Manager;
  no plaintext or legacy-format migration path exists.
- `internal/graph`: discover, create, inspect, update, and control the lifecycle
  and membership of Spaces through LibreGraph; list, inspect, and mutate
  directory identities allowed by the server; manage direct group membership
  and runtime-discovered application-role assignments; manage direct
  file/folder permissions using server-advertised roles; inspect or update
  public-link permission facets; and mutate resource tags by stable resource
  ID. Identity and authorization policy remain on the server.
- `internal/httpapi`: send replayable authenticated API requests with bounded
  retries for non-WebDAV protocols.
- `internal/logging`: provide an injected no-op or text diagnostic logger.
- `internal/output`: render human-readable output and versioned JSON/JSONL
  envelopes through injected writers.
- `internal/search`: issue bounded `search-files` REPORT requests and decode
  ranked WebDAV multistatus responses without depending on Cobra or Space
  selection policy.
- `internal/sharing`: resolve outgoing and received OCS share records, create
  and revoke public links, accept or decline received shares, and decode
  Spaces and sharing capability flags.
  Public-link property mutations use LibreGraph after the OCS record resolves
  the stable resource and permission IDs.
- `internal/sync`: compare source/destination or local/remote trees with the
  last successful baseline, apply filters, and produce deterministic plans.
  Bidirectional actions identify their local or remote target, detect only
  unique regular-file renames, reject ambiguous cross-platform path identity,
  and fail closed on divergent changes and changed subtrees below a directory
  deletion.
- `internal/syncjob`: validate and atomically persist reusable non-secret sync
  configurations beside the normal profile configuration.
- `internal/syncstate`: atomically persist versioned, non-secret sync baselines
  in the platform state directory with owner-only permissions; discover state
  IDs without decoding content so corrupt entries remain removable.
- `internal/syncrecovery`: atomically persist non-secret interrupted-run and
  conflict journals. Stored actions are evidence only; recovery always
  re-scans and re-plans before mutation.
- `internal/trash`: list recycle items, decode trash-specific WebDAV
  properties, restore to original paths, and permanently purge selected items
  or a complete Space trash bin.
- `internal/transfer`: coordinate bounded parallel local and remote traversal
  and aggregate byte progress; atomically replace local files after successful
  downloads.
- `internal/versions`: list, stream, and restore historical file versions
  through the resource-ID-based WebDAV metadata endpoint.
- `internal/webdav`: implement DAV requests, retries, capability-negotiated TUS
  uploads, atomic resumable downloads, direct file streaming, size verification, path encoding,
  authentication headers, metadata and checksum response mapping, and safe
  scalar custom-property `PROPFIND`/`PROPPATCH` operations.

Protocol-specific behavior belongs in dedicated `internal/auth`,
`internal/graph`, `internal/search`, `internal/sharing`, `internal/trash`,
`internal/versions`, and `internal/webdav` adapters. Recursive local/remote
traversal belongs in `internal/transfer`.

Configuration, credentials, protected upload-session storage, named sync jobs,
sync baselines, and recovery journals enter the application through
`ConfigRepository`, `CredentialRepository`, `UploadSessionRepository`,
`SyncJobRepository`, `SyncStateRepository`, and `SyncRecoveryRepository` ports.
Production adapters use the filesystem and OS credential service; tests use
in-memory repositories.

The integration suite is a separate outer test adapter. It invokes the
compiled executable, drives the embedded IDP over its browser-facing HTTP
flow, and uses the production OS credential adapter. Its helpers may decode
the public JSON envelope but must not call `internal/app` or protocol clients.
Fast package tests remain Docker-independent.

## Design rules

- Dependencies point inward toward use cases.
- Configuration I/O is isolated and tested.
- Destructive commands fail closed.
- Destructive Space operations require explicit intent in both the Cobra and
  application layers; interactive commands also confirm before mutation.
- Permanent trash removal requires explicit intent in both the Cobra and
  application layers; dry runs resolve the exact item without mutating it.
- Share removal requires explicit intent in both the Cobra and application
  layers. Share IDs are resolved against the caller's outgoing shares before
  mutation, and direct-share roles come from the target resource.
- The share overview joins account-wide OCS share records with the caller's
  LibreGraph drive inventory. It ignores the saved default Space unless an
  explicit `--space` filter is provided and excludes declined invitations by
  default.
- Space names and aliases are convenience selectors. Destructive operations
  on disabled Spaces use stable IDs.
- Server-advertised permissions and roles are authoritative; the CLI does not
  infer authorization from a local role table.
- Account administration proves the server's account-management permission and
  MFA state through the guarded user-inventory route before every read or
  mutation. Space administration uses an MFA-only guard because oCIS
  authorizes Account Admin, Space Admin, and Space Manager separately.
- Password input crosses the Cobra/application boundary in memory only. It is
  read from a hidden prompt or environment variable, never a value flag, and
  never enters output data.
- Self-disablement, self-deletion, and self-role-revocation fail closed.
- Administrative directory selectors follow the backend contracts exposed by
  oCIS. Stable IDs are printed, names are conveniences, and the CLI does not
  invent unsupported pagination or nested-group behavior.
- Network protocol details do not enter the executable entrypoint.
- Cobra remains an outer adapter and does not enter application services.
- No global mutable state.
- Use-case operations use named types and constants instead of free-form
  strings.
- Dependencies, streams, clocks where needed, and logging cross package
  boundaries explicitly.
- Diagnostic logs never contain passwords, tokens, or client secrets.
- Errors retain operation context and are returned to the entrypoint.
- Persisted account-specific state is owned by a stable account fingerprint:
  OIDC issuer plus subject, or Basic server plus username.
- Sync baselines are also bound to the profile, Space, direction, and exact
  local and remote roots. They advance only after a fully successful,
  re-verified run.
- Bidirectional execution refuses the complete plan before mutation when any
  conflict exists, verifies both action endpoints, and reuses conditional
  atomic upload and resumable atomic download adapters. Dry runs, conflicts,
  cancellation, and failed operations never advance the baseline.
- One-way sync never infers destructive intent: `--overwrite` resolves content
  conflicts and `--delete` independently permits destination-only deletion.
- Sync-state selectors are stable opaque IDs. User-facing commands accept only
  unambiguous hexadecimal prefixes, and removal requires application-layer
  confirmation in addition to the Cobra prompt or `--yes`.
- Named sync jobs store exact profile, account, Space, direction, roots, and
  policies. Execution revalidates that binding and delegates to the existing
  planner; job configuration never implements a second transfer path.
- An unavailable persisted Space is cleared and reported; commands never
  silently fall back to personal files.
- Cancellation propagates through Cobra contexts, application use cases, HTTP
  requests, and transfer workers and maps to exit code 130.
- New behavior requires tests at its narrowest package boundary.
- Core application, authentication, Graph, HTTP transport, search, sharing,
  trash, transfer, versions, and WebDAV packages maintain at least 75%
  statement coverage.
- Machine-readable output and exit codes are public compatibility contracts.
