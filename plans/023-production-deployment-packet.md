# Plan 023: Ship the production deployment packet

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 9f8d948..HEAD -- Dockerfile docs/release-and-recovery.md README.md scripts/`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: LOW
- **Depends on**: none
- **Category**: docs
- **Beads**: agent-task-tracker-919
- **Planned at**: commit `9f8d948`, 2026-07-18

## Why this matters

Forge has release engineering (GoReleaser, distroless Docker image, recovery drill script, upgrade/rollback runbook) but no path from "I have a binary" to "Forge is serving production traffic": there is no compose or systemd example, no reverse-proxy TLS example (the server itself is HTTP-only and requires `FORGE_AUTH_COOKIE_SECURE=true` behind HTTPS), and no scheduled-backup example, and no single production checklist. The production dogfood pilot (plan 024) is blocked on exactly this. The deliverable is a `deploy/` directory of working examples plus `docs/deployment.md`, all consistent with decisions already recorded in `docs/release-and-recovery.md`.

## Current state

- `Dockerfile` — two-stage build; final image `gcr.io/distroless/static-debian12:nonroot`, `ENTRYPOINT ["/forge"]`, runs as nonroot. Build args `VERSION`, `COMMIT`, `BUILD_DATE`.
- `docs/release-and-recovery.md` — runbook: back up Postgres + artifact store before migration; run `forge migrate` with the new binary before swapping processes; check `/readyz`; never run down migrations in production; `scripts/recovery-smoke.sh` is the restore drill.
- `README.md:49-95` — configuration contract: `FORGE_DATABASE_URL`, `FORGE_HTTP_ADDR`, `FORGE_WORKER_CONCURRENCY`, `FORGE_ADMIN_TOKEN`, `FORGE_AUTH_COOKIE_SECURE`, `FORGE_ARTIFACT_ROOT`, `FORGE_ARTIFACT_BACKEND` (+ S3 keys). `forge init` generates a mode-0600 config with a 32-byte token. Non-loopback servers require secure cookies. Processes: `forge server` (HTTP + web UI) and `forge worker` (maintenance + webhook delivery), plus one-shot `forge migrate`.
- `README.md:36` — `/livez` (process liveness) and `/readyz` (PostgreSQL dependency) exist for probes.
- No `deploy/`, no compose file, no systemd units, no TLS example anywhere in the repo (`ls` the root to confirm).
- Product decisions to honor (from `plans/README.md` "Decisions intentionally preserved"): v0.1 is trusted, single-tenant, self-hosted; no Redis, no enterprise RBAC. Do not add infrastructure beyond Postgres + the two Forge processes + a reverse proxy.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Compose config check | `docker compose -f deploy/compose/docker-compose.yml config` | renders, exit 0 |
| Compose boot | `docker compose -f deploy/compose/docker-compose.yml up -d` then `curl -fsS http://localhost:<port>/livez` | 200 |
| Image build | `docker build -t forge:plan023 .` | exit 0 |
| systemd lint | `systemd-analyze verify deploy/systemd/*.service` (Linux only; skip on macOS and note it) | no errors |
| Go gate untouched | `go test ./...` | all pass |

## Scope

**In scope** (files to create/modify):
- `deploy/compose/docker-compose.yml` (create)
- `deploy/compose/Caddyfile` (create)
- `deploy/compose/.env.example` (create — placeholder values only, no real secrets)
- `deploy/systemd/forge-server.service`, `deploy/systemd/forge-worker.service` (create)
- `deploy/backup/forge-backup.sh`, `deploy/backup/forge-backup.timer` + `.service` (create)
- `docs/deployment.md` (create)
- `README.md` (one link line to `docs/deployment.md`)

**Out of scope** (do NOT touch):
- Any Go source — no in-process TLS, no new endpoints, no config keys.
- `.goreleaser.yaml`, `Dockerfile`, CI workflows.
- Kubernetes manifests — deliberately deferred until someone needs them; note this in `docs/deployment.md`.

## Git workflow

- Branch: `advisor/023-deployment-packet`
- Commit style: short imperative sentence (match `git log --oneline`).
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Compose stack

Create `deploy/compose/docker-compose.yml` with four services: `postgres` (postgres:16, named volume, healthcheck `pg_isready`), `migrate` (forge image, command `migrate`, runs to completion, depends_on postgres healthy), `server` (command `server`, depends_on migrate completed successfully, healthcheck GET `/livez`), `worker` (command `worker`, same dependency), and `caddy` (reverse proxy terminating TLS, proxying to `server`). All Forge config via environment from `.env` (see `.env.example` with placeholders such as `FORGE_ADMIN_TOKEN=replace-with-32-byte-token`). Set `FORGE_AUTH_COOKIE_SECURE=true` in the server environment since Caddy terminates HTTPS. Artifact root on a named volume shared read-only into nothing else.

**Verify**: `docker compose -f deploy/compose/docker-compose.yml config` → exit 0.

### Step 2: Caddyfile

Create `deploy/compose/Caddyfile`: one site block with automatic HTTPS for a `{$FORGE_DOMAIN}` placeholder, `reverse_proxy server:3017`, and a comment showing the `localhost`-only variant (internal TLS) for LAN deployments.

**Verify**: `docker run --rm -v $PWD/deploy/compose/Caddyfile:/etc/caddy/Caddyfile caddy:2 caddy validate --config /etc/caddy/Caddyfile` → valid (set a dummy `FORGE_DOMAIN`).

### Step 3: End-to-end compose smoke

Boot the stack with the local image (`docker build -t forge:plan023 .`), run the README smoke sequence against it through the proxy (create workspace/project/ticket via `docker compose exec server` is NOT possible on distroless — run the CLI from the host with `FORGE_DATABASE_URL` pointed at the published postgres port instead), log in through the browser URL, then `docker compose down -v`.

**Verify**: `/livez` 200 through Caddy; login page renders over HTTPS (or localhost TLS); `docker compose down -v` clean.

### Step 4: systemd units

Create `deploy/systemd/forge-server.service` and `forge-worker.service` for bare-metal binaries: `User=forge`, `EnvironmentFile=/etc/forge/forge.env`, `ExecStart=/usr/local/bin/forge server` (resp. `worker`), `Restart=on-failure`, hardening keys (`NoNewPrivileges=true`, `ProtectSystem=strict`, `ReadWritePaths=` for the artifact root, `PrivateTmp=true`). Include a commented `ExecStartPre=/usr/local/bin/forge migrate` note explaining the runbook prefers explicit migration per `docs/release-and-recovery.md`.

**Verify**: `systemd-analyze verify deploy/systemd/*.service` on Linux; on macOS, state in the run notes that verification was skipped and why.

### Step 5: Backup examples

Create `deploy/backup/forge-backup.sh` (pg_dump custom format to a dated file, plus `tar` of the local artifact root, retention of N days, never echoing the database URL) and a systemd timer pair running it daily. Reference `scripts/recovery-smoke.sh` as the restore drill.

**Verify**: shellcheck-clean if shellcheck is available (`shellcheck deploy/backup/forge-backup.sh`); dry-run the script against a scratch database.

### Step 6: `docs/deployment.md`

Write the operator guide: prerequisites; choosing compose vs systemd; initial deploy checklist (generate token with `forge init`, set `FORGE_AUTH_COOKIE_SECURE=true`, run `forge migrate`, verify `/readyz`, run one create→claim→complete smoke); upgrade procedure (link `docs/release-and-recovery.md`, don't duplicate it); backup/restore pointers; explicit non-goals (no Kubernetes manifests yet, no in-process TLS — reverse proxy owns HTTPS). Add one link line in `README.md` near the other doc links (around line 411-423).

**Verify**: every command quoted in the doc has been executed once during this plan; `grep -c "TODO" docs/deployment.md` → 0.

## Test plan

This plan is examples + docs; its tests are the verification commands above (compose config/boot, caddy validate, systemd-analyze, shellcheck, smoke through the proxy). `go test ./...` must remain green and untouched.

## Done criteria

- [ ] `docker compose -f deploy/compose/docker-compose.yml config` exits 0
- [ ] Full stack boots locally; `/livez` 200 through the proxy; clean `down -v`
- [ ] `deploy/systemd/` units exist and pass `systemd-analyze verify` (or skip documented)
- [ ] `docs/deployment.md` exists, linked from README, zero TODO markers
- [ ] No secret values anywhere in `deploy/` (`grep -rn "postgres://.*:.*@" deploy/ | grep -v example` → empty)
- [ ] `go test ./...` exits 0 with no Go changes
- [ ] `plans/README.md` status row updated

## STOP conditions

Stop and report back (do not improvise) if:

- The server requires a config key the README does not document (config contract drift).
- The distroless image cannot run `migrate` as a compose one-shot (entrypoint drift) — report rather than switching base images.
- Docker is unavailable in the execution environment — deliver compose/systemd/docs unverified-by-boot and say so explicitly.

## Maintenance notes

- `docs/deployment.md` and `deploy/` must be updated together with any config key addition — reviewers should demand it in the same PR.
- When plan 024 (dogfood pilot) runs, friction with these examples is signal: file tickets against this packet.
- Deferred: Kubernetes manifests; OpenTelemetry/Prometheus metrics endpoint (revisit after the pilot proves need — recorded as rejected-for-now in `plans/README.md`).
