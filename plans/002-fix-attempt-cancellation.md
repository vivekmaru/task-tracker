# Plan 002: Make attempt cancellation valid in PostgreSQL

> **Executor instructions**: Follow the migration conventions exactly. Never edit an already-applied migration to fix this bug.
>
> **Drift check**: `git diff --stat 1601f86..HEAD -- sql/migrations sql/queries/transitions.sql internal/services/attempts.go internal/api/router.go internal/db/migrations_test.go internal/integration`

## Status

- **Priority**: P0
- **Effort**: S
- **Risk**: LOW
- **Depends on**: Plan 001 and Plan 003
- **Category**: bug, migration
- **Planned at**: commit `1601f86`, 2026-07-12
- **Beads**: `agent-task-tracker-vds.2`

## Why this matters

`CancelAttempt` writes a `cancelled` ticket event, but the database check constraint rejects that value. The whole CTE rolls back, so cancellation cannot work on a real database even though service tests pass.

## Current state

- `sql/queries/transitions.sql:186-226` updates the attempt and ticket, then inserts event type `cancelled`.
- `sql/migrations/0002_ticket_transition_event_types.sql:5-7` allows many transition types but omits `cancelled`.
- `internal/services/attempts.go:277-291` exposes cancellation as a normal runtime operation.
- Migration tests currently inspect SQL text rather than exercising the constraint.

## Commands

| Purpose | Command | Expected |
|---|---|---|
| Unit tests | `rtk go test ./internal/services ./internal/db` | pass |
| Integration | `rtk go test -tags=integration ./internal/integration -run TestCancelAttempt` | pass against PostgreSQL |
| Full gate | `rtk ./scripts/verify.sh` | exit 0 |

## Scope

**In scope**: one new forward migration, migration registration/tests, supported event-type catalogs, and one PostgreSQL regression test.

**Out of scope**: cancellation authorization, UI actions, retry-policy changes, or editing migrations `0001` and `0002`.

## Git workflow

- Branch: `feat/production-002-cancel-attempt`
- Commit: `Fix attempt cancellation event type`

## Steps

1. Add the next monotonic migration that replaces `ticket_events_type_check` with the existing values plus `cancelled`. Include a reversible Down section matching repository conventions.
   - **Verify**: the migration applies to a database migrated through the previous version.
2. Add `cancelled` to API/observability event-type catalogs wherever the code enumerates valid ticket events. Do not silently broaden unrelated event filters.
   - **Verify**: `rtk rg -n 'supportedObservabilityEventTypes|cancelled' internal sql` shows consistent support.
3. Add a live PostgreSQL test that creates and claims a ticket, cancels its attempt, and asserts: attempt=`cancelled`, ticket=`todo`, exactly one `cancelled` event, and no running attempt remains.
   - **Verify**: the focused integration command passes.
4. Add migration-shape coverage only as a supplementary check.
   - **Verify**: full gate passes.

## Done criteria

- [ ] No historical migration was modified.
- [ ] Real PostgreSQL cancellation succeeds atomically.
- [ ] Event filtering accepts `cancelled`.
- [ ] Existing terminal-transition tests remain green.

## STOP conditions

- Plan 003 has not provided the integration harness.
- Existing deployed data contains an event type outside both old and proposed constraints.
- Fixing cancellation requires changing its ticket-state semantics.

## Maintenance notes

Any future attempt terminal state must update the schema constraint, event catalog, observability allowlist, and live integration suite together.

