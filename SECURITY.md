# Security

## Supported versions

Only the latest released version receives security fixes.

## Reporting a vulnerability

Please do not open a public issue. Use GitHub's private vulnerability reporting
for this repository. Include reproduction steps, affected versions, and the
potential impact. You should receive an acknowledgement within five business
days.

Never include passwords, access tokens, refresh tokens, client secrets, or
private server URLs in a report.

For the complete implemented authentication flow, storage diagrams, platform
keyring mapping, token lifecycle, and exposure boundaries, see
[AUTHENTICATION.md](AUTHENTICATION.md).

## Administrative authentication

The CLI does not decide who is an administrator. oCIS authorizes account,
Space, and membership operations independently. Account and group commands
verify the server's account-management permission and MFA state before acting;
Space commands verify MFA and then rely on the specific Space endpoint's
permission check.

Use `ocis auth login PROFILE --mfa` when the server requires step-up
authentication. The requested OIDC authentication context comes from server
capabilities unless `--acr` is explicitly supplied. Basic authentication
cannot perform OIDC MFA step-up.

Administrative passwords are accepted only through a hidden prompt or
`OCIS_USER_PASSWORD`. Avoid putting the environment assignment in persistent
shell history or CI logs.
