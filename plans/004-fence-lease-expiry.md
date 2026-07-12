# Plan 004: Fence lease expiry against renewed attempts

> **Executor instructions**: Preserve the documented lease and retry policy. Fix the stale-worker race without inventing a new grace-period policy.
>
> **Drift check**: `git diff --stat 1601f86..HEAD -- sql/queries/attempts.sql sql/queries/transitions.sql internal/jobs/maintenance.go internal/jobs/maintenance_test.go internal/integration internal/db`

## Status

- **Priority**: P0
- **Effort**: M
- **Risk**: MED
- **Depends on**: Plan 003
- **Category**: bug, correctness
- **Planned at**: commit `1601f86`, 2026-07-12
- **Beads**: `agent-task-tracker-vds.4`

## Why this matters

Maintenance first lists expired attempts and then expires each by ID. A heartbeat can renew the lease between those statements, but the expiration update checks only `status='running'`, so a stale worker can expire active work and requeue its ticket.

## Current state

- `sql/queries/attempts.sql:41-47` selects expired running attempts without locking ownership.
- `internal/jobs/maintenance.go:60-79` later expires the selected IDs one at a time.
- `sql/queries/transitions.sql:238-246` checks status but not the current lease deadline.
- Transition CTEs already atomically update attempt, ticket, and event; preserve that pattern.

## Commands

| Purpose | Command | Expected |
|---|---|---|
| Focused unit | `rtk go test ./internal/jobs ./internal/services` | pass |
| Integration race | `rtk go test -tags=integration ./internal/integration -run 'TestLeaseExpiry|TestHeartbeatExpiryRace' -count=20` | pass every run |
| Full gate | `rtk ./scripts/verify.sh` | exit 0 |

## Scope

**In scope**: expiry query parameters/guards, maintenance handling of lost expiry races, generated sqlc output, and concurrency regression tests.

**Out of scope**: changing lease duration defaults, heartbeat frequency, worker scheduling, or retry-policy meaning.

## Git workflow

- Branch: `feat/production-004-lease-fencing`
- Commit: `Fence attempt lease expiry`

## Steps

1. Change `ExpireAttempt` so the update succeeds only when the attempt is still running and its current `lease_expires_at` is earlier than the worker's expiration cutoff. Pass the cutoff explicitly; do not call database `now()` in one part and application time in another.
   - **Verify**: regenerated query parameters include the cutoff.
2. Treat a zero-row expiration as lost eligibility rather than a fatal maintenance pass. Distinguish `pgx.ErrNoRows` from real database errors and report skipped-race count if operational output needs it.
   - **Verify**: unit test covers renewed lease, already terminal attempt, and actual database error.
3. Add a deterministic PostgreSQL race test: list expired work, heartbeat it to a later lease, then execute the stale expiration and assert it remains running with no expiry event.
4. Add the control case where no heartbeat occurs and assert one expiry event plus the correct ticket retry state.
   - **Verify**: focused integration command passes repeatedly.

## Done criteria

- [ ] A renewed lease cannot be expired by a stale maintenance selection.
- [ ] True expiry remains atomic across attempt, ticket, and event.
- [ ] Maintenance continues after losing an eligibility race.
- [ ] No sleep-based test synchronization was introduced.

## STOP conditions

- The intended product rule is changed to forbid all late heartbeats; that is a separate policy decision requiring broader terminal-operation fencing.
- sqlc regeneration changes unrelated query APIs.
- A repeated concurrency test flakes once.

## Maintenance notes

The same ownership-fencing concept applies to webhook deliveries in Plan 008, but the implementations should remain domain-specific.

