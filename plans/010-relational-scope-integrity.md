# Plan 010: Enforce workspace and project scope integrity in PostgreSQL

> **Executor instructions**: This is a data migration. Inventory and fail safely before adding constraints; never coerce mismatched rows into a workspace silently.
>
> **Drift check**: `git diff --stat 1601f86..HEAD -- sql/migrations/0001_initial_schema.sql sql/migrations sql/queries internal/services internal/integration internal/db`

## Status

- **Priority**: P0
- **Effort**: L
- **Risk**: HIGH
- **Depends on**: Plan 009
- **Category**: migration, security, correctness
- **Planned at**: commit `1601f86`, 2026-07-12
- **Beads**: `agent-task-tracker-vds.10`

## Why this matters

Workspace, project, ticket, attempt, event, artifact, and dependency IDs are independently foreign-keyed. Valid IDs from different scopes can be combined into one row, causing claims, analytics, cascades, and authorization to disagree about ownership.

## Current state

- `sql/migrations/0001_initial_schema.sql:12-25` links a project to a workspace but lets a ticket independently select both IDs.
- Attempts at lines 68-72 and artifacts at lines 136-142 repeat scope IDs without composite foreign keys.
- `internal/services/tickets.go:823-830` validates UUID presence, not project membership.
- Some service operations derive child scope from a trusted ticket; direct create paths accept caller-provided scope.

## Commands

| Purpose | Command | Expected |
|---|---|---|
| Migration integration | `rtk go test -tags=integration ./internal/integration -run 'TestScopeIntegrity|TestScopeMigration'` | pass |
| Services | `rtk go test ./internal/services ./internal/db` | pass |
| Full gate | `rtk ./scripts/verify.sh` | exit 0 |

## Scope

**In scope**: mismatch inventory query, forward migration with composite uniqueness/FKs, scoped write validation where needed, generated DB code, tests, and operator remediation documentation.

**Out of scope**: membership roles, API-key scopes, data sharing across workspaces, or changing public identifiers.

## Git workflow

- Branch: `feat/production-010-scope-integrity`
- Commit: `Enforce relational scope integrity`

## Steps

1. Write a read-only inventory query covering project/workspace, ticket/project, attempt/ticket, checkpoint/attempt, event/ticket/attempt, artifact/ticket/attempt, dependency endpoints, and source references. Produce counts and IDs only; never dump artifact or ticket content.
   - **Verify**: clean fixture returns zero; deliberately inconsistent fixture identifies every mismatch.
2. Add composite unique keys needed as FK targets, beginning with `(workspace_id, id)` on projects and `(workspace_id, project_id, id)` on tickets/attempts.
3. Add composite foreign keys from every repeated-scope child. Use appropriate `ON DELETE` behavior matching the existing simple FK. Do not remove simple FKs until the composite constraints are proven and redundancy is reviewed.
4. Make the migration fail with an actionable error when inventory finds inconsistent rows. Document manual remediation choices; do not auto-reassign.
5. Add scoped service lookups before caller-controlled writes where a database error would otherwise be opaque.
   - **Verify**: integration tests attempt cross-workspace combinations for each resource family and assert rejection.

## Done criteria

- [ ] Every repeated workspace/project/resource relationship is enforced.
- [ ] Migration preflight detects existing inconsistent rows.
- [ ] No automatic cross-tenant data reassignment occurs.
- [ ] Same-scope create, claim, transition, artifact, and dependency flows still pass.
- [ ] Delete cascades retain intended behavior.

## STOP conditions

- Inventory finds real inconsistent data in a user database.
- A composite FK would change an intentional cross-project relationship documented in the PRD.
- Adding constraints requires long blocking validation unsuitable for the supported deployment; design `NOT VALID` plus later validation before proceeding.

## Maintenance notes

Future tables containing both workspace/project and another resource ID must use the same composite-FK pattern. This plan provides isolation integrity, not user authorization.

