# Plan 006: Make decomposition and terminal attempt writes atomic

> **Executor instructions**: Reuse the transaction helper from Plan 005. One logical runtime request must use one database transaction; do not add nested transactions.
>
> **Drift check**: `git diff --stat 1601f86..HEAD -- internal/runtime internal/services/ticket_decomposition.go internal/services/attempts.go internal/integration internal/storage sql/queries`

## Status

- **Priority**: P0
- **Effort**: M
- **Risk**: MED
- **Depends on**: Plan 005
- **Category**: bug, architecture
- **Planned at**: commit `1601f86`, 2026-07-12
- **Beads**: `agent-task-tracker-vds.6`

## Why this matters

Decomposition can return after committing only some children or dependencies. Complete, fail, and block can commit terminal state and then fail while writing metrics, so callers receive an error even though the attempt can no longer be retried. These are externally visible retry-safety failures.

## Current state

- `internal/services/ticket_decomposition.go:58-98` creates children and edges in loops with no enclosing transaction.
- `internal/services/attempts.go:170-254` writes terminal state before optional metrics.
- `internal/runtime/runtime.go:284-320` already groups transition plus artifact metadata in a transaction, but the pattern is not shared by all terminal methods.
- Filesystem/S3 proof upload occurs before database registration and has explicit cleanup on database failure; preserve that boundary.

## Commands

| Purpose | Command | Expected |
|---|---|---|
| Runtime/services | `rtk go test ./internal/runtime ./internal/services ./internal/cli ./internal/mcp` | pass |
| Integration | `rtk go test -tags=integration ./internal/integration -run 'TestDecomposeAtomic|TestTerminalMetricsAtomic|TestTerminalArtifactsAtomic'` | pass |
| Full gate | `rtk ./scripts/verify.sh` | exit 0 |

## Scope

**In scope**: runtime transaction wrapping for decomposition and complete/fail/block, transaction-bound services, artifact metadata registration, uploaded-object cleanup, and fault tests.

**Out of scope**: changing decomposition validation, new artifact backends, cancel/expiry semantics, or REST transport work.

## Git workflow

- Branch: `feat/production-006-domain-transactions`
- Commit: `Make decomposition and terminal writes atomic`

## Steps

1. Route `Runtime.DecomposeTicket` through the Plan 005 transaction helper and a transaction-bound `TicketService`. All children, their initial events, and dependency edges must share the transaction.
   - **Verify**: an injected failure on child N or an edge leaves zero children from the request.
2. Route complete, fail, and block through a transaction-bound `AttemptService`, including optional metrics.
   - **Verify**: a metrics insert failure leaves the attempt running and ticket unchanged.
3. Consolidate `CompleteWithArtifacts` and `BlockWithArtifacts` on the same helper. Keep object upload outside the DB transaction and clean uploaded objects if the DB transaction rolls back.
   - **Verify**: tests cover DB failure after upload and confirm cleanup is attempted exactly once.
4. Confirm CLI and MCP commands receive an error only when no terminal state committed.
   - **Verify**: existing adapter tests plus new regression cases pass.

## Done criteria

- [ ] Decomposition is all-or-nothing.
- [ ] Terminal state, event, metrics, and artifact metadata are all-or-nothing.
- [ ] External object cleanup behavior is preserved.
- [ ] No nested pgx transaction is introduced.
- [ ] Public payloads remain compatible.

## STOP conditions

- Plan 005 transaction helper is not available or has unresolved rollback defects.
- S3 cleanup semantics require deleting pre-existing remote objects.
- Achieving atomicity requires holding a transaction open during file or network upload.

## Maintenance notes

Reviewers should trace every error return after the first durable write. Future multi-write runtime operations must use the same unit-of-work pattern.

