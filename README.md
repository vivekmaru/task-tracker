# Forge

Forge is a pull-based work ledger for autonomous AI agents.

Forge currently includes the execution core, JSON-first CLI commands, Codex-oriented convenience commands, a Bubble Tea TUI, server-rendered web views, local and S3-compatible proof artifact storage, search, basic attempt analytics, and a first external observability export foundation.

## Current Status

Implemented:

- Config loading from JSON file and environment.
- Postgres migration for workspaces, projects, tickets, dependencies, attempts, checkpoints, events, artifacts, idempotency keys, API keys, capabilities, and metrics.
- sqlc-generated query layer.
- Ticket create/propose/list/update services.
- Transactional `claim-next` query and claim context bundle hydration.
- Heartbeat, checkpoint, terminal attempt transitions, lease expiry transition, and idempotency cleanup worker.
- Claim idempotency replay for stable retry keys.
- Local and S3-compatible proof artifact upload, metadata registration, and human web access.
- JSON-first CLI commands over the shared runtime.
- JSON-first workspace/project setup commands for agent-friendly local bootstrapping.
- Codex harness commands for claim, checkpoint, complete, propose/follow-up, and block flows.
- Proposed-work CLI triage for listing proposals and marking them ready, enqueueing, rejecting, or archiving.
- Huma OpenAPI route registration under `/api/v1`.
- Server-rendered web pages for login, workspaces, projects, ticket queues, ticket detail, attempt detail, artifact access, proposed work, and search.
- Runtime-backed web actions for proposed-work triage and ticket lifecycle decisions.
- Bubble Tea TUI for scoped queue inspection and ticket detail handoff links.
- Basic attempt analytics by summary, model, and harness.
- Structured observability webhook payloads for ticket events, attempt metadata, and attempt metrics.
- JSON-first CLI and API management for observability webhook subscriptions.
- Correctness regression tests across services, CLI, web, storage, runtime, and contracts.

Known current limitations:

- `forge server` starts the HTTP router with the OpenAPI surface and human web inspection pages. `forge worker` runs the maintenance and webhook delivery workers in a long-lived loop; use `forge worker --once` for deterministic smoke checks.
- Observability export sink management UI and OpenTelemetry-native exporters are not implemented yet.
- The TUI and web UI are usable, but still early. They are not yet at the full "beautiful, low-friction" product bar.

## Requirements

- Go 1.26+
- PostgreSQL with `pgcrypto`
- `psql`
- Optional but useful for the smoke test snippets: `jq`
- Optional: `sqlc` via `go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1`
- PostgreSQL integration tests additionally require a role that can create and drop databases.

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
export FORGE_ARTIFACT_BACKEND=local
```

Equivalent config file:

```json
{
  "database_url": "postgres://localhost:5432/forge?sslmode=disable",
  "http_addr": "127.0.0.1:3017",
  "worker_concurrency": 1,
  "admin_token": "replace-with-a-generated-32-byte-token",
  "auth_cookie_secure": false,
  "artifact_root": ".forge/artifacts",
  "artifact_backend": "local"
}
```

Run `forge init` without `--admin-token` to generate a 32-byte token in a mode-0600 config file; the command intentionally never prints that secret. Set `FORGE_AUTH_COOKIE_SECURE=true` or `"auth_cookie_secure": true` when the human web UI is served through HTTPS, including HTTPS termination in front of the local server. Non-loopback servers require secure cookies and reject the former development placeholder.

Local artifact URLs under `local://artifacts/...` resolve inside `FORGE_ARTIFACT_ROOT`, so a proof registered as `local://artifacts/go-test-output.txt` can be inspected from the human `/artifacts/{id}` route and opened from `/artifacts/{id}/content` when that file exists under the configured artifact root. Relative config-file artifact roots resolve from the config file directory; the built-in default resolves under the user's home directory.

Set `FORGE_ARTIFACT_BACKEND=s3` or `"artifact_backend": "s3"` to store filesystem proofs in an S3-compatible bucket instead. The S3 backend uses the AWS SDK credential chain by default, or explicit static credentials when `FORGE_S3_ACCESS_KEY_ID` and `FORGE_S3_SECRET_ACCESS_KEY` are set. S3-compatible providers can be configured with `FORGE_S3_ENDPOINT`, `FORGE_S3_REGION`, `FORGE_S3_BUCKET`, `FORGE_S3_PREFIX`, and `FORGE_S3_USE_PATH_STYLE=true`. Equivalent JSON keys are `s3_endpoint`, `s3_region`, `s3_bucket`, `s3_prefix`, `s3_access_key_id`, `s3_secret_access_key`, `s3_session_token`, and `s3_use_path_style`.

With either backend, `forge codex complete --proof ./go-test-output.txt` and `forge codex block --proof ./blocked.log` copy filesystem proofs into the selected artifact store before registering them. Existing URL proofs such as `local://artifacts/go-test-output.txt` or `s3://forge-bucket/proofs/go-test-output.txt` are registered directly with the matching storage backend.

External observability export uses scoped webhook subscriptions in Postgres and posts `forge.observability.v1` JSON payloads through the durable webhook delivery worker. See [Observability Export Foundation](docs/observability-export.md) for subscription setup, payload shape, signing, retry behavior, and current limits.

Pass it with:

```bash
forge server --config forge.json
forge worker --config forge.json
forge worker --config forge.json --once
```

## Database Setup

Create a database:

```bash
createdb forge
```

Apply all migration `Up` sections:

```bash
go run ./cmd/forge migrate
```

If you already have a local Forge database created before `forge migrate` tracked applied migrations, adopt the existing schema once before applying new migrations:

```bash
go run ./cmd/forge migrate --baseline-existing
```

Regenerate sqlc code after query changes:

```bash
go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate
```

Run the PostgreSQL integration suite against disposable databases. The URL must name a database beginning with `forge_test`; Forge connects to the server's `postgres` maintenance database and creates a unique `forge_test_*` database for each test, then drops it during cleanup.

```bash
export FORGE_TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/forge_test?sslmode=disable'
go test -tags=integration ./internal/integration
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

## Local Smoke Test

This path exercises the CLI, Codex convenience commands, local proof artifact storage, search, analytics, TUI, and web UI.

Create and migrate a disposable local database:

```bash
createdb forge_smoke
export FORGE_DATABASE_URL='postgres://localhost:5432/forge_smoke?sslmode=disable'
export FORGE_ADMIN_TOKEN='change-me-local-admin-token'
export FORGE_ARTIFACT_ROOT="$PWD/.forge/artifacts"

go run ./cmd/forge migrate
```

Create a config file:

```bash
go run ./cmd/forge init \
  --path forge.local.json \
  --database-url "$FORGE_DATABASE_URL" \
  --admin-token "$FORGE_ADMIN_TOKEN" \
  --artifact-root "$FORGE_ARTIFACT_ROOT"
```

Create a workspace and project through Forge:

```bash
WORKSPACE_JSON=$(go run ./cmd/forge workspaces create --config forge.local.json --json \
  --name "Smoke Workspace")
WORKSPACE_ID=$(printf '%s' "$WORKSPACE_JSON" | jq -r '.id')

PROJECT_JSON=$(go run ./cmd/forge projects create --config forge.local.json --json \
  --workspace-id "$WORKSPACE_ID" \
  --name "Smoke Project")
PROJECT_ID=$(printf '%s' "$PROJECT_JSON" | jq -r '.id')
```

Start the web server in one terminal:

```bash
go run ./cmd/forge server --config forge.local.json
```

In another terminal, create, claim, checkpoint, and complete a ticket with a local proof file:

```bash
TICKET_JSON=$(go run ./cmd/forge create --config forge.local.json --json \
  --workspace-id "$WORKSPACE_ID" \
  --project-id "$PROJECT_ID" \
  --title "Smoke ticket" \
  --type bug \
  --description "Verify the local Forge smoke path" \
  --acceptance "Smoke ticket can be claimed and completed" \
  --verify "go test ./...")
TICKET_ID=$(printf '%s' "$TICKET_JSON" | jq -r '.id')

CLAIM_JSON=$(go run ./cmd/forge codex claim --config forge.local.json \
  --workspace-id "$WORKSPACE_ID" \
  --project-id "$PROJECT_ID" \
  --agent-id codex \
  --capability codegen \
  --capability testing \
  --lease 30m)
ATTEMPT_ID=$(printf '%s' "$CLAIM_JSON" | jq -r '.attempt_id')

go run ./cmd/forge codex checkpoint --config forge.local.json "$ATTEMPT_ID" \
  --summary "Smoke path reached checkpoint" \
  --progress 50 \
  --file README.md \
  --command "go test ./..."

printf 'smoke proof ok\n' > smoke-proof.txt
COMPLETE_JSON=$(go run ./cmd/forge codex complete --config forge.local.json "$ATTEMPT_ID" \
  --summary "Smoke path completed" \
  --proof smoke-proof.txt \
  --tokens-in 12 \
  --tokens-out 7 \
  --cost-usd 0.001 \
  --duration 2s)
ARTIFACT_ID=$(printf '%s' "$COMPLETE_JSON" | jq -r '.artifacts[0].id')
```

Check the CLI and web surfaces:

```bash
go run ./cmd/forge list --config forge.local.json --json \
  --workspace-id "$WORKSPACE_ID" \
  --project-id "$PROJECT_ID"

go run ./cmd/forge analytics summary --config forge.local.json --json \
  --workspace-id "$WORKSPACE_ID" \
  --project-id "$PROJECT_ID"

go run ./cmd/forge tui --config forge.local.json \
  --workspace-id "$WORKSPACE_ID" \
  --project-id "$PROJECT_ID"
```

Open these URLs:

- `http://127.0.0.1:3017/login` and sign in with the `admin_token` from your mode-0600 config file.
- `http://127.0.0.1:3017/workspaces`
- `http://127.0.0.1:3017/tickets?workspace_id=$WORKSPACE_ID&project_id=$PROJECT_ID`
- `http://127.0.0.1:3017/search?workspace_id=$WORKSPACE_ID&project_id=$PROJECT_ID&q=smoke`
- `http://127.0.0.1:3017/artifacts?workspace_id=$WORKSPACE_ID&project_id=$PROJECT_ID`
- `http://127.0.0.1:3017/artifacts/$ARTIFACT_ID`
- `http://127.0.0.1:3017/artifacts/$ARTIFACT_ID/content`

Expected results:

- Login redirects to `/workspaces` without console errors.
- The scoped ticket queue shows the completed smoke ticket.
- Search finds the smoke ticket.
- The artifact detail route shows metadata, and the content route serves `smoke proof ok`.
- Analytics summary reports one attempt with the token metrics above.

## Runtime Commands

The process commands open the shared runtime. `forge server` listens on `http_addr` and exposes `/api/v1/openapi.json`, `/login`, `/workspaces`, `/tickets`, and `/tickets/{id}`. Human web views require `admin_token`; cookie-authenticated changes require a same-origin browser request. Every `/api/v1` route requires `Authorization: Bearer $FORGE_ADMIN_TOKEN` or `X-Forge-Admin-Token`; browser session cookies are not accepted there.

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
- `/artifacts?workspace_id={workspace_id}&project_id={project_id}` lists artifacts for a workspace/project scope, with optional `ticket_id` filtering.
- `/artifacts/{artifact_id}` shows artifact metadata and open/delete actions. `/artifacts/{artifact_id}/content` streams locally stored artifact content. Local artifact deletion removes the stored object before removing metadata; remote artifact deletion is intentionally constrained until Forge owns safe remote object cleanup.
- `/search?workspace_id={workspace_id}&project_id={project_id}&q={query}` searches ticket execution context.
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

1. Create a workspace and project with `forge workspaces create` and `forge projects create`, or from `/workspaces`.
2. Create tickets with `forge create --json`.
3. Have an agent claim eligible work with `forge claim-next --json`.
4. Heartbeat during execution.
5. Write at least one checkpoint with summary, files touched, commands run, and next step.
6. Complete, fail, or block the attempt.
7. Attach proof metadata such as test output, diff path, diagnostic log, or final response.
8. List tickets and inspect ticket/attempt details.
9. Retry `claim-next` with the same idempotency key and request to verify replay behavior.

## Correctness Checks

Regression tests cover:

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
