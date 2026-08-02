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
- Put use-case orchestration in `internal/app`.
- Keep authentication and WebDAV protocol details out of commands.
- Pass contexts, dependencies, and output streams explicitly.
- Add tests at the narrowest package boundary.
- Keep fast unit tests independent of Docker; put compiled-CLI compatibility
  workflows in the opt-in `test/integration` suite.
- Format with `gofmt`.
- Run `make check`, including per-package coverage gates, golangci-lint v2.12.2,
  and `gosec`.
- Keep `app`, `auth`, `graph`, `httpapi`, `sharing`, `transfer`, and `webdav`
  at or above 75% statement coverage.
- Every `//nolint` directive must name the linter and explain why suppression
  is safe using a second `//`, for example `//nolint:gosec // reason`.
- Write lowercase, contextual errors without trailing punctuation.

## Pull requests

Keep changes focused, add tests for new behavior, update user documentation,
and use conventional commit prefixes such as `feat:`, `fix:`, `docs:`,
`refactor:`, and `test:`.

Before publishing a tag, follow [RELEASE_CHECKLIST.md](RELEASE_CHECKLIST.md).
