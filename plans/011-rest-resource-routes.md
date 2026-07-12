# Plan 011: Implement typed REST resource routes

> **Executor instructions**: Replace placeholders only for the resource operations listed in scope. Use shared runtime services and the Plan 007 authentication middleware; do not duplicate domain rules in handlers.
>
> **Drift check**: `git diff --stat 1601f86..HEAD -- internal/api internal/contracts internal/runtime internal/services internal/integration docs/phase-2-closeout.md README.md`

## Status

- **Priority**: P0
- **Effort**: L
- **Risk**: MED
- **Depends on**: Plans 007 and 009
- **Category**: feature, api
- **Planned at**: commit `1601f86`, 2026-07-12
- **Beads**: `agent-task-tracker-vds.11`

## Why this matters

OpenAPI advertises ticket and artifact operations whose shared handler always returns 501. A production client must receive typed schemas, stable validation/errors, authentication, and real runtime behavior.

## Current state

- `internal/api/router.go:50-97` registers most core routes through a placeholder.
- `internal/api/router.go:100-110` models mutation bodies as untyped maps.
- `internal/mcp/payloads.go` and CLI request construction provide existing transport-to-service mapping examples.
- `docs/phase-2-closeout.md:39` acknowledges that REST parity currently proves metadata only.

## Commands

| Purpose | Command | Expected |
|---|---|---|
| API unit | `rtk go test ./internal/api ./internal/contracts` | pass |
| REST integration | `rtk go test -tags=integration ./internal/integration -run 'TestRESTResource'` | pass |
| OpenAPI check | focused API test | no scoped operation uses placeholder handler |
| Full gate | `rtk ./scripts/verify.sh` | exit 0 |

## Scope

**In scope**: typed handlers for create/propose/list/get/update/decompose ticket and register/get/delete artifact metadata; DTO mapping, error mapping, OpenAPI tests, auth tests, integration tests, docs.

**Out of scope**: claim/attempt lifecycle (Plan 012), MCP transport, multi-tenant authorization, artifact file upload, or response-shape redesign outside existing contracts.

## Git workflow

- Branch: `feat/production-011-rest-resources`
- Commit: `Implement REST resource routes`

## Steps

1. Define explicit Huma input/output DTOs with documented field types, bounds, required fields, and JSON names. Reuse contract vocabulary but keep transport types in `internal/api` unless an existing shared type is truly transport-neutral.
2. Implement mapping functions to service requests. Normalize only at the service layer; handlers parse identity, pagination, and optional fields without inventing defaults that conflict with CLI/MCP.
3. Wire each scoped operation to the authenticated runtime. Remove its placeholder registration once real.
4. Map validation to 400, missing resource to 404, transition/conflict conditions to 409, authentication to 401/403, and unexpected errors to an opaque 500 with request ID. Never return database or filesystem error text.
5. Add table-driven handler tests and PostgreSQL process tests for happy path, invalid UUID/body, missing scope, duplicate resource, unauthorized request, and runtime failure.
6. Regenerate/check OpenAPI and update the parity document so `yes` means executable, not merely registered.

## Done criteria

- [ ] Every in-scope route is typed and runtime-backed.
- [ ] No in-scope route returns the generic placeholder 501.
- [ ] All routes require Plan 007 authentication.
- [ ] OpenAPI schemas match actual success and error bodies.
- [ ] PostgreSQL integration tests cover one complete resource workflow.

## STOP conditions

- Existing contracts cannot represent a required REST field without a compatibility decision.
- A handler needs direct `db.Queries` access rather than runtime/service methods.
- Error mapping would expose internal error strings.

## Maintenance notes

Plan 012 should use the same registration helpers, error envelope, authentication, pagination, and integration-test client.

