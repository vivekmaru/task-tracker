# Plan 005: Make ticket creation and updates all-or-nothing

> **Executor instructions**: Introduce transaction ownership at the production runtime boundary while keeping service interfaces easy to fake. Do not mix decomposition or terminal attempt work into this packet.
>
> **Drift check**: `git diff --stat 1601f86..HEAD -- internal/runtime/runtime.go internal/services/tickets.go internal/services/tickets_test.go internal/integration sql/queries/tickets.sql internal/db`

## Status

- **Priority**: P0
- **Effort**: M
- **Risk**: MED
- **Depends on**: Plan 003
- **Category**: bug, architecture
- **Planned at**: commit `1601f86`, 2026-07-12
- **Beads**: `agent-task-tracker-vds.5`

## Why this matters

Ticket creation writes the ticket, each dependency, and the initial event as separate committed statements. Ticket update commits its data before writing the audit event. Failures therefore return errors after durable partial state has already changed, which breaks retry safety and the execution-ledger promise.

## Current state

- `internal/services/tickets.go:697-740` performs create, dependency, and event writes sequentially.
- `internal/services/tickets.go:302-376` updates before inserting its event.
- `internal/runtime/runtime.go:284-320` already demonstrates the preferred `Pool.BeginTx` plus `Queries.WithTx` pattern for transition-with-artifacts.
- `sql/queries/tickets.sql:67-98` shows the alternative CTE pattern used by simple status transitions.

## Commands

| Purpose | Command | Expected |
|---|---|---|
| Services | `rtk go test ./internal/services ./internal/runtime` | pass |
| Integration faults | `rtk go test -tags=integration ./internal/integration -run 'TestCreateTicketAtomic|TestUpdateTicketAtomic'` | pass |
| Full gate | `rtk ./scripts/verify.sh` | exit 0 |

## Scope

**In scope**: reusable runtime transaction helper, runtime ticket create/propose/update methods, service construction against `Queries.WithTx`, and fault-injection integration tests.

**Out of scope**: decomposition, attempt metrics/artifacts, schema changes, API handler wiring, and changing ticket request/response shapes.

## Git workflow

- Branch: `feat/production-005-ticket-transactions`
- Commit: `Make ticket writes transactional`

## Steps

1. Extract a small runtime transaction helper from `transitionWithArtifacts` that begins a pgx transaction, supplies `*db.Queries` bound to it, rolls back on every non-commit exit, and wraps begin/commit errors with stable context.
   - **Verify**: runtime tests cover begin failure, callback failure, commit failure, and success.
2. Route production `CreateTicket` and `ProposeTicket` through a transaction-bound `TicketService`. Keep normalization and domain validation inside the service.
   - **Verify**: existing service tests remain unchanged or require only constructor setup changes.
3. Route `UpdateTicket` through the same unit of work so data and event commit together.
4. Add PostgreSQL fault tests using transaction-local constraint or a test store hook that fails dependency/event writes. Assert zero ticket rows after failed create and unchanged ticket data after failed update-event insertion.
   - **Verify**: focused integration tests pass.
5. Ensure every CLI and MCP runtime call reaches the transactional runtime methods; direct service tests may continue to exercise domain logic with fakes.
   - **Verify**: `rtk rg -n 'NewTicketService\(' internal | sort` shows production construction only through the runtime paths intended by the plan.

## Done criteria

- [ ] Create/propose commit ticket, dependencies, and initial event together.
- [ ] Update and its audit event commit together.
- [ ] Errors never leave partial durable state.
- [ ] Existing public runtime and service request shapes remain compatible.
- [ ] Artifact and decomposition code is untouched.

## STOP conditions

- Implementing the helper requires a public transaction abstraction outside `internal/`.
- Existing callers bypass runtime in production command paths and cannot be migrated within scope.
- Fault tests cannot deterministically induce intermediate failure without production-only hooks; use test-only constraints or stores, not runtime debug flags.

## Maintenance notes

Plan 006 must reuse this transaction helper for decomposition and attempt terminal operations. Reviewers should scrutinize rollback behavior and accidental nested transactions.

