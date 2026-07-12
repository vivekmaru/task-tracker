# Plan 009: Make migrations immutable and sqlc schema-complete

> **Executor instructions**: Preserve upgrades from every currently supported database. Never rewrite historical migrations to simplify the fresh-install path.
>
> **Drift check**: `git diff --stat 1601f86..HEAD -- internal/cli/migrate.go internal/cli/migrate_test.go internal/db/migrations_test.go sql/migrations sqlc.yaml README.md scripts`

## Status

- **Priority**: P0
- **Effort**: M
- **Risk**: MED
- **Depends on**: Plan 003
- **Category**: migration, dx
- **Planned at**: commit `1601f86`, 2026-07-12
- **Beads**: `agent-task-tracker-vds.9`

## Why this matters

Applied migrations are recorded by ID and filename only, so modified content is silently accepted. `sqlc.yaml` stops at migration 0006 even though later migrations affect the deployed schema. Fresh installs and upgrades can therefore diverge without a failing check.

## Current state

- `internal/cli/migrate.go:19-24` defines history without a checksum.
- `internal/cli/migrate.go:147-165` skips an applied ID without comparing file content.
- `sqlc.yaml:4-10` manually lists migrations only through 0006.
- Both fresh migration and baseline-existing behavior must remain supported.

## Commands

| Purpose | Command | Expected |
|---|---|---|
| Migration unit | `rtk go test ./internal/cli ./internal/db` | pass |
| Upgrade integration | `rtk go test -tags=integration ./internal/integration -run 'TestMigration|TestSQLC'` | pass |
| Generation | pinned sqlc generate command from Plan 001 | no git diff |
| Full gate | `rtk ./scripts/verify.sh` | exit 0 |

## Scope

**In scope**: migration-history checksum support, adoption/backfill behavior, complete sqlc schema input, generation drift check, fresh and upgrade tests, docs.

**Out of scope**: scope foreign keys from Plan 010, changing application queries, or deleting baseline-existing support.

## Git workflow

- Branch: `feat/production-009-migration-integrity`
- Commit: `Verify migration and sqlc integrity`

## Steps

1. Extend `forge_schema_migrations` with a nullable SHA-256 checksum using idempotent bootstrap SQL. Hash the entire migration file bytes so Down sections and operational comments are immutable too.
2. On first run after upgrade, backfill null checksums from the matching current file while recording that adoption occurred. On later runs, reject any mismatch before applying new migrations.
   - **Verify**: tests cover new DB, existing history without checksums, matching checksum, changed file, renamed file, and missing file.
3. Make sqlc consume the authoritative ordered migration directory if the pinned version supports it. Otherwise add a check that fails when any migration file is absent from `sqlc.yaml`.
   - **Verify**: every current and Plan 002 migration contributes to schema validation.
4. Add integration paths for migrate-from-zero, migrate twice, upgrade from a fixture at migration 0006, and baseline-existing adoption.
5. Add generated-code drift to the quality gate using a temporary clean comparison, not by committing after-the-fact generated changes in CI.

## Done criteria

- [ ] Applied migration mutation is detected before startup changes schema.
- [ ] Existing installations adopt checksums safely once.
- [ ] sqlc sees the complete migration chain.
- [ ] Fresh and representative upgrade paths pass on PostgreSQL.
- [ ] Pinned generation produces no diff.

## STOP conditions

- Existing migration files are already known to differ across deployed environments.
- The chosen checksum adoption would bless unknown production content without operator visibility.
- sqlc cannot parse a migration in the authoritative directory; isolate the exact incompatibility before excluding anything.

## Maintenance notes

All future migrations must be append-only. Release review should reject any change to a migration with a recorded checksum.

