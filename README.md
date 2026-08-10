# oCIS CLI

[![CI](https://github.com/mzner/ocis-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/mzner/ocis-cli/actions/workflows/ci.yml)
[![oCIS integration](https://github.com/mzner/ocis-cli/actions/workflows/integration.yml/badge.svg)](https://github.com/mzner/ocis-cli/actions/workflows/integration.yml)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache--2.0-blue.svg)](LICENSE)

A script-friendly CLI for connecting to one or more oCIS servers,
authenticating in the browser with OIDC, and managing files through WebDAV.

This is an independent community project and is not affiliated with or
endorsed by the organization that develops oCIS.

The source follows the standard Go `cmd`/`internal` layout. See
[ARCHITECTURE.md](ARCHITECTURE.md) for package responsibilities and dependency
rules. See [AUTHENTICATION.md](AUTHENTICATION.md) for diagrams of OIDC, PKCE, Basic
authentication, token refresh, profile storage, and the macOS, Linux, and
Windows keyring backends.

Commands, flags, generated help, aliases, validation, and shell completion are
implemented with Cobra. Cobra is confined to the outer `internal/command`
adapter; authentication, configuration, and WebDAV code do not depend on it.

Generate shell completion with:

```sh
ocis completion zsh > "${fpath[1]}/_ocis"
```

## Install

```sh
go install github.com/mzner/ocis-cli/cmd/ocis@latest
```

With Homebrew on macOS or Linux:

```sh
brew install mzner/tap/ocis-cli
```

On Windows with Scoop, the same repository acts as the Scoop bucket:

```powershell
scoop bucket add mzner https://github.com/mzner/homebrew-tap
scoop install mzner/ocis-cli
```

Prebuilt archives for Linux, macOS, and Windows are available from
[GitHub Releases](https://github.com/mzner/ocis-cli/releases). Extract the
archive for your operating system and architecture, then place `ocis` (or
`ocis.exe`) in a directory on `PATH`.

Every release includes SHA-256 checksums, one SPDX JSON SBOM per archive, and
GitHub build-provenance attestations. For example:

```sh
gh release download v1.0.0 --repo mzner/ocis-cli
archive=ocis-cli_1.0.0_darwin_arm64.tar.gz
grep "  ${archive}$" checksums.txt | shasum -a 256 --check
gh attestation verify "$archive" --repo mzner/ocis-cli
```

On Linux, replace `shasum -a 256 --check` with `sha256sum --check`.

## Build from source

Requires Go 1.26.5 or newer:

```sh
make test
make build
make install
make uninstall # alias: make remove
```

The slower black-box compatibility suite starts a disposable, pinned full oCIS
server and tests the compiled CLI:

```sh
make integration
```

See [test/integration/README.md](test/integration/README.md) for its coverage,
Linux Secret Service setup, lifecycle targets, and environment variables.

## Connect and sign in

Point directly at any oCIS server:

```sh
ocis server add work https://cloud.example.com
ocis auth setup work
ocis auth login work
```

`auth setup` discovers the server's OIDC configuration. If the server supports
dynamic client registration, it creates a native client and stores any returned
client secret in the operating system's credential service. Otherwise it prints
the exact embedded-IDP entry an administrator must add. It never edits the
remote server configuration.

`auth login` then opens the browser, uses Authorization Code + PKCE, receives
the redirect on a temporary loopback port, and saves refreshable tokens in the
operating system's credential service.

Administrative operations may require a token issued after multi-factor
authentication:

```sh
ocis auth login work --mfa
```

The CLI reads the first MFA authentication-context value advertised by the
server's OCS capabilities, matching oCIS Web's behavior, and sends it as the
OIDC `acr_values` parameter. For an external identity provider whose required
value is known but not advertised, use
`ocis auth login work --mfa --acr VALUE`. MFA step-up requires OIDC; it is not
available with Basic authentication. The identity provider—not this CLI—shows
and verifies the second-factor challenge.

For a local server with a self-signed certificate:

```sh
ocis login \
  --server https://localhost:9200 \
  --name local \
  --insecure
```

`--insecure` is saved on that profile and only affects its connections. A server
URL must otherwise use `https`, because every authenticated request carries a
password or access token; `--insecure` is also what permits a cleartext
`http://` URL for a development server whose network path you trust. The check
applies to a stored URL as well as a new one, and a redirect from `https` to
`http://` is refused rather than followed. A profile saved by an earlier release
that used cleartext is reported when selected and can be repaired with
`ocis server add NAME https://...`; `server list`, `status`, `logout`, and
`server remove` keep working meanwhile.

If the deployment enables Basic authentication:

```sh
ocis login \
  --server https://cloud.example.com \
  --name work \
  --auth basic \
  --username alice
```

The CLI securely prompts for the password and validates it with a WebDAV
request. For non-interactive use, set `OCIS_PASSWORD`.

## Multiple servers

```sh
ocis server add work https://cloud.company.test
ocis server add local https://localhost:9200 --insecure

ocis login work
ocis login local

ocis server list
ocis server use work
ocis ls /

ocis --profile local ls /
```

The current profile is marked with `*` in `server list`.

Run `ocis doctor [PROFILE]` to validate the config schema, operating-system
credential service, authentication, advertised WebDAV capabilities, Spaces,
public-link support, and resumable-upload support.

## Spaces

Discover the spaces available to the authenticated user:

```sh
ocis space list
ocis space info Engineering
ocis --json space info Engineering
ocis space use Engineering
ocis space current
ocis space unset
```

`space info` includes metadata, quota usage, members, the current user's role,
server-advertised roles, and member-management capabilities. If the server
does not allow the caller to list members, the metadata remains available and
the human output marks members as unavailable. `space stat` remains an alias
for compatibility.

Create a project space when your account has the required server-side
permission:

```sh
ocis space create Engineering
ocis space create Engineering \
  --description "Shared engineering files" \
  --quota 10GB
ocis space create Engineering --quota unlimited --dry-run
```

The quota accepts raw bytes, decimal units (`KB`, `MB`, `GB`, `TB`), binary
units (`KiB`, `MiB`, `GiB`, `TiB`), `unlimited`, or `default`. The default
omits the quota from the request so the server applies its configured default.
Use `--dry-run` to inspect the operation without loading a profile or contacting
the server.

Update project-space metadata and quota:

```sh
ocis space update Engineering --name Platform
ocis space update Platform \
  --description "Shared platform files" \
  --alias project/platform \
  --quota 20GiB
ocis space update Platform --description= --dry-run
```

An explicitly empty `--description` or `--alias` clears that field. Omit a flag
to leave its field unchanged.

Manage members:

```sh
ocis space member list Engineering
ocis space members ls Engineering
ocis space member add Engineering alice --role viewer
ocis space member add Engineering developers --type group --role editor
ocis space member update Engineering PERMISSION_ID --role manager
ocis space member remove Engineering PERMISSION_ID
```

`member add` searches users by username, email, or display name and groups by
display name. Use `--recipient-id` when the argument is already an opaque Graph
ID. `member list` prints both the subject ID and the permission ID; use the
permission ID for `member update` and `member remove`. Role names and IDs are
read from the server for each Space, so the CLI does not hard-code a
deployment's role IDs. The convenience aliases `viewer`, `editor`, and
`manager` resolve to an unambiguous matching server role; use the exact
advertised name or ID when a server defines multiple roles in one category.

Disable and restore a project Space:

```sh
ocis space disable Engineering --dry-run
ocis space disable Engineering
ocis space restore SPACE_ID
```

Disabling is reversible and preserves the Space data. Save the stable Space ID
printed by `disable`, because disabled Spaces may no longer be discoverable by
name. Permanent deletion only accepts a disabled Space ID, requires the
explicit `--permanent` flag, and prompts for confirmation:

```sh
ocis space delete SPACE_ID --permanent --dry-run
ocis space delete SPACE_ID --permanent
```

Use `--yes` only in reviewed automation to skip confirmation for `disable`,
`member remove`, or permanent `delete`.

`space use` stores the selected space ID for the authenticated user in the
current server profile. The selection is bound to a fingerprint of the stable
OIDC issuer and `sub` claim, or the Basic-authentication server and username.
Logging in as a different account or logging out clears the selection so a
Space from one account is never reused for another. Reauthenticating the same
OIDC subject preserves it even if the display name changes. Names and aliases
are resolved case-insensitively; IDs remain stable when a space is renamed.
`space unset` clears the selection and returns to the implicit personal-file
root. `space current` explains which behavior is active, and the current
default—including implicit personal storage—is marked with `*` by `space list`.

If the selected Space was deleted or the account lost membership, the CLI
clears the stale selection and fails the current command with an explicit
message. It does not silently run the command against personal files.

Space discovery uses `/graph/v1.0/me/drives`, so it only returns spaces where
the authenticated user is a member. Administrative lookup uses
`/graph/v1.0/drives`, which lets the server expose additional Spaces to a Space
Admin without assuming that the caller is a member. Server support for Spaces
does not imply permission to create, inspect members, update, disable, restore,
or delete them. Every operation relies on server-side authorization; a caller
without the required permission receives an authentication/authorization error
(exit code 3).

Override the default for one command with `--space`:

```sh
ocis --space project/engineering ls /
ocis --space Engineering upload ./report.pdf /reports/report.pdf
```

Without a selected space, file commands continue to use the user's personal
file root for backward compatibility.

## Administration

Server administration is grouped under an explicit namespace:

```sh
ocis admin user list
ocis admin user list --search einstein
ocis admin user list --search "Alice Example"
ocis admin user list --search-raw '"einstein"'
ocis admin user info USERNAME_OR_ID
OCIS_USER_PASSWORD='initial secret' \
  ocis admin user create einstein \
  --display-name "Albert Einstein" --email einstein@example.test
ocis admin user update einstein --display-name "Prof. Einstein"
OCIS_USER_PASSWORD='replacement secret' \
  ocis admin user update einstein --set-password
ocis admin user disable einstein
ocis admin user enable einstein
ocis admin user delete einstein
ocis admin user role available
ocis admin user role list einstein
ocis admin user role grant einstein "Space Admin"
ocis admin user role revoke einstein ASSIGNMENT_ID

ocis admin group list
ocis admin group list --search engineering
ocis admin group info GROUP_NAME_OR_ID
ocis admin group create Engineering
ocis admin group update Engineering --name Platform
ocis admin group member list GROUP_NAME_OR_ID
ocis admin group member add Engineering einstein
ocis admin group member remove Engineering einstein
ocis admin group delete Engineering

ocis admin space list
ocis admin space info SPACE_NAME_ALIAS_OR_ID
ocis admin space create Engineering --quota 10GiB
ocis admin space update Engineering --description "Shared files"
ocis admin space member add Engineering einstein --role viewer
ocis admin space disable Engineering
ocis admin space restore SPACE_ID
ocis admin space delete SPACE_ID --permanent
```

`list` can be shortened to `ls`, and `info` to `stat`. Human list output has
labeled columns and always includes each resource's opaque server ID. JSON and
JSONL use the same global flags as the rest of the CLI.

These commands mirror current oCIS server behavior:

- Every `admin user` and `admin group` operation first calls oCIS's guarded
  user inventory endpoint. This proves both full account-management permission
  and the server's MFA state before any lookup or mutation. Being logged in
  does not make a user an administrator.
- If oCIS returns `X-Ocis-Mfa-Required: true`, sign in again with
  `ocis auth login PROFILE --mfa`. The CLI does not treat an ordinary token,
  a role name, or a local flag as proof of MFA.
- `--search TEXT` treats the value as one literal LibreGraph search phrase, so
  spaces and hyphens do not become search syntax.
  `--search-raw QUERY` is the explicit escape hatch for an exact server-side
  expression. The flags are mutually exclusive. Current oCIS accepts only
  simple search expressions and still controls the minimum query length and
  which results are visible.
- User `info` passes an exact username or stable user ID to the configured
  identity backend. Group `info` and member listing similarly pass an exact
  group name or stable group ID. Name lookup support is backend-dependent;
  stable IDs from list output are portable and unambiguous. Display names are
  not treated as user selectors.
- Group membership contains direct users. Current oCIS rejects nested groups.
  A group reported with the `ReadOnly` group type is labeled `read-only`.
- User creation and deletion respect the server's advertised identity-backend
  capabilities. Updates reject fields advertised as read-only. LDAP and other
  externally managed identity backends may disable some or all mutations.
- Initial and replacement passwords are read from a hidden terminal prompt or
  `OCIS_USER_PASSWORD`. There is no password-value command flag, and passwords
  are never included in normal or structured output.
- The CLI refuses to disable or delete the currently authenticated account and
  refuses to revoke its role. Destructive operations prompt unless `--yes` is
  supplied; all mutations support `--dry-run`.
- Role names and IDs come from `/graph/v1.0/applications`; the CLI does not
  hard-code deployment-specific role UUIDs. Role management is unavailable
  when the oCIS role service is not configured. Current oCIS may replace an
  existing role when another role is assigned.
- `admin space list` uses `/graph/v1.0/drives` instead of the member-only
  `/graph/v1.0/me/drives` endpoint. Current oCIS may return a restricted,
  caller-visible set instead of denying a non-Space-Admin, so a successful list
  is not proof of administrative permission.
- A global Space Admin is not automatically a member or manager of every
  Space. `admin space info` reuses the normal Space detail service and reports
  member information as unavailable when the server denies that permission.
- `admin space` mutations reuse the tested `space` services. They require
  server-confirmed MFA first, then let each oCIS endpoint enforce its Space
  creation, management, or membership permission. Account Admin, Space Admin,
  and Space Manager are not assumed to be equivalent.
- A Space mutation's `--dry-run` validates and resolves its inputs without
  sending the mutation request. It therefore does not claim that the server
  will authorize the eventual write.

The global `--space` flag is intentionally rejected for administrative
commands because it selects a file-operation root, not an administration
scope. Current oCIS returns these directory collections as complete responses;
the CLI does not expose unverified client-side pagination flags.

The server authorizes every request. A regular user may receive `403
Forbidden`, and a deployment that does not expose the relevant LibreGraph
endpoint receives an explicit unsupported-inventory error. Account
administration, global Space administration, and per-Space member management
are separate permissions.

## Trash

Deleting a file or directory with `ocis rm` moves it to the selected Space's
trash. With no explicit default Space, trash commands use personal storage.
Override the selection for one command with `--space`:

```sh
ocis trash list
ocis --space Engineering trash list
ocis --json --space Engineering trash list
```

The final column in human output is the opaque trash item ID. Use that ID—not
the original path—to restore or permanently remove an item:

```sh
ocis --space Engineering trash restore ITEM_ID --dry-run
ocis --space Engineering trash restore ITEM_ID
ocis --space Engineering trash restore ITEM_ID --overwrite

ocis --space Engineering trash remove ITEM_ID --dry-run
ocis --space Engineering trash remove ITEM_ID
ocis --space Engineering trash empty --dry-run
ocis --space Engineering trash empty
```

Restore returns the resource to its original path and refuses to replace an
existing destination unless `--overwrite` is set. `trash remove` and
`trash empty` are irreversible and prompt for confirmation; `--yes` is
available for reviewed automation. Trash permissions are enforced by the
server, so some Space roles may list or restore items without being allowed to
empty the trash.

## File versions

List historical versions of a file, inspect their metadata, or download an
older copy without changing the current file:

```sh
ocis version list /reports/report.pdf
ocis version info /reports/report.pdf VERSION_ID
ocis version download /reports/report.pdf VERSION_ID ./report-old.pdf
ocis version download /reports/report.pdf VERSION_ID - > report-old.pdf
```

The version ID is an opaque value printed by `version list`; pass it exactly as
shown. File commands honor the profile's default Space and the global
`--space` override:

```sh
ocis --space Engineering version list /reports/report.pdf
```

Restoring changes the current content of the remote file and therefore prompts
for confirmation. Preview the resolved operation first when needed:

```sh
ocis version restore /reports/report.pdf VERSION_ID --dry-run
ocis version restore /reports/report.pdf VERSION_ID
```

Use `--yes` only in reviewed automation. Version downloads verify the listed
size and available ETag by default, replace local files atomically, and support
`--no-clobber`. Versions apply to files, not directories. Availability and
restore permission are enforced by the server and may differ by Space role.

## Files

```sh
ocis ls /
ocis tree /Documents
ocis du /Documents
ocis cat /Documents/notes.txt
ocis stat /Documents/report.pdf
ocis mkdir /cli-demo
ocis mkdir -p /Projects/2026/Reports
ocis touch /Documents/notes.txt
ocis upload ./README.md /cli-demo/README.md
ocis upload --recursive ./photos /backup/photos
ocis --json ls /cli-demo
ocis --jsonl ls /cli-demo
ocis download /cli-demo/README.md ./downloaded.md
ocis download --recursive /backup/photos ./
ocis cp /cli-demo/README.md /cli-demo/README-copy.md
ocis mv /cli-demo/README-copy.md /cli-demo/README-renamed.md
ocis rm /cli-demo/README.md
ocis rm --recursive /cli-demo
```

Use `mkdir -p` (or `mkdir --parents`) when intermediate directories may be
missing:

```sh
ocis mkdir -p /Projects/2026/Reports
```

The command inspects every component, creates only missing directories, and is
safe to run repeatedly. Existing directories are accepted. It fails without
creating deeper components if any component is a file, and the server remains
authoritative for access permissions. Batch `mkdir` records expose the same
behavior with `"parents": true`.

`touch` safely creates a zero-byte remote file when the path is missing. If a
regular file already exists, it is left byte-for-byte unchanged. Existing
directories are rejected. Unlike the local Unix command, `ocis touch` does not
update modification times because WebDAV does not provide a portable,
reliable operation for doing so. Creation uses a temporary upload followed by
a no-overwrite move, so a concurrent creator cannot be overwritten.

`cat` streams one remote file as raw bytes to stdout without adding a newline,
so it can be piped or redirected. It rejects directories and cannot be combined
with `--json` or `--jsonl` because those modes would corrupt the file stream:

```sh
ocis cat /Documents/notes.txt
ocis cat /Documents/archive.tar.gz > archive.tar.gz
```

`tree` displays a deterministic, recursive view. Traversal is bounded to 10
levels and 10,000 resources by default; the requested root counts as depth zero
and as one resource. Reduce or explicitly raise those limits when appropriate:

```sh
ocis tree /Documents --max-depth 2
ocis --json tree /Documents --max-depth 4 --max-entries 25000
```

Reaching `--max-depth` stops descent at that level. Exceeding
`--max-entries` fails without printing a partial result, so scripts cannot
mistake an incomplete tree for a complete one. JSON and JSONL entries include
`name`, `path`, `type`, `size` for files, and `depth`.

`du` recursively sums the logical `getcontentlength` reported for files. It
does not claim to report physical storage, deduplication, version history, trash
usage, or Space quota consumption. Counts include the requested resource. The
default traversal bounds are 100 levels and 100,000 resources:

```sh
ocis du /Documents
ocis --json du /Documents --max-depth 20 --max-entries 250000
```

Machine output exposes `logicalBytes`, `files`, `directories`, `entries`, and
`complete`. When the requested depth excludes descendants, `complete` is false
and human output says `depth-limited`. Exceeding the entry limit fails without
printing a partial result.

## Batch file operations

`batch` accepts one JSON object per line from a file or stdin. Supported
operations are `mkdir`, `touch`, `upload`, `download`, `copy` (`cp`), `move`
(`mv`), and `remove` (`rm` or `delete`):

```jsonl
{"operation":"mkdir","path":"/reports/archive"}
{"operation":"touch","path":"/reports/notes.txt"}
{"operation":"upload","source":"./report.pdf","destination":"/reports/report.pdf","noClobber":true}
{"operation":"copy","source":"/reports/report.pdf","destination":"/reports/archive/report.pdf"}
{"operation":"remove","path":"/reports/old","recursive":true}
```

Preview or execute the complete manifest:

```sh
ocis batch operations.jsonl --dry-run
ocis batch operations.jsonl --yes
cat operations.jsonl | ocis batch - --yes
ocis --jsonl batch operations.jsonl --yes --continue-on-error
```

The complete JSONL document is parsed, strictly schema-checked, and bounded by
`--max-operations` (default 1,000) before the first mutation. Runtime checks
such as remote permissions or a changing server can still fail after earlier
operations succeeded, so batches are deliberately non-atomic. By default the
first runtime failure stops execution and later records are reported as
`skipped`. `--continue-on-error` attempts the remaining records. Either mode
returns the first failure's normal exit code after writing all result records.
Execution always requires `--yes`; `--dry-run` never mutates. Batch uploads
cannot consume stdin and batch downloads cannot write to stdout because those
streams are reserved for the manifest and results.

`cp` and `mv` accept either a complete destination path or an existing remote
directory. When the destination is a directory, the source basename is
appended automatically:

```sh
ocis mv /report.pdf /Documents
ocis cp /report.pdf /Archive/
```

These resolve to `/Documents/report.pdf` and `/Archive/report.pdf`. A trailing
slash explicitly requires an existing directory and fails if it is missing or
is a file. Dry-run and structured output report the resolved destination.
Explicit full paths such as `/Documents/final.pdf` remain unchanged. `cp` and
`mv` refuse to overwrite an existing resolved destination unless `--overwrite`
is provided. `rm` refuses to remove directories unless `--recursive` is
provided.

Uploads and downloads verify the transferred size and available ETag
consistency by default. For non-empty files, uploads automatically use TUS when
the server advertises TUS 1.0 creation support. If an upload is interrupted,
running the same command with the same local source and remote destination
continues from the offset acknowledged by the server. The upload location may
contain a transfer token, so resumable-upload state is account-bound and stored
in the operating system's credential service rather than the config file.
Expired sessions and changed local files start a new upload automatically.
Zero-byte files and servers without compatible TUS support use WebDAV `PUT`.

Downloads use an atomic `.part` file and resume it with an HTTP byte range when
possible. A resumed range is guarded by the entity validator recorded for that
`.part` file, so a remote file that changed since the interruption restarts from
the beginning instead of mixing old and new content. Use
`--no-clobber` to protect an existing destination, `--interactive` to confirm
an operation, or `--dry-run` to print the plan without changing files:

```sh
ocis upload --no-clobber ./report.pdf /reports/report.pdf
ocis download --interactive /reports/report.pdf ./report.pdf
ocis mv --dry-run /reports/draft.pdf /reports/final.pdf
ocis rm --dry-run --recursive /old-backup

printf 'hello\n' | ocis upload - /notes/hello.txt
ocis download /notes/hello.txt - > hello.txt
```

Temporary network errors, HTTP `429`, and HTTP `5xx` responses are retried with
bounded exponential backoff, never waiting longer than 30 seconds between
attempts. A server may ask for a specific delay with `Retry-After`; the CLI
honors it exactly when it is within that limit. A longer delay stops the command
with an error naming the requested wait, because retrying sooner than a
throttling server allows can extend a rate-limit ban — run the command again
later. Either way no response can stall a command indefinitely. Global
reliability controls are available on every command:

```sh
ocis --timeout 2m --retries 5 --concurrency 8 upload --recursive ./photos /photos
```

Human output writes transfer progress to stderr so stdout remains suitable for
redirection. Use `--quiet` to suppress progress.

For recursive downloads, an existing destination is treated as a parent
directory. For example, `ocis download /demo ./ --recursive` creates
`./demo`. Passing an existing `./demo` directory reuses it and does not create
`./demo/demo`. A destination that does not exist is created as the downloaded
directory itself.

## One-way synchronization

Reconcile complete directory trees from one authoritative source:

```sh
# Local directory to remote directory
ocis sync push ./project /project --dry-run
ocis sync push ./project /project

# Remote directory to local directory
ocis sync pull /project ./project --dry-run
ocis sync pull /project ./project
```

Both commands operate on directories. Local symbolic links and other special
files are rejected rather than followed. Every run first produces a
deterministic plan. On the first run, a different file already present at the
destination is a conflict; on later runs, an independently changed destination
is a conflict. The default stops before making any change when the plan
contains a conflict. Use `--overwrite` only when the selected source should
replace those destination changes:

```sh
ocis sync push ./project /project --overwrite
```

Destination-only files are retained by default. `--delete` separately permits
their deletion. Replacing a destination directory with a source file requires
both `--overwrite` and `--delete`.

Use repeatable slash-based glob filters to limit the tree:

```sh
ocis sync push ./site /site \
  --include 'assets/*' \
  --include '*.html' \
  --exclude '*.tmp' \
  --dry-run
```

`--dry-run`, `--json`, and `--jsonl` expose the same plan without mutation.
Conflicts return exit code 5. Tree scans are bounded by
`--max-entries` (default 100,000).

After a complete successful run, the CLI saves a versioned non-secret baseline
bound to the profile, stable account identity, Space, direction, local root,
remote root, and canonical include/exclude policy. This prevents two filtered
jobs over the same roots from sharing an incompatible baseline. Partial or
failed runs never advance it. Sync state is kept separately from `config.json`:

- macOS: `~/Library/Application Support/ocis-cli/sync`
- Linux: `$XDG_STATE_HOME/ocis-cli/sync`, or
  `~/.local/state/ocis-cli/sync`
- Windows: the user's local application-data cache under `ocis-cli/sync`

Set `OCIS_STATE_DIR` to override the state directory for testing or isolated
automation. Sync state contains file metadata and content fingerprints, but no
passwords or OAuth tokens.

### Named sync jobs

Save frequently repeated sync settings under a portable name:

```sh
ocis --profile work sync job add website \
  --direction push \
  --local ./site \
  --remote /website \
  --exclude '*.tmp'

ocis --profile work sync job add project-two-way \
  --direction bidirectional \
  --local ./project \
  --remote /project

ocis sync job list
ocis sync job show website
ocis sync job run website --dry-run
ocis sync job run website
ocis sync job remove website --yes
```

`job add` resolves the local root to an absolute path and binds the job to the
current stable account identity and exact Space ID. It also stores the
direction, remote root, include/exclude patterns, one-way deletion and
overwrite policies, and entry limit. Running the job uses those saved settings and the
normal sync planner; it fails before scanning if another account is logged in,
the bound Space is unavailable, or `--profile` names a different profile.
`--space` cannot override a named job.

Job definitions are non-secret configuration stored separately from both
`config.json` and disposable synchronization state:

- macOS: `~/Library/Application Support/ocis-cli/sync-jobs.json`
- Linux: `$XDG_CONFIG_HOME/ocis-cli/sync-jobs.json`, or
  `~/.config/ocis-cli/sync-jobs.json`
- Windows: `%AppData%\ocis-cli\sync-jobs.json`

When `OCIS_CONFIG` is set, `sync-jobs.json` is placed beside that file. Set
`OCIS_SYNC_JOBS` to override the job-file path explicitly. The file is written
atomically with owner-only permissions and contains no passwords or tokens.
Removing a job leaves its synchronization baseline and all local and remote
files untouched.

Named jobs do not include a scheduler or background process. Use the operating
system's scheduler to invoke `ocis sync job run NAME` when unattended execution
is appropriate.

### Sync-state management

Manage these local baselines without contacting the server:

```sh
ocis sync state list
ocis --profile work sync state list
ocis sync state show STATE_ID
ocis sync state export STATE_ID > sync-state.json
ocis sync state remove STATE_ID --dry-run
ocis sync state remove STATE_ID --yes
```

`list` prints the shortest unique prefix of at least 12 characters; `show`,
`export`, and `remove` accept any unambiguous hexadecimal prefix of at least
eight characters. Machine-readable list and show output include the complete ID.
Export writes a standalone versioned JSON document and therefore does not use
the global `--json` or `--jsonl` envelope flags.

Removing state does not delete local or remote files. It removes only the
saved comparison baseline, so the next synchronization treats both trees as a
first run and may report pre-existing differences as conflicts. Removal
requires interactive confirmation or `--yes`; `--dry-run` resolves the ID
without changing state. Corrupt state remains visible in `state list` and can
be removed by ID.

### Bidirectional synchronization

Preview or apply a conflict-safe reconciliation in both directions:

```sh
ocis sync bidirectional ./project /project --dry-run
ocis sync bi ./project /project --dry-run
ocis --json sync bi ./project /project --dry-run
ocis sync bi ./project /project
ocis sync bi ./project /project \
  --conflict-strategy keep-both --prefer local
```

The command scans both directory trees, binds state to the exact profile,
account, Space, roots, and canonical filters, and reports a target of `local`
or `remote` for every transfer, move, conflict copy, directory creation, or deletion. `--dry-run`
never changes local files, remote resources, or saved state.

On an initial comparison, local-only entries are proposed for upload,
remote-only entries for download, matching entries are skipped, and different
pre-existing entries are conflicts. A successful first execution creates the
bidirectional baseline. Later, a change made on only one side propagates to the
other, matching changes are skipped, deletions become tombstones, and different
changes on both sides conflict.

Any conflict stops the complete run before mutation and returns exit code 5.
The CLI never silently chooses one version. For an ordinary file/content
conflict, `--conflict-strategy keep-both` requires an explicit `--prefer local`
or `--prefer remote`. The preferred content remains at the original path and
the losing content is preserved on both sides under a deterministic name such
as `report.conflict-remote-a1b2c3d4.txt`. Directory, type, occupied-copy-path,
and filter-excluded-copy conflicts still abort safely.

Conflict-free plans use atomic,
resumable downloads and conditional atomic WebDAV uploads, verify local
fingerprints and remote ETags immediately before each mutation, re-scan both
trees for convergence, and atomically advance the baseline only after complete
success. Unique regular-file renames are applied as a local or WebDAV move
instead of delete plus transfer. Ambiguous matches and directory moves are
deliberately represented by the normal safe operations because the CLI does
not guess identity. Timestamp-only comparisons tolerate the common two-second
filesystem precision difference; checksums and ETags remain authoritative.

A cancelled or partially failed run may have completed earlier actions, but
it never advances the baseline. Before every mutation, the CLI atomically
updates a non-secret recovery journal. Retry always re-scans and builds a new
plan instead of replaying stored requests:

```sh
ocis sync recovery list
ocis sync recovery show RECOVERY_ID
ocis sync recovery retry RECOVERY_ID --dry-run
ocis sync recovery retry RECOVERY_ID
ocis sync recovery remove RECOVERY_ID --yes
```

Recovery IDs use the same unambiguous-prefix rules as sync-state IDs. Completed
runs remove their journal. Failed, canceled, and unresolved-conflict reports
remain until a successful retry/re-run or explicit removal. Journals contain
profile/account/Space bindings, paths, filters, fingerprints, the planned
actions, and progress—but never passwords, OAuth tokens, client secrets, or
protected transfer URLs. Their default locations are:

- macOS: `~/Library/Application Support/ocis-cli/sync-recovery`
- Linux: `$XDG_STATE_HOME/ocis-cli/sync-recovery`, or
  `~/.local/state/ocis-cli/sync-recovery`
- Windows: the user's local application-data cache under
  `ocis-cli/sync-recovery`

Set `OCIS_SYNC_RECOVERY_DIR` to override this directory for testing or isolated
automation.

Directory roots are anchors and are never proposed for deletion. A missing
root is recreated on the missing side. Deleting or replacing a directory also
conflicts when the other side changed or added anything in its subtree.
Symbolic links and other unsupported local file types are rejected. Rename
detection never guesses ambiguous content. Paths that differ only by Unicode
normalization or Unicode case folding are rejected before mutation so the same
tree remains representable on case-sensitive and case-insensitive platforms.

## File metadata

`stat` displays labels for the normal DAV fields and includes tags, favorite
state, and every checksum returned by the server:

```sh
ocis stat /reports/report.pdf
ocis --json stat /reports/report.pdf
ocis --space Engineering stat /reports/report.pdf
```

Checksums are server-provided metadata. The CLI does not invent or recalculate
missing algorithms.

List, add, and remove resource tags:

```sh
ocis tag list /reports/report.pdf
ocis tag add /reports/report.pdf approved quarterly
ocis tag add /reports/report.pdf "customer review"
ocis tag remove /reports/report.pdf draft
ocis tag remove /reports/report.pdf draft --dry-run
```

Comma-separated tag arguments are accepted and duplicate arguments are
removed before the request. Tag mutations resolve the path through WebDAV and
send the stable resource ID to the current LibreGraph tag endpoint. The server
remains authoritative for tag length, write permission, and deployment
support.

Mark or unmark a resource as a favorite:

```sh
ocis favorite set /reports/report.pdf
ocis favorite unset /reports/report.pdf
ocis favorite set /reports/report.pdf --dry-run
```

Advanced users can manage scalar custom WebDAV properties by providing an
absolute namespace URI and XML property name:

```sh
ocis property get /reports/report.pdf \
  https://example.com/metadata review-status
ocis property set /reports/report.pdf \
  https://example.com/metadata review-status approved
ocis property remove /reports/report.pdf \
  https://example.com/metadata review-status
```

Custom property values are plain scalar text, not raw XML. The reserved
`DAV:` and `http://owncloud.org/ns` namespaces are rejected; use `stat`,
`tag`, or `favorite` for their supported properties. Favorite and custom
property mutations run only when the selected WebDAV endpoint advertises
`PROPPATCH`. A missing custom property produces an explicit
unsupported-or-not-set error. All metadata commands honor the selected
profile Space and the global `--space` override. Mutations support `--dry-run`.

Use `--verbose` for sanitized diagnostic details such as selected operations
and retry attempts. Diagnostics are written to stderr and never include
passwords, access tokens, refresh tokens, or client secrets:

```sh
ocis --verbose --profile work ls /
```

## Search

Search names in the current Space (or implicit personal files) by substring:

```sh
ocis search report
ocis search "quarterly report" --type file
ocis search budget --path /Finance --min-size 1MB
ocis --space Engineering search design --media-type pdf
```

Search every Space accessible to the authenticated user:

```sh
ocis search report --all-spaces
ocis --json search report --all-spaces
ocis --jsonl search report --all-spaces
```

`--all-spaces` cannot be combined with the global `--space` flag or `--path`.
The server applies the authenticated user's permissions, so inaccessible
Spaces and resources are not returned. The CLI does not require permission to
create or administer Spaces.

Filter by indexed metadata:

```sh
ocis search report --type file --media-type document
ocis search photo --media-type image --modified-after 2026-01-01
ocis search archive --min-size 10MiB --max-size 1GiB
```

Media types accept oCIS categories such as `file`, `folder`, `document`,
`spreadsheet`, `presentation`, `pdf`, `image`, `video`, `audio`, and
`archive`, or a MIME type such as `application/pdf`.

Plain queries are escaped and translated to a case-insensitive name substring
search. Use `--raw` for an advanced oCIS KQL expression:

```sh
ocis search 'name:*report* AND tag:approved' --raw
```

Use `--content` to search indexed file contents instead of names:

```sh
ocis search "revenue forecast" --content
```

Content search only works when the target deployment enables a content
extractor such as Apache Tika. Search results can lag briefly behind writes
because oCIS updates its search index asynchronously. `--limit` defaults to
100 and accepts values up to 1000; the server reports the total match count,
but its WebDAV search REPORT does not currently expose reliable offset-based
pagination.

## Sharing

Share a file or folder directly with a user or group:

```sh
ocis share roles /reports/report.pdf
ocis share user add /reports/report.pdf alice --role viewer
ocis share group add /projects developers --role editor
ocis share list /reports/report.pdf
ocis share overview
ocis share received
ocis share received --state pending
ocis share accept SHARE_ID --dry-run
ocis share accept SHARE_ID
ocis share decline SHARE_ID
```

Recipient names are resolved through the server directory and ambiguous
matches fail closed. When a trusted automation already has the exact opaque
Graph identity ID, bypass search with `--recipient-id`:

```sh
ocis share user add /reports/report.pdf USER_ID \
  --recipient-id \
  --role ROLE_ID
```

Roles are read from the server for the selected resource because available
roles can differ between files and folders and between deployments.
`viewer`, `editor`, `uploader`, and `manager` are convenience aliases only
when they identify one unambiguous advertised role. Use `share roles` and pass
the exact role name or ID when necessary.

Change or remove an outgoing share using the opaque share ID printed by
`share list`:

```sh
ocis share update SHARE_ID --role editor --dry-run
ocis share update SHARE_ID --role editor
ocis share remove SHARE_ID --dry-run
ocis share remove SHARE_ID
```

Removal prompts for confirmation and accepts `--yes` only for reviewed
automation. `share list` includes user, group, federated, and public-link shares
created by the caller. `share received [REMOTE_PATH]` lists incoming user,
group, and federated shares and is not filtered by `--space`. Filter it with `--state accepted`,
`--state pending`, `--state declined`, or `--state all`. Human output names the
state; JSON and JSONL include both the numeric OCS `state` and readable
`stateName`.

Use `share overview` for one account-wide inventory of outgoing and received
shares without changing the profile's selected Space:

```sh
ocis share overview
ocis share overview --direction outgoing
ocis share overview --direction received --state pending
ocis share overview --state all
ocis --space Engineering share overview
ocis share overview --json
```

The default overview contains every outgoing share and received shares in the
`accepted` or `pending` state. Declined invitations are not current and appear
only with `--state declined` or `--state all`. An explicit `--state accepted`,
`pending`, or `declined` selects received shares only. `--space` filters the
inventory by a visible Space; selecting the virtual `Shares` drive includes
received shares. If an incoming personal share's source Space is not visible
through the recipient's drive inventory, its human-readable Space is `Shares`
and machine output retains the source `spaceId`.

Use the opaque received-share ID to accept or decline an invitation. Both
commands resolve the ID against shares received by the authenticated account
before changing it and support `--dry-run`. A declined share can be accepted
again when the server permits it:

```sh
ocis share accept SHARE_ID
ocis share decline SHARE_ID --dry-run
ocis share decline SHARE_ID
ocis share accept SHARE_ID
```

Direct sharing honors the profile's default Space and the global `--space`
override for resource-addressed commands:

```sh
ocis --space Engineering share roles /reports/report.pdf
ocis --space Engineering share group add /reports/report.pdf developers \
  --role editor
```

Create and revoke public links with the original commands:

```sh
ocis share create /demo
ocis share create /reports/report.pdf \
  --name "Quarterly report" \
  --expire 2026-12-31 \
  --permissions read
ocis share link list
ocis share link list /reports
ocis share revoke SHARE_ID
```

The same commands are also grouped under `share link` for explicit scripts:

```sh
ocis share link create /reports/report.pdf --permissions read
ocis share link info SHARE_ID
ocis share link update SHARE_ID --name "Published report"
ocis share link update SHARE_ID --permissions edit --password
ocis share link update SHARE_ID --expire 2026-12-31
ocis share link update SHARE_ID --remove-expiration
ocis share link update SHARE_ID --remove-password
ocis share link revoke SHARE_ID
```

Use `--space` or the profile's default Space to manage links for a resource in
that Space:

```sh
ocis --space Engineering share create /reports/report.pdf
```

Supported public-link permission presets are `read`, `upload`, and `edit`.
`share link update` changes only explicitly selected properties. Use
`--name ""` to clear the display name, `--remove-expiration` to clear an
expiration, and `--remove-password` to remove password protection. Link IDs are
global share identifiers, so `share link info` and `share link update` do not
accept `--space`.

When `--password` is specified for link creation or update, the password is read from
`OCIS_SHARE_PASSWORD` or requested through a secure terminal prompt. It is
never stored in the config or OS credential service and is not accepted as a
command-line value. `--dry-run` is available for creation, update, and
revocation. Dry-run output reports that a password would be set but never reads
or prints the secret.

### Federated Open Cloud Mesh sharing

Federated sharing connects users on two different OCM-enabled oCIS servers.
It has two explicit stages: establish a connection once, then share resources
with that accepted remote user. The CLI never accepts an invitation or a
resource share automatically.

On the invitation issuer's server, create an invitation:

```sh
ocis --profile work federation invite create \
  --email bob@remote.example \
  --description "Share project documents"
ocis --profile work federation invite list
```

Send the returned token to the other user through a trusted channel. On the
recipient's server, accept it while naming the issuer's public host. A full
`http` or `https` URL is also accepted; paths, queries, credentials, and other
URL schemes are rejected:

```sh
ocis --profile remote federation invite accept INVITATION_TOKEN \
  --provider cloud.example.com
```

To avoid placing the invitation token in shell history, omit the positional
token and enter it at the secure prompt, or set
`OCIS_FEDERATION_INVITE_TOKEN` for non-interactive execution:

```sh
ocis --profile remote federation invite accept \
  --provider cloud.example.com
```

After acceptance, both users can discover the connection and share files or
folders using the server-advertised federated roles:

```sh
ocis federation connection list
ocis --space Engineering share federated roles /reports/report.pdf
ocis --space Engineering share federated add \
  /reports/report.pdf bob@remote.example --role viewer --dry-run
ocis --space Engineering share federated add \
  /reports/report.pdf bob@remote.example --role viewer
```

Incoming OCM resource shares appear in the existing intentional workflow:

```sh
ocis share received --state pending
ocis share accept SHARE_ID --dry-run
ocis share accept SHARE_ID
```

Remove a federated connection only after reviewing it. Removal can make
resources shared through that connection unavailable:

```sh
ocis federation connection remove bob@remote.example --dry-run
ocis federation connection remove bob@remote.example
```

Both servers must enable incoming and outgoing OCM support. The CLI reads the
server's federation capabilities and returns a conflict error before mutation
when the required direction is disabled. Invitations establish a connection;
they do not themselves grant access to any file or Space. Federated users
cannot be added as project Space members in current oCIS, so share a file or
folder inside the Space instead.

The server remains authoritative for directory visibility, available roles,
sharing restrictions, and resource permissions. A user may be able to read a
file without being allowed to share it, update a share, or remove another
user's permission.

## Machine-readable output

`--json` writes one indented result and `--jsonl` writes one compact record per
collection item. Both use a stable, versioned envelope:

```json
{
  "schemaVersion": "1",
  "type": "item",
  "data": {
    "name": "report.pdf",
    "path": "/reports/report.pdf",
    "type": "file",
    "size": 1234
  }
}
```

New optional fields may be added without changing `schemaVersion`; removing or
renaming fields requires a schema-version change.

Errors use the same envelope on stderr and include the stable exit code,
classification, message, and operation:

```json
{
  "schemaVersion": "1",
  "type": "error",
  "data": {
    "code": 4,
    "kind": "not_found",
    "message": "stat: 404 Not Found",
    "operation": "stat"
  }
}
```

## Exit codes

| Code | Meaning |
| ---: | --- |
| `0` | Success |
| `1` | General or network failure |
| `2` | Invalid command arguments or flags |
| `3` | Authentication or authorization failure |
| `4` | Remote resource not found |
| `5` | Destination conflict or failed precondition |
| `130` | Operation cancelled by Ctrl-C or process interruption |

The executable installs a signal-aware root context. Ctrl-C cancels OIDC login,
HTTP requests, search, and transfer workers; loopback listeners and open bodies
are closed. An interrupted resumable download retains its `.part` file, and the
entity validator that produced it, for the next invocation.

## OIDC clients

By default, the CLI requests the namespaced public client ID
`github.com/mzner/ocis-cli`. The target deployment must register it as a native
public client with the loopback redirect `http://127.0.0.1`, or provide another
client ID:

```sh
ocis server add production https://cloud.example.com
ocis auth setup production
ocis auth login production
```

For an administrator-provisioned or external-IDP client, skip `auth setup` and
provide its client ID explicitly:

```sh
ocis server add production https://cloud.example.com \
  --client-id my-ocis-cli
ocis auth login production
```

If that client is confidential, provide its secret only while adding/logging in:

```sh
OCIS_CLIENT_SECRET='secret' ocis server add production \
  https://cloud.example.com --client-id my-ocis-cli
```

`auth setup` configures the CLI client and clears any current authentication
and account-bound Space selection for that profile, so the next step is a fresh
browser login.

### Register `github.com/mzner/ocis-cli` in the full example deployment

The embedded IDP only accepts known OIDC applications. The full example already
registers its bundled clients, but it does not register
`github.com/mzner/ocis-cli`. Register the CLI once on the server while keeping
the existing clients.

First run:

```sh
ocis auth setup PROFILE
```

The stock embedded IDP does not advertise dynamic registration, so this prints
the exact entry below. The following administrator steps add that entry to the
full example deployment.

Run the following commands from the server's
`deployments/examples/ocis_full` directory. First confirm that a custom IDP
configuration does not already exist:

```sh
docker compose exec -T ocis sh -c \
  'if [ -f /etc/ocis/idp.yaml ]; then echo EXISTS; else echo ABSENT; fi'
```

If the command prints `EXISTS`, do not overwrite the file. Add the client entry
below to its existing `clients` list. For the standard deployment, which prints
`ABSENT`, copy the effective built-in client list and create a backup:

```sh
docker compose exec -T ocis cp \
  /var/lib/ocis/idp/tmp/identifier-registration.yaml \
  /etc/ocis/idp.yaml
docker compose exec -T ocis cp \
  /etc/ocis/idp.yaml \
  /etc/ocis/idp.yaml.backup
```

Append the CLI registration:

```sh
docker compose exec -T ocis sh -c \
  'cat >> /etc/ocis/idp.yaml' <<'YAML'
- id: github.com/mzner/ocis-cli
  name: oCIS CLI
  trusted: false
  secret: ""
  redirect_uris:
  - http://127.0.0.1
  origins: []
  application_type: native
YAML
```

Here, `docker compose exec` runs a command inside the oCIS container,
`cat >>` appends standard input to its IDP configuration, and the `YAML`
marker passes the block between the markers as that input. The empty secret is
intentional: `github.com/mzner/ocis-cli` is a native public client and protects
the authorization flow with PKCE.

Restart oCIS so the IDP loads the registration:

```sh
docker compose restart ocis
docker compose logs --tail=100 ocis
```

Select the newly registered client for the first login:

```sh
unset OCIS_CLIENT_SECRET
ocis auth login PROFILE
```

`auth setup` already saves the client ID in the profile, so subsequent logins
only need `ocis auth login PROFILE`. Do not append the registration more than
once.

Some external identity providers advertise a `registration_endpoint`. In that
case `ocis auth setup PROFILE` registers a native, untrusted client
automatically and does not print its client secret. Enabling public dynamic
client registration on a server allows arbitrary parties to create client
records and can increase phishing, registration-spam, and endpoint-abuse risk.
Enable it only when the deployment's identity-provider policy and monitoring
make that acceptable; static administrator registration is the safer default.

Use `--no-browser` to print the authorization URL without launching it.
For CI, `OCIS_ACCESS_TOKEN` overrides the stored access token.

## Configuration

The config is stored at the operating system's user config location under
`ocis-cli/config.json`. Set `OCIS_CONFIG` to choose another file.

Inspect the effective local paths without contacting a server or opening the
credential service:

```sh
ocis config path
ocis config paths
ocis --json config paths
```

`config path` prints only the active `config.json` path, which makes it suitable
for shell scripts. `config paths` also reports the named-job file, sync-state
and sync-recovery directories, their effective environment sources, and the operating-system
credential backend. It distinguishes defaults from `OCIS_CONFIG`,
`OCIS_SYNC_JOBS`, `OCIS_STATE_DIR`, and `OCIS_SYNC_RECOVERY_DIR` overrides.

Show the effective non-secret profile configuration:

```sh
ocis config show
ocis --profile work config show
ocis --json config show
```

`config show` uses an explicit allowlist of non-secret fields. It never reads
the credential service and never prints passwords, client secrets, access
tokens, refresh tokens, or protected resumable-upload URLs. `--profile`
restricts the output to one locally configured profile.

Non-secret profile settings are stored in a mode-`0600` config file. Passwords,
OAuth tokens, client secrets, and resumable-upload locations that can contain
transfer tokens are stored separately in macOS Keychain, Linux Secret Service,
or Windows Credential Manager.

Named sync jobs use the separate owner-only `sync-jobs.json` described above;
they contain only non-secret configuration and hashed account bindings.
Interrupted-run journals use the separate owner-only sync-recovery directory
and likewise contain no authentication material.

The non-secret settings include the OIDC issuer and subject and an opaque
account fingerprint for account-bound state such as the selected Space. The
fingerprint is not an authentication credential.

On Linux, a Secret Service provider such as GNOME Keyring or KDE Wallet must
be available and unlocked. If the operating-system credential service is
unavailable or locked, the CLI exits with an actionable error instead of
falling back to plaintext secret storage.

## Security

Do not include credentials or private server URLs in issues. See
[SECURITY.md](SECURITY.md) for private vulnerability reporting.

## Contributing

Contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[Apache License 2.0](LICENSE)
