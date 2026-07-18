# Plan 021: Make the browser gate real — fix CI database bootstrap and cover core web workflows

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 9f8d948..HEAD -- .github/workflows/ci.yml ui-tests/`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P0
- **Effort**: M
- **Risk**: LOW
- **Depends on**: none
- **Category**: tests
- **Beads**: agent-task-tracker-0v5
- **Planned at**: commit `9f8d948`, 2026-07-18

## Why this matters

The GitHub Actions `browser` job has been failing on `main` (runs 29545792976 and 29286663530): the Playwright web server boots `forge server`, which pings PostgreSQL at startup, but the workflow never creates or migrates the `forge_browser` database, so the server exits with `FATAL: database "forge_browser" does not exist`. CI on main is red while every local gate passes — the browser gate is currently decorative. Separately, the whole browser suite is a single test against `/login`; the proof-first operator workflow (queue → ticket → artifact → search) has zero browser regression coverage. This plan makes the gate green and makes it test the product.

## Current state

- `.github/workflows/ci.yml` — `browser` job starts a `postgres:16-alpine` service, installs Bun and Playwright, then runs `bun run test` in `ui-tests/` with:
  ```yaml
  env:
    FORGE_DATABASE_URL: postgres://postgres:postgres@localhost:5432/forge_browser?sslmode=disable
    FORGE_ADMIN_TOKEN: browser-test-token
    FORGE_ARTIFACT_ROOT: ${{ runner.temp }}/forge-artifacts
  ```
  There is no step that creates the `forge_browser` database or runs `forge migrate`.
- `ui-tests/playwright.config.ts` — boots the server itself:
  ```ts
  webServer: process.env.FORGE_UI_BASE_URL ? undefined : { command: 'go run ./cmd/forge server', cwd: '..', url: 'http://127.0.0.1:3017/livez', timeout: 30000, reuseExistingServer: false },
  ```
- `internal/runtime/runtime.go:52-58` — `pgxpool.New` + `pool.Ping(ctx)` on runtime open; `forge server` exits if the database is unreachable or missing.
- `ui-tests/acceptance.spec.ts` — the only test: loads `/login`, checks the heading, skip link focus, and asserts no serious/critical axe violations. It never logs in and never touches the database.
- `internal/web/handler_test.go` — extensive Go-side tests of the same routes; use it to learn the route semantics, but browser coverage is the point here.
- Login semantics (from `internal/web/handler.go`): POST `/login` with form field `admin_token`; on success a session cookie is set and the browser is redirected to `next` (default `/workspaces`). Human routes: `/workspaces`, `/workspaces/{id}`, `/tickets?workspace_id=&project_id=`, `/tickets/{id}`, `/attempts/{id}`, `/artifacts?workspace_id=&project_id=`, `/artifacts/{id}`, `/artifacts/{id}/content`, `/search?workspace_id=&project_id=&q=`, `/proposed/{ticket_id}`.
- Seeding tools (no HTTP needed): `go run ./cmd/forge workspaces create --json --name ...`, `projects create --json --workspace-id ...`, `create --json ...` (ticket), `codex claim`, `codex checkpoint`, `codex complete --proof <file>` — see the "Local Smoke Test" section of `README.md` at repo root for exact flags and the JSON fields (`.id`, `.attempt_id`, `.artifacts[0].id`).

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Create DB (local) | `createdb forge_browser_local` | exit 0 |
| Migrate | `FORGE_DATABASE_URL=... go run ./cmd/forge migrate` | exit 0, prints applied migrations |
| Browser tests (local) | `cd ui-tests && bun install && FORGE_DATABASE_URL=... FORGE_ADMIN_TOKEN=browser-test-token FORGE_ARTIFACT_ROOT=$(mktemp -d) bun run test` | all Playwright tests pass |
| Go gate untouched | `go test ./...` | all pass |

## Scope

**In scope** (the only files you should modify):
- `.github/workflows/ci.yml` (browser job only)
- `ui-tests/acceptance.spec.ts` (extend) or new `ui-tests/*.spec.ts` files
- `ui-tests/playwright.config.ts` (only if a `globalSetup` is needed)
- `ui-tests/global-setup.ts` (create, optional)
- `ui-tests/package.json` (only if a setup script is added)

**Out of scope** (do NOT touch):
- Any Go source under `cmd/` or `internal/` — the server and CLI behavior is correct; this plan only fixes CI bootstrap and adds tests.
- The `verify` CI job.
- `scripts/production-acceptance.sh` (it already tolerates missing Bun; leave as is).

## Git workflow

- Branch: `advisor/021-browser-gate`
- Commit style: short imperative sentence, e.g. `Fix browser CI database bootstrap` (match `git log --oneline`).
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Bootstrap the database in the browser CI job

In `.github/workflows/ci.yml`, before the `bun run test` step of the `browser` job, add a step that creates and migrates the database the server will ping:

```yaml
- name: Prepare browser database
  run: |
    PGPASSWORD=postgres createdb -h localhost -U postgres forge_browser
    go run ./cmd/forge migrate
  env:
    FORGE_DATABASE_URL: postgres://postgres:postgres@localhost:5432/forge_browser?sslmode=disable
```

(`createdb` is available on ubuntu-24.04 runners via the preinstalled postgresql-client; if you prefer, use `psql -c 'CREATE DATABASE forge_browser'`.)

**Verify**: reproduce the failure mode locally first — from a clean local DB name that does not exist, `FORGE_DATABASE_URL=postgres://localhost:5432/forge_browser_local?sslmode=disable go run ./cmd/forge server` → exits with `database ... does not exist`; then after `createdb forge_browser_local` + `forge migrate` the same command boots and `curl -fsS http://127.0.0.1:3017/livez` → 200.

### Step 2: Seed a deterministic workflow fixture

Add a Playwright `globalSetup` (wire it in `playwright.config.ts`) that shells out to the Forge CLI to create: one workspace, one project, one ticket, one claimed+checkpointed+completed attempt with a local proof file. Persist the created IDs to `ui-tests/.fixture.json` for tests to read. Use the exact CLI invocations from `README.md` "Local Smoke Test" (workspaces create → projects create → create → codex claim → codex checkpoint → codex complete --proof). All commands take the database from `FORGE_DATABASE_URL` env; pass `--admin-token` via env `FORGE_ADMIN_TOKEN` where required.

**Verify**: `bun run test` locally → globalSetup runs, `.fixture.json` exists and contains non-empty `workspaceId`, `projectId`, `ticketId`, `attemptId`, `artifactId`.

### Step 3: Add workflow specs

Keep the existing accessibility test. Add specs covering, at minimum:

1. **Login round-trip**: POST the admin token via the form, land on `/workspaces`, workspace from the fixture is visible; a wrong token shows "Invalid admin token." and stays on `/login`.
2. **Queue → ticket detail**: `/tickets?workspace_id=&project_id=` lists the fixture ticket; clicking it opens `/tickets/{id}` showing status, acceptance criteria, and the attempt link.
3. **Attempt and artifact proof**: `/attempts/{attempt_id}` shows the checkpoint summary; `/artifacts/{artifact_id}` shows metadata; `/artifacts/{artifact_id}/content` serves the proof file body.
4. **Search**: `/search?...&q=<fixture ticket title word>` returns the fixture ticket.
5. Run the axe check (serious/critical = none) on `/workspaces`, `/tickets` list, and `/tickets/{id}` in addition to `/login`.

**Verify**: `bun run test` → all specs pass locally against a fresh `forge_browser_local` database.

### Step 4: Prove the CI job green

Run the full local equivalent of the CI browser job from a clean state (drop and recreate `forge_browser_local`, rerun migrate, `bun run test`). If the operator allows pushing, push the branch and confirm the `browser` job passes on GitHub Actions.

**Verify**: local clean-state run passes; `gh run view <new-run-id>` shows `browser` job success (only if a push was authorized).

## Test plan

The new Playwright specs ARE the tests. Model their structure on the existing `ui-tests/acceptance.spec.ts` (single-purpose tests, role-based locators, no arbitrary sleeps — use Playwright auto-waiting). Go tests must remain untouched and passing (`go test ./...`).

## Done criteria

- [ ] `.github/workflows/ci.yml` browser job creates and migrates `forge_browser` before tests
- [ ] `bun run test` in `ui-tests/` passes from a clean database locally
- [ ] Specs exist for login, queue→detail, attempt/artifact proof, and search (grep: `grep -c "^test(" ui-tests/*.spec.ts` ≥ 5)
- [ ] `go test ./...` exits 0 with no Go files modified (`git diff --name-only | grep -c '\.go$'` → 0)
- [ ] `plans/README.md` status row updated

## STOP conditions

Stop and report back (do not improvise) if:

- The server fails to boot even after `createdb` + `forge migrate` (the boot contract has drifted from `internal/runtime/runtime.go:52-58`).
- Seeding via CLI requires flags not documented in `README.md` (the CLI contract drifted).
- Making a spec pass appears to require changing Go handler code — the finding then is a product bug; report it instead of patching around it.
- `bun install` cannot resolve the pinned Playwright version in CI.

## Maintenance notes

- Any new human web route should get: a Go handler test (existing convention) AND a browser spec here if it is part of the operator's core loop.
- Reviewers should check that specs assert on rendered content (roles/text), not raw HTML internals, so templ refactors don't produce false failures.
- Deferred: cross-browser matrix (webkit/firefox) — chromium-only is deliberate for v0.1.
