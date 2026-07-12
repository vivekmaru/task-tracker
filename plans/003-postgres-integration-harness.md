# Plan 003: Add a real PostgreSQL integration harness

> **Executor instructions**: Keep ordinary unit tests fast. Integration tests must be deterministic, isolated, and explicit about their PostgreSQL dependency.
>
> **Drift check**: `git diff --stat 1601f86..HEAD -- internal/db internal/cli/migrate.go internal/integration internal/testsupport scripts .github README.md`

## Status

- **Priority**: P0
- **Effort**: M
- **Risk**: LOW
- **Depends on**: Plan 001
- **Category**: tests
- **Planned at**: commit `1601f86`, 2026-07-12
- **Beads**: `agent-task-tracker-vds.3`

## Why this matters

Forge's defining promise—atomic claim, lease, retry, event, and idempotency correctness—lives in PostgreSQL. Current database tests only search SQL source text, so invalid migrations, constraints, transaction behavior, and concurrency races can ship with a green suite.

## Current state

- `internal/db/phase_one_correctness_test.go` and `internal/db/claims_test.go` use `strings.Contains` assertions.
- `internal/db` reports 0% statement coverage.
- `internal/cli/migrate.go:88-171` contains the real migration runner and is callable from tests.
- CI created by Plan 001 has no PostgreSQL service yet.

## Commands

| Purpose | Command | Expected |
|---|---|---|
| Integration | `FORGE_TEST_DATABASE_URL=postgres://... rtk go test -tags=integration ./internal/integration` | isolated database tests pass |
| Unit suite | `rtk go test ./...` | remains fast and passes without PostgreSQL |
| Full CI gate | `rtk ./scripts/verify.sh` | includes integration when test URL is present |

## Scope

**In scope**: `internal/integration/`, a reusable test database helper under `internal/testsupport/`, CI PostgreSQL service configuration, verification script integration, and test documentation.

**Out of scope**: changing claim or transition behavior, production Docker images, and fixing bugs uncovered by the harness. File separate Beads bugs for newly discovered behavior.

## Git workflow

- Branch: `feat/production-003-postgres-tests`
- Commit: `Add PostgreSQL integration harness`

## Steps

1. Create a test helper that accepts `FORGE_TEST_DATABASE_URL`, creates a uniquely named disposable database, returns its connection URL, and drops it during cleanup. Quote identifiers safely and refuse non-test-looking database names.
   - **Verify**: a helper self-test proves two parallel test databases do not collide.
2. Add an integration `TestMain` or fixture that applies every migration using the production migration runner, opens the normal runtime, and guarantees cleanup on pass or failure.
   - **Verify**: a fresh-schema smoke test can create a workspace and project through runtime services.
3. Add initial tests for migration-from-zero, concurrent `claim-next`, one-running-attempt enforcement, idempotency replay, and a basic terminal transition. Use goroutine barriers, not sleeps.
   - **Verify**: repeat the concurrency subset at least 20 times with `-count=20`.
4. Extend CI and `scripts/verify.sh` so CI always runs integration tests while local runs require an explicit test URL. Missing CI configuration must fail, not silently skip.
   - **Verify**: local unit tests pass without PostgreSQL; the integration command fails clearly when explicitly invoked without configuration.
5. Document local prerequisites and the destructive safety boundary.

## Test plan

- Fresh migration and repeated migration no-op.
- Two simultaneous claimers for one ticket: one success, one no-work result.
- Stable idempotency key replay returns the original attempt.
- Constraint violations roll back their CTE.
- Cleanup occurs after a deliberately failing test fixture.

## Done criteria

- [ ] Integration tests execute real generated queries against PostgreSQL.
- [ ] Concurrency tests contain no arbitrary sleeps.
- [ ] CI runs the integration suite on every PR.
- [ ] Unit-only developer feedback remains available.
- [ ] Existing SQL-shape tests are clearly described as supplementary.

## STOP conditions

- The available CI cannot run PostgreSQL.
- Database creation requires superuser privileges unavailable in the supported setup; switch to unique schemas only after documenting how migration `current_schema()` behavior remains correct.
- The harness exposes or logs credentials.

## Maintenance notes

Plans 002, 004-010, and 012 must add their database regressions to this suite rather than creating separate ad hoc harnesses.

