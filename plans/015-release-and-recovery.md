# Plan 015: Build a reproducible release and recovery path

> **Executor instructions**: Do not publish a real tag, package, container, or Homebrew formula without explicit operator authorization. Building and testing local artifacts is authorized by this plan.
>
> **Drift check**: `git diff --stat 1601f86..HEAD -- go.mod .github Dockerfile .goreleaser.yaml README.md docs scripts cmd/forge LICENSE CHANGELOG.md`

## Status

- **Priority**: P1
- **Effort**: L
- **Risk**: MED
- **Depends on**: Plans 001, 009, and 014
- **Category**: release, docs, ops
- **Planned at**: commit `1601f86`, 2026-07-12
- **Beads**: `agent-task-tracker-vds.15`

## Why this matters

There is no tested artifact, image, installation channel, rollback procedure, or backup/restore drill. The module path and git remote also disagree, which will break public `go install` and provenance unless canonical identity is settled.

## Current state

- `go.mod:1` uses `github.com/vivek/agent-task-tracker`.
- `origin` is `https://github.com/vivekmaru/task-tracker.git`.
- `README.md:120-132` documents only local `go test` and `go build`.
- No tags, Dockerfile, GoReleaser config, release workflow, license, or operational recovery guide exists.

## Commands

| Purpose | Command | Expected |
|---|---|---|
| Quality | `rtk ./scripts/verify.sh` | exit 0 |
| Snapshot release | pinned GoReleaser snapshot command | archives, checksums, SBOM/provenance metadata produced locally |
| Container | `rtk docker build ...` or project-selected builder | minimal image builds |
| Recovery smoke | repository recovery script against disposable PostgreSQL | backup, destroy, restore, verify passes |

## Scope

**In scope**: canonical identity decision, version injection, GoReleaser, minimal OCI image, CI snapshot/tag workflows, checksums/SBOM/provenance, install smoke, backup/restore/upgrade/rollback runbook, license decision recording.

**Out of scope**: publishing without approval, Helm charts, managed hosting, auto-updaters, enterprise support policy, or database Down migrations as rollback.

## Git workflow

- Branch: `feat/production-015-release-recovery`
- Commit: `Add release and recovery path`

## Steps

1. **Decision gate**: obtain the canonical public repository/module path and license from the operator. Recommendation: align repository, module, binary, and product naming around Forge before the first tag. Stop until decided.
2. Update module imports only if the canonical path changes; verify the entire suite and external install path from a temporary clean module.
3. Add build-time version, commit, and build-date injection consumed by `forge version` and Plan 014 startup logs.
4. Add a pinned GoReleaser snapshot producing supported OS/architecture archives, checksums, SBOM, and provenance metadata. Add a minimal non-root container image containing only the binary and required certificates.
5. Add CI snapshot builds on PRs and a tag workflow that requires the full quality/integration gate. Keep publishing disabled or approval-gated until explicitly authorized.
6. Write and execute a runbook for topology, secrets/TLS boundary, migrate-before-start, PostgreSQL backup/restore, artifact-store backup, upgrade rehearsal, binary rollback with forward-only schema, and incident diagnostics.
7. Add a clean-environment smoke that installs the artifact, migrates, starts server and worker, checks readiness, runs day-zero CLI flow, backs up, destroys, restores, and verifies ticket/artifact metadata.

## Done criteria

- [ ] Canonical module/repository/license decisions are recorded.
- [ ] Snapshot artifacts and container are reproducible and versioned.
- [ ] Checksums and supply-chain metadata are produced.
- [ ] Clean install and recovery smoke passes.
- [ ] No real publication occurred without explicit approval.

## STOP conditions

- Canonical repository path or license is undecided.
- Recovery requires a Down migration.
- Container must run as root or include source/build tools.
- Artifact-store backup cannot preserve proof referenced by restored metadata.

## Maintenance notes

Every release must run Plan 020 acceptance. Schema rollback policy is forward-fix plus compatible binary rollback unless a separately rehearsed migration says otherwise.

