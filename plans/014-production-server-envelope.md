# Plan 014: Add the production server and worker envelope

> **Executor instructions**: Add operational behavior around the existing runtime without changing ticket semantics. Health endpoints must disclose no sensitive configuration.
>
> **Drift check**: `git diff --stat 1601f86..HEAD -- internal/cli internal/api internal/runtime internal/config internal/jobs cmd/forge README.md docs`

## Status

- **Priority**: P0
- **Effort**: M
- **Risk**: MED
- **Depends on**: Plan 007
- **Category**: ops, reliability
- **Planned at**: commit `1601f86`, 2026-07-12
- **Beads**: `agent-task-tracker-vds.14`

## Why this matters

Server mode uses background contexts and never installs the worker's signal handling. Shutdown is unbounded, there are no liveness/readiness probes, `log_level` is unused, and requests lack correlation IDs. A process can be feature-complete and still unsafe to operate.

## Current state

- `internal/cli/cli.go:293-310` passes `context.Background()` to runtime and HTTP serving.
- Worker mode at lines 339-355 uses `signal.NotifyContext`; reuse that pattern.
- `serveHTTP` at lines 491-512 has good HTTP timeouts but calls `Shutdown(context.Background())`.
- `internal/config/config.go:15,25` defines log level but no logger consumes it.

## Commands

| Purpose | Command | Expected |
|---|---|---|
| Lifecycle unit | `rtk go test ./internal/cli ./internal/api ./internal/runtime` | pass |
| Process integration | `rtk go test -tags=integration ./internal/integration -run 'TestServerLifecycle|TestHealth'` | pass |
| Full gate | `rtk ./scripts/verify.sh` | exit 0 |

## Scope

**In scope**: signal context, bounded shutdown, `/livez`, `/readyz`, structured `slog` logging, request IDs, access/error logs, build/version metadata, worker pass logs, tests, docs.

**Out of scope**: metrics backend selection, distributed tracing, Kubernetes manifests, release packaging, or changing job scheduling.

## Git workflow

- Branch: `feat/production-014-server-envelope`
- Commit: `Add production server envelope`

## Steps

1. Create one signal-aware root context for server and worker modes. Runtime open, HTTP serving, and worker loops must share it.
2. Add a configurable shutdown timeout with a safe default. On signal: stop accepting new requests, wait within deadline, cancel remaining work, close runtime, and return zero unless shutdown itself fails.
3. Add unauthenticated minimal `/livez` and `/readyz`. Liveness proves the process loop; readiness pings PostgreSQL and verifies required storage configuration without exposing URLs, bucket names, or errors.
4. Wire `log_level` to `log/slog`. Add request ID middleware, method/path template/status/duration access logs, opaque 5xx correlation, worker-pass summaries, and startup version/commit metadata.
5. Add process tests for startup failure, readiness DB loss/recovery, SIGTERM during request, shutdown timeout, and no secret leakage.

## Done criteria

- [ ] SIGTERM produces bounded graceful shutdown.
- [ ] Liveness and readiness have distinct semantics.
- [ ] Logs are structured and honor configured level.
- [ ] Every request has a correlation ID.
- [ ] Health/log output contains no credentials or internal payload content.

## STOP conditions

- Existing deployers depend on server ignoring SIGTERM.
- Readiness requires a destructive storage probe.
- Access logging would record admin tokens, query secrets, or artifact content.

## Maintenance notes

Plan 015 consumes health endpoints and build metadata in container/release smoke tests. OpenTelemetry export can later attach to the same request IDs.

