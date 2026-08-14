# Contributing

## Development

Requirements: Go 1.26.5 or newer.

```sh
git clone https://github.com/mzner/ocis-cli.git
cd ocis-cli
go mod download
make check
```

Changes to authentication, protocols, machine output, permissions, or
cross-package workflows must also pass the black-box compatibility suite:

```sh
make integration
```

The suite requires Docker and a usable OS credential service. See
[test/integration/README.md](test/integration/README.md) for Linux setup and
debugging targets.

## Design rules

- Keep Cobra code in `internal/command` thin.
- Put small cross-domain orchestration in the `internal/app` facade. Put large
  domain policy in `internal/app/<domain>` behind narrow injected ports; domain
  packages must not import the parent `internal/app` package. Current domain
  boundaries are `admin`, `archive`, `filesystem`, `share`, `spaces`, and
  `sync`.
- Keep authentication and WebDAV protocol details out of commands.
- Pass contexts, dependencies, and output streams explicitly.
- Add tests at the narrowest package boundary.
- Keep fast unit tests independent of Docker; put compiled-CLI compatibility
  workflows in the opt-in `test/integration` suite.
- Format with `gofmt`.
- Run `make check`, including per-package coverage gates, golangci-lint v2.12.2,
  and `gosec`.
- Keep the complete `app/...` tree, plus `auth`, `graph`, `httpapi`, `sharing`,
  `transfer`, and `webdav`, at or above 75% statement coverage.
- Every `//nolint` directive must name the linter and explain why suppression
  is safe using a second `//`, for example `//nolint:gosec // reason`.
- Write lowercase, contextual errors without trailing punctuation.

## Pull requests

Keep changes focused, add tests for new behavior, update user documentation,
and use conventional commit prefixes such as `feat:`, `fix:`, `docs:`,
`refactor:`, and `test:`.

## Maintainer releases

Releases use semantic version tags such as `v1.0.0` and must point to a commit
already contained in `main`. The tag workflow builds Linux, macOS, and Windows
archives, executes the packaged binary on all three operating systems, creates
SPDX JSON SBOMs and SHA-256 checksums, records GitHub provenance attestations,
publishes those exact tested artifacts in the GitHub release, and updates
Homebrew and Scoop metadata from the same release candidate. GitHub generates
the release notes automatically; the repository does not maintain a separate
changelog file.

Before creating a tag:

1. Confirm the working tree is clean and the intended commit is on `main`.
2. Run `make check` and `make integration`.
3. Install GoReleaser and Syft, then run `make release-snapshot`.
4. Push `main` and wait for both CI and oCIS integration workflows to pass.

The first release is `v1.0.0`. Later releases must also use a stable semantic
version with a major version of at least 1. Configure the repository secret
`TAP_GITHUB_TOKEN` with a fine-grained token that has Contents read/write access
only to `mzner/homebrew-tap`; GitHub's default workflow token cannot write to a
different repository.

Run the guarded release command:

```sh
make release VERSION=1.0.0
```

It repeats the release snapshot and vulnerability checks, runs the full pinned
oCIS integration suite, verifies that the signed tag points to the pushed
`main` commit, and pushes the tag. The tag workflow then publishes the release.
Inspect the generated release notes, archives, checksums, SBOMs, attestations,
Homebrew formula, and Scoop manifest after it succeeds. Do not move or reuse a
published version tag.
