---
name: use-ocis-cli
description: Safely operate oCIS servers through the installed ocis command-line client. Use when an AI agent is asked to inspect, list, search, transfer, synchronize, share, restore, manage notifications, or administer files, Spaces, shares, users, or groups in oCIS. Do not use for developing the ocis-cli source code.
---

# Use oCIS CLI

Operate through the installed `ocis` command. Do not bypass the CLI with direct
WebDAV, Graph, OCS, or internal API calls unless the user explicitly requests
protocol-level work.

## Explain available commands

- If the user asks what the CLI can do, invokes this skill without a specific
  task, or appears unsure which command to use, read
  [references/commands.md](references/commands.md) and present a concise command
  overview in plain language.
- If the user has a specific task, explain the selected command and any closely
  relevant alternative instead of dumping the entire catalog.
- Explain aliases such as `ls, list` together. Distinguish `ocis --version`,
  which prints the CLI version, from `ocis version`, which manages historical
  file versions.
- Treat the catalog as orientation. Confirm current syntax and flags with live
  `ocis --help` and `ocis COMMAND --help` output before executing a command.

## Establish context

1. Run `ocis --version` to confirm that the command is installed.
2. Run `ocis status` before an operation that needs server access.
3. Use the current profile by default. Treat `--profile` as the name of a local
   CLI account configuration, not as an oCIS server-side role. Do not guess a
   profile when the user explicitly refers to a different account or server.
4. Run `ocis space current` before a Space-scoped operation. If the user names a
   Space, prefer `--space SPACE` for that operation instead of changing their
   saved default with `ocis space use`.
5. Ask for clarification only when the intended account, Space, local path, or
   remote path cannot be determined safely.

## Discover commands

- Run `ocis --help` and `ocis COMMAND --help` before using an unfamiliar
  command. Never invent a command, flag, accepted value, or positional argument.
- Prefer the global `--json` or `--jsonl` output when consuming results. Do not
  parse human-readable tables when structured output is available.
- Use read-only discovery commands such as `ls`, `stat`, `search`, `tree`,
  `space list`, `share overview`, `federation connection list`, `trash list`,
  `notification list`, and admin `list` or `info` commands to resolve names and
  IDs before changing anything.
- Interpret a remote path in the selected Space. Keep local filesystem paths and
  remote oCIS paths distinct according to the command help.

## Choose the operation

- Use `upload` or `download` for a one-time transfer.
- Use `sync push`, `sync pull`, or `sync bidirectional` for directory trees that
  should be reconciled. Run a sync with `--dry-run` first.
- Use `share received` or `share overview` to inspect shares. Never accept a
  share automatically.
- Use `federation invite` and `federation connection` to establish and inspect
  OCM identity connections. An invitation establishes a connection; sharing a
  file or folder is a separate explicit operation.
- Use `trash` for recoverable deletion management and `version` for historical
  file versions.
- Use `notification list` and `notification info` to inspect unread events.
  In oCIS, `notification dismiss` is the server's mark-as-read operation; it
  does not delete the resource referenced by the notification.
- Use `admin` only when the user explicitly requests administration. A normal
  user may not have permission; report authorization failures without trying to
  bypass them.

## Control mutations

For every command that creates, modifies, overwrites, moves, synchronizes,
shares, disables, or deletes data:

1. Inspect the existing target and resolve identifiers.
2. Run `--dry-run` when the command supports it.
3. Summarize the exact profile, Space, source, destination, and planned effects.
4. Obtain explicit user authorization before an irreversible, destructive,
   broad, or administrative operation.
5. Execute only the authorized operation, then verify it with a read-only
   command.

Never add `--yes` merely to avoid a prompt. Use it only when the user has
explicitly authorized that exact operation. Do not silently overwrite files,
empty trash, permanently delete resources, disable or delete accounts, change
roles, accept federation invitations, remove federation connections, accept or
decline shares, clear all notifications, or execute unreviewed batch input.

## Protect authentication and secrets

- Let `ocis auth login PROFILE` handle interactive browser login and server-
  required MFA. Tell the user when browser interaction is required.
- Never ask the user to paste a password, access token, refresh token, client
  secret, keyring record, federation invitation token, or TUS resume URL into
  the conversation.
- Never inspect, print, export, or copy operating-system keyring contents.
- Never expose credential-bearing environment variables, authorization headers,
  local secret storage, or complete diagnostic output that may contain secrets.
- Do not weaken TLS verification or add `--insecure` unless the user explicitly
  requests it for a server they understand and control.

## Report results

State which command ran, which profile and Space it targeted, what changed, and
how the result was verified. For failures, preserve the useful status and error
message while redacting credentials, tokens, private URLs, and sensitive local
paths.
