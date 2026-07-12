# Plan 007: Protect the API and harden human authentication

> **Executor instructions**: Implement the trusted single-tenant v0.1 boundary. Do not expand this packet into multi-tenant RBAC or OAuth.
>
> **Drift check**: `git diff --stat 1601f86..HEAD -- internal/api internal/web internal/config internal/cli/init.go internal/cli/cli.go internal/auth README.md`

## Status

- **Priority**: P0
- **Effort**: M
- **Risk**: MED
- **Depends on**: Plan 001
- **Category**: security
- **Planned at**: commit `1601f86`, 2026-07-12
- **Beads**: `agent-task-tracker-vds.7`

## Why this matters

The configured admin token protects only human web routes. Implemented `/api/v1` event, analytics, and webhook routes are reachable without authentication, and `forge init` writes a known development credential by default. Cookie-authenticated mutations also lack a dedicated request-authenticity defense.

## Current state

- `internal/api/router.go:30-47` mounts Huma directly and wraps only the separate web handler with `AuthOptions`.
- `internal/cli/init.go:34-39` supplies a static development admin credential and insecure cookies by default.
- `internal/web/handler.go:152-205` accepts bearer, header, or signed-session-cookie authentication.
- `sql/migrations/0001_initial_schema.sql:189-202` defines API keys, but no production code validates them. Full API-key/RBAC management is intentionally out of scope for v0.1.

## Commands

| Purpose | Command | Expected |
|---|---|---|
| Auth tests | `rtk go test ./internal/api ./internal/web ./internal/config ./internal/cli` | pass |
| Full gate | `rtk ./scripts/verify.sh` | exit 0 |
| Route audit | `rtk rg -n 'Handle\(|huma.Register' internal/api internal/web` | every protected route covered by auth tests |

## Scope

**In scope**: single-tenant admin-token middleware for all API routes, secure init credential generation, production cookie validation, CSRF defense for cookie-authenticated mutations, logout, and tests.

**Out of scope**: workspace roles, per-agent scopes, OAuth/OIDC, public SaaS hosting, password recovery, or API-key CRUD.

## Git workflow

- Branch: `feat/production-007-auth-boundary`
- Commit: `Harden Forge authentication boundary`

## Steps

1. Put the entire `/api/v1/` tree, including OpenAPI unless explicitly documented otherwise, behind constant-time bearer or `X-Forge-Admin-Token` validation. Reuse one authentication primitive rather than copying web logic.
   - **Verify**: table-driven tests prove every API route returns 401 without credentials and reaches its existing handler with valid credentials.
2. Remove the static init credential. When no token is supplied, generate at least 32 random bytes, encode safely, write only to the mode-0600 config, and print the config path rather than the secret.
   - **Verify**: two init runs produce different non-placeholder tokens and stdout/stderr do not contain them.
3. Define fail-closed server validation for non-loopback deployment: reject the former development credential and reject cookie auth without `Secure` unless an explicit local-only mode is active.
   - **Verify**: config tests cover loopback local HTTP, TLS-terminated production, unsafe non-loopback, and malformed settings.
4. Add a request-authenticity mechanism for cookie-authenticated POSTs. Prefer a signed session-bound CSRF token placed in every form and checked before mutation; bearer/header clients are not required to send it.
   - **Verify**: missing/wrong token returns 403; valid token succeeds; cross-session token fails.
5. Add logout that expires both session and CSRF cookies and update security documentation.

## Done criteria

- [ ] No `/api/v1` data or mutation route is anonymous.
- [ ] Init never writes the known development credential by default.
- [ ] Unsafe production cookie configuration fails startup.
- [ ] Cookie-authenticated mutations require valid request authenticity.
- [ ] Secrets are not emitted to logs or test failures.

## STOP conditions

- A machine client depends on anonymous API access; report the exact consumer before adding exceptions.
- The app must support multiple untrusted workspaces now; that requires a separate authorization design.
- CSRF support would require weakening progressive form fallback.

## Maintenance notes

Plans 011-013 must use this middleware. If API keys are implemented later, keep the v0.1 admin-token path as a clearly scoped bootstrap/operator credential.

