# Plan 020: Prove v0.1 with a clean production acceptance pilot

> **Executor instructions**: This is a validation packet, not a feature packet. When an acceptance step fails, file a Beads issue and stop that path; do not patch unrelated code inside the pilot branch.
>
> **Drift check**: `git diff --stat 1601f86..HEAD -- scripts docs README.md .github internal/integration plans`

## Status

- **Priority**: P0
- **Effort**: M
- **Risk**: LOW
- **Depends on**: Plans 002, 008, 010, 012, 013, 015, 018, and 019
- **Category**: tests, direction
- **Planned at**: commit `1601f86`, 2026-07-12
- **Beads**: `agent-task-tracker-vds.20`

## Why this matters

Production readiness is not the sum of merged features. Forge must be installable by a clean environment, usable by two harness identities, correct under claim/interruption races, inspectable from human surfaces, and recoverable from backup without repository knowledge or direct database intervention.

## Current state

- `README.md:368-380` defines the day-zero flow but it is manual and CLI-centric.
- Prior phase closeouts mostly use package tests and hand-run smoke commands.
- The intended v0.1 boundary is trusted, single-tenant, self-hosted, proof-first, and cross-harness.

## Commands

| Purpose | Command | Expected |
|---|---|---|
| Full gate | `rtk ./scripts/verify.sh` | exit 0 |
| Acceptance | `rtk ./scripts/production-acceptance.sh` | all machine-checkable stages pass |
| Report | generated acceptance report | contains timings/counts and no secrets |

## Scope

**In scope**: automated clean-environment acceptance, two harness identities/transports, concurrency/interruption/proof/UI/recovery/upgrade checks, results report, Beads follow-ups.

**Out of scope**: fixing discovered product defects, public SaaS load, advanced analytics, multi-tenant RBAC, or performance claims beyond the measured pilot.

## Git workflow

- Branch: `feat/production-020-acceptance-pilot`
- Commit: `Add production acceptance pilot`

## Steps

1. Build release artifacts from a clean checkout and install only from those artifacts into an isolated environment. Start fresh PostgreSQL, artifact storage, server, and worker using documented configuration.
2. Measure time from start to readiness and complete workspace/project setup without direct SQL.
3. Use two distinct harness identities and at least two transports: REST plus MCP or CLI. Create claimable work, race claims, heartbeat, checkpoint, complete/fail/block, attach proof, propose follow-up, and verify idempotent replay.
4. Simulate interruption: terminate one harness, let its lease expire, verify safe requeue, claim with the other harness, and complete with prior context visible.
5. Verify human workflows: login, scoped navigation, ticket proof page, checkpoints, artifact open, proposed triage, event ledger, mobile browser smoke, and long-history TUI navigation.
6. Back up PostgreSQL and artifact storage, destroy the environment, restore it, and verify all ticket/attempt/event/artifact metadata plus accessible proof.
7. Rehearse one compatible binary/schema upgrade and binary rollback according to Plan 015.
8. Generate `docs/production-acceptance-report.md` with environment versions and metrics, excluding URLs containing credentials, tokens, ticket content, or proof content.

## Acceptance thresholds

- Fresh install to readiness: under 15 minutes of automated wall-clock time on the documented reference machine.
- Duplicate successful claims for one ticket: zero.
- Interrupted attempt recovery: one expiry event, one requeue, successful later claim.
- Completed attempts with proof in the pilot: at least 90%.
- Canonical web link exposes state, latest checkpoint, blocker, attempts, and proof without raw UUID entry.
- Backup/restore loses zero rows and zero referenced proof objects.
- No ordinary step requires direct SQL or editing generated files.

## Done criteria

- [ ] Acceptance script passes from a clean environment.
- [ ] Report records every threshold and result.
- [ ] Failures have Beads issues with reproduction and dependencies.
- [ ] No feature implementation is mixed into the pilot branch.
- [ ] v0.1 can be declared ready only if every P0 threshold passes.

## STOP conditions

- Any security boundary can be bypassed.
- A duplicate claim or partial terminal state occurs.
- Restore leaves missing referenced proof.
- The pilot requires undocumented operator knowledge or direct SQL.

## Maintenance notes

Run this acceptance suite for every release candidate. Add scenarios only for production regressions or supported deployment promises, not every feature.

