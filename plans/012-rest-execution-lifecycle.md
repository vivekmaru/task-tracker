# Plan 012: Implement the REST execution lifecycle

> **Executor instructions**: Complete the agent execution loop through authenticated typed HTTP. Preserve the PostgreSQL transaction and idempotency semantics established by earlier plans.
>
> **Drift check**: `git diff --stat 1601f86..HEAD -- internal/api internal/contracts internal/runtime internal/services internal/integration sql/queries README.md docs`

## Status

- **Priority**: P0
- **Effort**: L
- **Risk**: HIGH
- **Depends on**: Plans 004, 006, and 011
- **Category**: feature, api
- **Planned at**: commit `1601f86`, 2026-07-12
- **Beads**: `agent-task-tracker-vds.12`

## Why this matters

The core product loop—claim, heartbeat, checkpoint, and terminal transition—is advertised in OpenAPI but unavailable over HTTP. This packet makes the backend usable by remote harnesses without routing around domain services.

## Current state

- `internal/api/router.go:65-82` registers claim, attempt, event, and artifact lifecycle routes.
- Most use the placeholder handler at lines 88-97.
- `internal/services/claims.go` owns request hashing and replay.
- `sql/queries/claims.sql` and transition CTEs own atomic state changes; handlers must not split them.

## Commands

| Purpose | Command | Expected |
|---|---|---|
| API unit | `rtk go test ./internal/api ./internal/contracts` | pass |
| Lifecycle integration | `rtk go test -tags=integration ./internal/integration -run 'TestRESTExecution|TestRESTIdempotency' -count=5` | pass |
| Full gate | `rtk ./scripts/verify.sh` | exit 0 |

## Scope

**In scope**: claim-next, get attempt, heartbeat, checkpoint, complete, fail, block, cancel, ticket/attempt event reads, idempotency headers/body mapping, request limits, typed errors, OpenAPI, tests, docs.

**Out of scope**: generic attempt PATCH without defined semantics, streaming/SSE, MCP, agent API-key scopes, A2A, or MCP Tasks.

## Git workflow

- Branch: `feat/production-012-rest-lifecycle`
- Commit: `Implement REST execution lifecycle`

## Steps

1. Define typed DTOs for each operation using the established CLI/MCP payload vocabulary. Remove or omit generic routes whose semantics are not implemented rather than leaving 501 placeholders.
2. Wire claim-next with stable idempotency key support. Accept one canonical header and document body compatibility only if already required. Same key plus same request must replay; same key plus different request must return 409.
3. Wire heartbeat and checkpoint with explicit lease/progress validation and opaque errors.
4. Wire complete/fail/block/cancel through the transactional runtime methods from Plans 004 and 006. Never reimplement transition rules in HTTP handlers.
5. Wire bounded event reads using the existing cursor model.
6. Apply request body size limits and context cancellation. Ensure client disconnect cancels database work without producing partial commits.
7. Add one process-level integration flow: authenticate, create, claim, replay claim, checkpoint, complete with metrics/artifact metadata, read attempt/events, and verify a second terminal request conflicts.

## Done criteria

- [ ] A remote HTTP client can complete the documented day-zero loop.
- [ ] Claim idempotency behavior is preserved exactly.
- [ ] Terminal operations use transactional runtime paths.
- [ ] No undefined generic operation remains advertised as executable.
- [ ] Every error response has stable status/code and no internal detail.

## STOP conditions

- The REST contract conflicts with current CLI/MCP semantics.
- A required operation is not available through the runtime interface.
- A process-level test can create partial state after cancellation or timeout.

## Maintenance notes

This is the reference transport behavior for Plan 013. Future A2A or MCP Tasks adapters should map to the same runtime operations rather than change the state machine.

