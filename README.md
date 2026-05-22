# Forge

Forge is a pull-based work ledger for autonomous AI agents.

Phase 1 establishes the execution core: Postgres schema, sqlc queries, ticket and attempt services, transactional claim semantics, idempotency replay, lightweight artifact metadata, JSON-first CLI commands, Huma route registration, and correctness tests.

## Current Phase 1 Status

Implemented:

- Core Go binary skeleton.
- Config loading from JSON file and environment.
- Postgres migration for workspaces, projects, tickets, dependencies, attempts, checkpoints, events, artifacts, idempotency keys, API keys, capabilities, and metrics.
- sqlc-generated query layer.
- Ticket create/propose/list service.
- Transactional `claim-next` query and claim context bundle hydration.
- Heartbeat, checkpoint, terminal attempt transitions, lease expiry transition, and idempotency cleanup worker.
- Claim idempotency replay for stable retry keys.
- Lightweight artifact metadata registration.
- JSON-first CLI commands over the shared runtime.
- Huma OpenAPI route registration under `/api/v1`.
- Server-rendered web ticket list and detail inspection pages under `/tickets`.
- Phase 1 correctness regression tests.

Known current limitation:

- `forge server` starts the HTTP router with the OpenAPI surface and the first web inspection pages. `forge worker` opens the live runtime and validates configuration, but it does not yet run a long-lived River loop.

## Requirements

- Go 1.26+
- PostgreSQL with `pgcrypto`
- `psql`
- Optional: `sqlc` via `go run github.com/sqlc-dev/sqlc/cmd/sqlc@latest`

## Configuration

Forge reads config from defaults, an optional JSON file, then environment variables.

Environment variables:

```bash
export FORGE_DATABASE_URL='postgres://localhost:5432/forge?sslmode=disable'
export FORGE_HTTP_ADDR='127.0.0.1:3017'
export FORGE_WORKER_CONCURRENCY=1
export FORGE_ADMIN_TOKEN='change-me-local-admin-token'
export FORGE_AUTH_COOKIE_SECURE=false
export FORGE_ARTIFACT_ROOT="$PWD/.forge/artifacts"
```

Equivalent config file:

```json
{
  "database_url": "postgres://localhost:5432/forge?sslmode=disable",
  "http_addr": "127.0.0.1:3017",
  "worker_concurrency": 1,
  "admin_token": "change-me-local-admin-token",
  "auth_cookie_secure": false,
  "artifact_root": ".forge/artifacts"
}
```

Set `FORGE_AUTH_COOKIE_SECURE=true` or `"auth_cookie_secure": true` when the human web UI is served through HTTPS, including HTTPS termination in front of the local server. Keep it `false` for direct plain HTTP access.

Local artifact URLs under `local://artifacts/...` resolve inside `FORGE_ARTIFACT_ROOT`, so a proof registered as `local://artifacts/go-test-output.txt` can be opened from the human `/artifacts/{id}` route when that file exists under the configured artifact root. Relative config-file artifact roots resolve from the config file directory; the built-in default resolves under the user's home directory. `forge codex complete --proof ./go-test-output.txt` and `forge codex block --proof ./blocked.log` also copy filesystem proofs into that root before registering them.

Pass it with:

```bash
forge server --config forge.json
forge worker --config forge.json
```

## Database Setup

Create a database:

```bash
createdb forge
```

Apply the current migration `Up` section:

```bash
sed '/-- +goose Down/,$d' sql/migrations/0001_initial_schema.sql | psql "$FORGE_DATABASE_URL"
```

Regenerate sqlc code after query changes:

```bash
go run github.com/sqlc-dev/sqlc/cmd/sqlc@latest generate
```

## Build And Test

Run all tests:

```bash
go test ./...
```

Build the CLI:

```bash
go build -o forge ./cmd/forge
```

## Runtime Commands

The process commands open the shared runtime. `forge server` listens on `http_addr` and exposes `/api/v1/openapi.json`, `/login`, `/workspaces`, `/tickets`, and `/tickets/{id}`. Human web views require `admin_token`; sign in at `/login`, or pass `Authorization: Bearer $FORGE_ADMIN_TOKEN` for scripted checks:

```bash
forge server --config forge.json
forge worker --config forge.json
forge tui --config forge.json --workspace-id "$WORKSPACE_ID" --project-id "$PROJECT_ID"
```

Human web routes are stable inspection links:

- `/workspaces` lists workspaces and creates minimal workspace scopes.
- `/workspaces/{workspace_id}` shows projects for a workspace and creates minimal project scopes.
- `/tickets?workspace_id={workspace_id}&project_id={project_id}` opens a scoped ticket queue.
- `/tickets/{ticket_id}` opens ticket detail.
- `/attempts/{attempt_id}` opens attempt detail.
- `/artifacts/{artifact_id}` opens artifact metadata.
- `/proposed/{ticket_id}` opens a proposed follow-up inspection view.

The TUI detail view prints the same route paths in its copy section so links can be pasted into chat, PRs, and handoffs.

The JSON-first execution commands call the same runtime services:

```bash
forge create --json \
  --workspace-id "$WORKSPACE_ID" \
  --project-id "$PROJECT_ID" \
  --title "Fix failing auth tests" \
  --type bug \
  --description "Investigate and fix failing auth tests" \
  --acceptance "Auth tests pass" \
  --verify "go test ./..."
```

```bash
forge propose --json \
  --workspace-id "$WORKSPACE_ID" \
  --project-id "$PROJECT_ID" \
  --title "Stabilize flaky auth refresh test" \
  --type bug \
  --description "Observed intermittent auth refresh failures" \
  --acceptance "Auth refresh test passes 10 consecutive runs" \
  --verify "pnpm test auth-refresh --repeat 10" \
  --created-by agent \
  --created-by-id codex \
  --reason "Follow-up discovered during attempt"
```

```bash
forge claim-next --json \
  --workspace-id "$WORKSPACE_ID" \
  --project-id "$PROJECT_ID" \
  --agent-id codex \
  --harness codex \
  --capability codegen \
  --capability testing \
  --lease 30m \
  --idempotency-key "$STABLE_CLAIM_KEY"
```

```bash
forge heartbeat --json "$ATTEMPT_ID" --lease 30m
```

```bash
forge checkpoint --json "$ATTEMPT_ID" \
  --summary "Identified auth middleware branch causing session expiry" \
  --progress 40 \
  --file internal/auth/middleware.go \
  --command "go test ./internal/auth" \
  --next "Patch retry branch and add regression coverage"
```

```bash
forge complete --json "$ATTEMPT_ID" --summary "Auth retry fix landed and tests pass"
```

```bash
forge fail --json "$ATTEMPT_ID" \
  --reason "Could not reproduce failure after clean checkout" \
  --category task_failed
```

```bash
forge block --json "$ATTEMPT_ID" \
  --reason "Missing API token required for integration test" \
  --category permission_required
```

```bash
forge attach --json \
  --workspace-id "$WORKSPACE_ID" \
  --project-id "$PROJECT_ID" \
  --ticket-id "$TICKET_ID" \
  --attempt-id "$ATTEMPT_ID" \
  --type test_output \
  --role evidence \
  --name "go-test-output.txt" \
  --url "local://artifacts/go-test-output.txt" \
  --mime-type text/plain
```

```bash
forge list --json \
  --workspace-id "$WORKSPACE_ID" \
  --project-id "$PROJECT_ID" \
  --status todo
```

```bash
forge get --json --kind ticket "$TICKET_ID"
forge get --json --kind attempt "$ATTEMPT_ID"
```

## Day-Zero Flow

The first end-to-end dogfood path should prove this sequence:

1. Create a workspace and project from `/workspaces`, or seed them directly in Postgres.
2. Create tickets with `forge create --json`.
3. Have an agent claim eligible work with `forge claim-next --json`.
4. Heartbeat during execution.
5. Write at least one checkpoint with summary, files touched, commands run, and next step.
6. Complete, fail, or block the attempt.
7. Attach proof metadata such as test output, diff path, diagnostic log, or final response.
8. List tickets and inspect ticket/attempt details.
9. Retry `claim-next` with the same idempotency key and request to verify replay behavior.

## Correctness Checks

Phase 1 tests cover:

- Claim locking with `FOR UPDATE SKIP LOCKED`.
- One running attempt per ticket.
- Workspace/project/type/tag/harness/capability/dependency eligibility.
- Retry-limit dead-letter behavior.
- Lease expiry returning work to `todo` or `failed`.
- Blocked work moving to `blocked`.
- Terminal attempts rejecting later terminal transitions.
- Claim idempotency response persistence and replay.

Run:

```bash
go test ./...
```

## Agent Harnesses

See [Harness Integration Examples](docs/harness-integration.md) for copy-pasteable Codex, Claude Code, Gemini CLI, OpenCode, and custom-agent flows.

See [Phase 2 Closeout](docs/phase-2-closeout.md) for the REST, CLI, and MCP parity matrix, closeout test commands, and current adapter boundaries.

See [Human Operations](docs/human-operations.md) for the Phase 3 TUI-first UX contract, view order, non-goals, and Beads breakdown.
