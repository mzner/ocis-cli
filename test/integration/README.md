# oCIS integration tests

This directory contains opt-in black-box tests for the compiled `ocis` binary.
The suite starts a disposable full oCIS server, uses the operating system's
credential service, and exercises the public CLI contract. It does not import
application packages.

## Coverage

The suite verifies:

- dynamic OIDC client registration, embedded-IDP login, PKCE, and refresh;
- Basic authentication with a normal demo user;
- administrative user and group CRUD, direct membership, role grant/revoke,
  server-visible Space inventory, and restricted-user mutation denials;
- personal files, raw `cat` streaming, bounded tree traversal, logical `du`,
  validated sequential JSONL batches, recursive transfers, one-way push/pull synchronization,
  bidirectional planning/execution, rename optimization, conflict detection and
  keep-both preservation, recovery reporting, named one-way/bidirectional
  sync-job binding/execution/removal, local sync-state inspection/export/removal,
  versions, trash, shares, and search;
- Space discovery, selection, lifecycle, membership, and restricted-user
  authorization failures;
- JSON and JSONL envelopes and stable exit codes; and
- absence of secrets in the normal config file.

The normal user is deliberately not granted Space-administration permission.
An authorization failure from that account is a required test result.
Dynamic clients expire after one hour in the disposable server; the suite
never enables unbounded registration credentials.

## Run locally

Requirements:

- Docker with Compose;
- Go 1.26.5 or newer; and
- a usable OS credential service.

Run the pinned supported suite:

```sh
make integration
```

`make integration` always tears down its version-scoped containers and named
volumes. It never uses the moving `latest` image tag.

If port 9200 is already in use, select the same unused port for the host and
the disposable server URL:

```sh
make integration \
  OCIS_INTEGRATION_PORT=19200 \
  OCIS_INTEGRATION_SERVER=https://localhost:19200
```

On a headless Linux machine, install `gnome-keyring` and run the suite in a
temporary D-Bus session:

```sh
dbus-run-session -- bash -c \
  'echo disposable-test-keyring | gnome-keyring-daemon --unlock; make integration'
```

For debugging, manage the lifecycle explicitly:

```sh
make integration-up OCIS_INTEGRATION_VERSION=8.1.0
make integration-test
make integration-logs OCIS_INTEGRATION_VERSION=8.1.0
make integration-down OCIS_INTEGRATION_VERSION=8.1.0
```

`integration-logs` passes container output through the repository sanitizer.
It redacts authorization headers, cookies, OAuth tokens, client secrets,
passwords, authorization codes, PKCE material, and OAuth state. Do not replace
this target with raw `docker compose logs` in CI artifacts.

## Configuration

The harness supports these environment variables:

| Variable | Default | Purpose |
| --- | --- | --- |
| `OCIS_INTEGRATION_VERSION` | `8.1.0` | Exact container image tag |
| `OCIS_INTEGRATION_SERVER` | `https://localhost:9200` | External server URL |
| `OCIS_INTEGRATION_BINARY` | `bin/ocis` | Compiled CLI under test |
| `OCIS_INTEGRATION_INSECURE` | `true` | Trust the disposable self-signed server |
| `OCIS_INTEGRATION_ADMIN_USERNAME` | `admin` | Disposable administrator fixture |
| `OCIS_INTEGRATION_ADMIN_PASSWORD` | `admin` | Disposable administrator password |
| `OCIS_INTEGRATION_RESTRICTED_USERNAME` | `einstein` | Normal demo-user fixture |
| `OCIS_INTEGRATION_RESTRICTED_PASSWORD` | `relativity` | Normal demo-user password |
| `OCIS_INTEGRATION_COMMAND_TIMEOUT` | `2m` | Per-command timeout |

The default credentials belong only to the disposable local container. CI
must use disposable fixtures or CI secrets for any other deployment.

Fast unit CI merely compiles this package and skips the black-box test.
`OCIS_INTEGRATION=1` is set only by `make integration-test`.
