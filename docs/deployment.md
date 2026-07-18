# Deploying Forge

This guide takes Forge from a built binary or image to serving production
traffic. Forge v0.1 is trusted, single-tenant, and self-hosted: the moving parts
are PostgreSQL, the `forge server` (HTTP + human web UI) and `forge worker`
(maintenance + webhook delivery) processes, and a reverse proxy that terminates
HTTPS. The server speaks plain HTTP by design — there is no in-process TLS.

Working examples live in [`deploy/`](../deploy):

- `deploy/compose/` — Docker Compose stack (Postgres + migrate + server + worker + Caddy).
- `deploy/systemd/` — hardened systemd units for a bare-metal install.
- `deploy/backup/` — a `pg_dump` + artifact backup script and a daily timer.

## Prerequisites

- PostgreSQL 16.
- A domain (for public HTTPS) or a LAN hostname (for internal TLS).
- Either Docker + Docker Compose, or a Linux host with systemd and the `forge` binary at `/usr/local/bin/forge`.
- The `sql/migrations/` directory from this repo or a release tarball (see "Supplying migrations" below).

## Choosing compose vs systemd

- **Compose** is the fastest path: one `docker compose up -d` brings up the
  database, applies migrations, starts both processes, and fronts them with
  Caddy. Best for a single host where Docker is already in use.
- **systemd** suits bare-metal or VM installs where you manage PostgreSQL and
  the reverse proxy separately and want the Forge processes supervised by the
  init system with resource/hardening controls.

## Supplying migrations

`forge migrate` reads migration files from a directory (default
`sql/migrations`). The release container image is distroless and ships only the
`/forge` binary, so the migrations must be provided to it:

- **Compose** bind-mounts `../../sql/migrations` into the `migrate` one-shot and
  runs `migrate --dir /migrations` — handled for you in
  `deploy/compose/docker-compose.yml`.
- **Bare metal** should keep the migrations directory alongside the binary (for
  example `/usr/local/share/forge/migrations`) and run
  `forge migrate --dir /usr/local/share/forge/migrations`.

(Embedding the migrations in the binary is tracked as a follow-up so the image
becomes self-sufficient.)

## Initial deploy with Docker Compose

```bash
cd deploy/compose
cp .env.example .env
# Edit .env: set FORGE_DOMAIN, POSTGRES_PASSWORD, a matching FORGE_DATABASE_URL,
# and a generated FORGE_ADMIN_TOKEN (for example: openssl rand -base64 32).

# Validate the rendered configuration.
docker compose config

# Build the image locally (or set FORGE_IMAGE in .env to a published tag).
docker build -t forge:latest ../..

# Bring the stack up. Order is enforced by depends_on: Postgres becomes healthy,
# the migrate one-shot applies migrations and exits 0, then server and worker
# start, then Caddy.
docker compose up -d

# Verify liveness through the proxy (use -k for the internal/self-signed cert
# when FORGE_DOMAIN=localhost).
curl -fsSk https://localhost/livez
```

`FORGE_AUTH_COOKIE_SECURE=true` is already set for the server in the compose
file because Caddy terminates HTTPS; the server binds `0.0.0.0:3017` internally
so Caddy can reach it. Because the release image is distroless (no shell), the
server and worker have no in-container HTTP healthcheck — startup ordering is
handled by `depends_on` and Caddy retries its upstream until the server is
listening. Verify health from the host with the `curl` above.

Tear the stack down (removing volumes) with:

```bash
docker compose down -v
```

## Initial deploy with systemd

1. Create a `forge` user and the artifact directory:

   ```bash
   useradd --system --home /var/lib/forge --shell /usr/sbin/nologin forge
   install -d -o forge -g forge /var/lib/forge/artifacts
   ```

2. Generate a config with a fresh 32-byte admin token (mode-0600, never printed):

   ```bash
   forge init --path /etc/forge/forge.json \
     --database-url "postgres://forge@db.internal:5432/forge?sslmode=disable" \
     --artifact-root /var/lib/forge/artifacts
   ```

   Then write `/etc/forge/forge.env` with `FORGE_DATABASE_URL`,
   `FORGE_ADMIN_TOKEN`, `FORGE_ARTIFACT_ROOT=/var/lib/forge/artifacts`, and
   `FORGE_AUTH_COOKIE_SECURE=true` (required for a non-loopback server behind
   HTTPS). Keep it mode-0600, owned by `forge`.

3. Apply migrations explicitly (the runbook prefers this over migrating on
   start):

   ```bash
   forge migrate --dir /usr/local/share/forge/migrations
   ```

4. Install the units and start the services:

   ```bash
   cp deploy/systemd/forge-server.service deploy/systemd/forge-worker.service /etc/systemd/system/
   systemctl daemon-reload
   systemctl enable --now forge-server forge-worker
   ```

5. Put a reverse proxy (Caddy, nginx, etc.) in front, terminating HTTPS and
   proxying to `127.0.0.1:3017`. `deploy/compose/Caddyfile` is a working
   reference.

6. Verify readiness (checks the PostgreSQL dependency):

   ```bash
   curl -fsS http://127.0.0.1:3017/readyz
   ```

The units set `NoNewPrivileges`, `ProtectSystem=strict`, `PrivateTmp`, and
restrict writes to the artifact root via `ReadWritePaths`. On Linux, verify them
with `systemd-analyze verify deploy/systemd/*.service`. (This was not run while
authoring this guide because the authoring host is macOS, which has no
`systemd-analyze`.)

## Post-deploy smoke

Confirm the operator workflow end to end by following the
[Local Smoke Test](../README.md#local-smoke-test) with `FORGE_DATABASE_URL`
pointed at your deployment's database: create a workspace and project, create a
ticket, then `codex claim` → `codex checkpoint` → `codex complete --proof`, and
finally sign in at `https://<domain>/login` and confirm the ticket, its attempt,
and the proof artifact render.

## Upgrades

Follow [release-and-recovery.md](release-and-recovery.md) — do not duplicate it
here. In short: back up first, run `forge migrate` with the new binary before
swapping processes, check `/readyz`, and never run down migrations in
production.

## Backups and restore

`deploy/backup/forge-backup.sh` writes a custom-format `pg_dump` plus a tar of
the artifact root to `FORGE_BACKUP_DIR`, pruning files older than
`FORGE_BACKUP_RETENTION_DAYS`. Schedule it with the provided
`forge-backup.timer`/`forge-backup.service` (daily at 02:30). The restore drill
is `scripts/recovery-smoke.sh`, described in
[release-and-recovery.md](release-and-recovery.md).

## Non-goals

- **Kubernetes manifests** are intentionally not provided yet; add them when a
  deployment actually needs them.
- **In-process TLS** is out of scope: the reverse proxy owns HTTPS. The server
  is HTTP-only and expects to sit behind a proxy that sets secure cookies.
- **Metrics endpoint (Prometheus/OTel)** is deferred; `/livez`, `/readyz`,
  structured logs, and the webhook observability export cover v0.1 needs.
