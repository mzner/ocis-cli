# oCIS CLI command catalog

Use this catalog to explain capabilities and select a command. Use live command
help as the source of truth for the installed CLI version:

```text
ocis --help
ocis COMMAND --help
```

Aliases are shown together. For example, `ls, list` means both names run the
same command.

## Categories

- Connection and authentication
- Files and transfers
- Synchronization
- Spaces
- Sharing
- Federation
- Notifications
- Metadata, trash, and versions
- Administration
- CLI utilities and global flags

## Connection and authentication

| Command | Purpose |
| --- | --- |
| `server add/list/use/remove` | Manage local profiles that point to oCIS servers and select the default profile. |
| `auth setup` | Show or prepare the server-side OIDC client registration needed by this CLI. |
| `auth login` | Authenticate a profile through browser OIDC, optional server-required MFA, or another supported flow. |
| `auth logout` | Remove the locally saved authentication for a profile. |
| `auth status` | Show whether a profile is authenticated. |
| `login`, `logout`, `status` | Top-level shortcuts for the corresponding authentication commands. |
| `doctor` | Validate local configuration and connectivity. |
| `config path/paths/show` | Show non-secret configuration and local storage locations. |

## Files and transfers

| Command | Purpose |
| --- | --- |
| `ls, list` | List a remote directory. |
| `tree` | Display a bounded remote directory tree. |
| `stat` | Show metadata for one remote resource. |
| `cat` | Write a remote file to standard output. |
| `touch` | Create an empty remote file when it does not already exist. |
| `mkdir` | Create a remote directory; use its help to inspect parent-creation support. |
| `cp, copy` | Copy a remote file or directory. |
| `mv, move` | Move or rename a remote resource. |
| `rm, remove` | Move a remote resource to trash unless the selected operation states otherwise. |
| `upload` | Transfer a local file or directory to oCIS. |
| `download` | Transfer a remote file or directory to the local filesystem. |
| `du` | Summarize logical remote file sizes. |
| `search, find` | Search remote files and directories. |
| `batch` | Execute reviewed file operations supplied as JSONL. |
| `filesystem, fs` | Group the file commands under one namespace; the top-level forms are equivalent. |

## Synchronization

| Command | Purpose |
| --- | --- |
| `sync push` | Reconcile a local directory into a remote directory. |
| `sync pull` | Reconcile a remote directory into a local directory. |
| `sync bidirectional, sync bi` | Reconcile changes in both directions using saved state and conflict policies. |
| `sync job add/list/show/run/remove` | Manage reusable named synchronization configurations. |
| `sync state list/show/export/remove` | Inspect or remove saved synchronization baselines. |
| `sync recovery list/show/retry/remove` | Inspect and recover interrupted bidirectional synchronizations. |

Always preview synchronization with `--dry-run` before changing either side.

## Spaces

| Command | Purpose |
| --- | --- |
| `space list, space ls` | List Spaces available to the authenticated user. |
| `space info, space stat` | Show Space metadata, quota, members, and permissions. |
| `space current` | Show the profile's saved default Space selection. |
| `space use` | Save a Space as the profile's default. |
| `space unset, space clear` | Return to the implicit personal-file root. |
| `space create/update` | Create a project Space or update its metadata when permitted. |
| `space member add/list/update/remove` | Manage project Space membership and roles when permitted. |
| `space disable/restore/delete` | Disable, restore, or permanently delete a project Space when permitted. |

Prefer the temporary global `--space SPACE` override when the user does not ask
to change their saved default.

## Sharing

| Command | Purpose |
| --- | --- |
| `share roles` | List server-advertised sharing roles for a resource. |
| `share user add` | Grant a user access to a remote resource. |
| `share group add` | Grant a group access to a remote resource. |
| `share federated add/roles` | Grant an accepted OCM user access using a server-advertised federated role. |
| `share list, share ls` | List outgoing shares for a resource. |
| `share overview` | List outgoing and received shares across Spaces. |
| `share received` | List shares received by the current user. |
| `share accept/decline` | Intentionally accept or decline a received share. |
| `share update` | Change a direct share's role. |
| `share remove, share rm` | Remove a direct share or public link. |
| `share create` | Create a public link. |
| `share link create/info/list/update/revoke` | Manage public links explicitly. |
| `share revoke` | Revoke a public link. |

Never accept a received share without an explicit user request.

## Federation

| Command | Purpose |
| --- | --- |
| `federation invite create/list/accept` | Establish an OCM connection between users on two federation-enabled servers. |
| `federation connection list/remove` | Inspect or remove accepted remote-user connections. |

An invitation token establishes identity trust; it does not share a resource.
Treat invitation tokens as secrets and never accept one without an explicit
user request.

## Notifications

| Command | Purpose |
| --- | --- |
| `notification list, notification ls` | List unread in-app notifications; optionally filter them with a search argument. |
| `notification info` | Inspect one unread notification by its opaque ID. |
| `notification dismiss, notification read` | Remove one or more notifications from the unread list. |
| `notification clear, notification read-all` | Remove every notification from the unread list after confirmation. |

In oCIS, dismissing is the server's mark-as-read operation. It does not delete
the resource mentioned by the notification. Preview dismissal or clearing with
`--dry-run`; never clear all notifications without an explicit user request.

## Metadata, trash, and versions

| Command | Purpose |
| --- | --- |
| `tag add/list/remove` | Manage tags on a remote resource. |
| `favorite set/unset` | Mark or unmark a remote resource as a favorite. |
| `property get/set/remove` | Manage scalar custom WebDAV properties. |
| `trash list/restore/remove/empty` | Inspect, restore, or permanently remove deleted resources. |
| `version list/info/download/restore` | Inspect or restore historical versions of a file. |

`ocis --version` prints the CLI version. `ocis version` manages historical file
versions; these are different operations.

## Administration

Administrative commands require suitable server permissions and may require
MFA. Never assume the authenticated user is an administrator.

| Command | Purpose |
| --- | --- |
| `admin user list/info/create/update/enable/disable/delete` | Manage server user accounts. |
| `admin user role` | Inspect and manage server-advertised user roles. |
| `admin group list/info/create/update/delete` | Manage server groups. |
| `admin group member` | Inspect and change direct group membership. |
| `admin space list/info/create/update` | Inspect or manage Spaces through the server-wide drives endpoint. |
| `admin space member` | Manage project Space membership and roles as an administrator. |
| `admin space disable/restore/delete` | Control the lifecycle of server-visible project Spaces. |

## CLI utilities and global flags

| Command or flag | Purpose |
| --- | --- |
| `completion` | Generate shell completion scripts. |
| `--help` | Show commands, arguments, flags, and examples. |
| `--version` | Print the installed CLI version. |
| `--profile PROFILE` | Use one local server/account profile for this invocation. |
| `--space SPACE` | Use one Space by name, alias, or ID for this invocation. |
| `--json`, `--jsonl` | Produce structured output for programs and AI agents. |
| `--quiet`, `--verbose` | Reduce progress output or add diagnostics. |
| `--timeout`, `--retries`, `--concurrency` | Control HTTP timeout, retries, and parallel transfers. |
