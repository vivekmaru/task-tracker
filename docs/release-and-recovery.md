# Release and recovery runbook

## Release identity

Forge uses `github.com/vivek/agent-task-tracker` as its canonical Go import
path. The current Git remote is source provenance only. The project is
proprietary and unpublished; release automation must not create tags or upload
artifacts without explicit operator approval.

## Build a local release candidate

Run the quality gate, then build a local snapshot using the pinned GoReleaser
image or binary described in `.goreleaser.yaml`. Verify `checksums.txt`, SBOM
documents, and `forge version` before any distribution. Build the container
with explicit `VERSION`, `COMMIT`, and `BUILD_DATE` arguments; it runs as the
distroless non-root user and contains no source or build tools.

## Upgrade and rollback

1. Back up PostgreSQL and the configured artifact store before migration.
2. Run `forge migrate` with the new binary before replacing server/worker
   processes.
3. Check `/readyz`, then run a create/claim/checkpoint/complete smoke flow.
4. If the binary regresses, roll back only to a binary compatible with the
   forward schema and repair forward with a new migration. Do not run down
   migrations in production.

## Restore drill

Use `scripts/recovery-smoke.sh` against an isolated PostgreSQL instance. Set
`FORGE_RECOVERY_PG_CONTAINER` when using a Docker database so the dump and
restore tools match the server major version. It
backs up metadata, restores into a fresh database, and verifies Forge can read
the restored schema. Back up local artifact roots or S3 buckets separately and
preserve objects referenced by restored artifact metadata.

Incident logs should use request IDs, never database URLs, tokens, query
strings, webhook secrets, or artifact contents.
