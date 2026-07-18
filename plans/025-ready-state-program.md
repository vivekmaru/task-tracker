# Plan 025: Ready-state program — take Forge from current state to production-ready

> **Executor instructions**: You are an AI agent executing this program end to
> end. Work packet by packet in the order given. Run every verification
> command and confirm the expected result before moving on. If any STOP
> condition occurs, stop that packet and report — do not improvise. Update the
> status rows in `plans/README.md` as you complete packets. Commit per packet
> with a short imperative message (match `git log --oneline`).
>
> **Drift check (run first)**: `git log --oneline -5` and compare against
> "Planned at". Then for each packet, compare its "Current state" excerpts to
> the live code before editing; on a mismatch, treat it as a STOP condition
> for that packet only.

## Status

- **Priority**: P0
- **Effort**: L (program of S/M packets)
- **Risk**: LOW-MED (each packet lists its own)
- **Depends on**: none (packets have internal ordering)
- **Category**: program
- **Planned at**: commit `9f8d948` + issue-tracking commits, 2026-07-18
- **Beads**: agent-task-tracker-0v5, -sij, -919, -4lh, -nq0, -cvd, -yqd

## Why this matters

Forge's execution core is proven: build, vet, 420 unit tests, and 20
PostgreSQL integration tests all pass; the 20-packet production-readiness
program (plans 001-020) is complete. A hands-on probe on 2026-07-18 exercised
the full product as an agent (CLI lifecycle) and as a human (web + TUI) and
found that everything remaining is **surface quality, CI truthfulness, and
deployment packaging** — no ledger-correctness defects. "Ready" means: green
CI on GitHub, the human web UI able to carry the triage loop it exists for,
a documented deploy path, and defense-in-depth on the login boundary.

## Definition of ready (program-level done criteria)

ALL must hold at the end:

- [ ] `./scripts/verify.sh` passes locally with `FORGE_TEST_DATABASE_URL` set
- [ ] GitHub Actions CI (both `verify` and `browser` jobs) green on the branch
- [ ] A blocked ticket's blocker reason is visible on the web ticket page
- [ ] Ticket action forms render without overlap at 1200px width
- [ ] `forge block <attempt-id> --reason X --category Y` (README form) works
- [ ] Playwright covers login, queue→detail, artifact proof, and search
- [ ] `deploy/` + `docs/deployment.md` exist and their commands were executed once
- [ ] Login failure throttle active with race-tested coverage
- [ ] All seven Beads issues above are closed with reasons

## Global commands

| Purpose | Command | Expected |
|---|---|---|
| Full gate | `FORGE_TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/forge_test?sslmode=disable' ./scripts/verify.sh` | exit 0 |
| Unit tests | `go test ./...` | all pass |
| Race | `go test -race ./...` | all pass |
| Web tests | `go test ./internal/web` | all pass |
| CLI tests | `go test ./internal/cli` | all pass |
| Browser tests | `cd ui-tests && bun install && bun run test` | all pass (needs a migrated local DB, see packet 1) |

Repo conventions: Go stdlib-first, no new third-party deps without strong
reason; handlers are methods on `Handler` in `internal/web/handler.go` with
inline CSS from `pageCSS()` (handler.go:2318); tests use `httptest` table
style in `internal/web/handler_test.go`; CLI commands live in
`internal/cli/cli.go` with paired tests in `cli_test.go`; commit messages are
short imperative sentences.

---

## Packet 1 (P0): Fix the red CI browser job and add real browser coverage

**Beads**: agent-task-tracker-0v5. **Full spec**: `plans/021-fix-browser-ci-and-cover-workflows.md` — read and execute it as written. Summary: CI's `browser` job fails on `main` because the `forge_browser` database is never created/migrated before `forge server` boots (verified in run 29545792976: `FATAL: database "forge_browser" does not exist`). Add a createdb+migrate step to `.github/workflows/ci.yml`, a Playwright `globalSetup` that seeds workspace/project/ticket/attempt/artifact via the CLI, and specs for login round-trip, queue→ticket detail, attempt/artifact proof, and search, keeping the existing axe accessibility check.

**Done when**: plan 021's done criteria all pass.

## Packet 2 (P0): Render the blocker reason on the web ticket page

**Beads**: agent-task-tracker-4lh. **Effort**: S. **Risk**: LOW.

A blocked ticket's page shows only `{"ticket_status": "blocked", "failure_category": "needs_human"}` in the event feed. The human-readable reason is stored in `attempts.blocker` (JSON `{"reason": ...}`) and `attempts.failure_reason`-adjacent fields, and **the TUI already renders it** — its detail view prints `Failure: <reason> (<category>)` and `Blocker: <reason>` lines. The web attempt section renders only status + agent (see the Attempts section written by the ticket detail handler in `internal/web/handler.go`; find it via `grep -n "Attempts" internal/web/handler.go`).

Steps:
1. Locate how the TUI detail assembles Failure/Blocker lines (`grep -rn "Blocker:" internal/tui/`) and which runtime/service call supplies them — reuse that data path; do not add a new query if the ticket-detail view model already carries the attempt rows with blocker fields.
2. In the web ticket detail's Attempts section, for any attempt with status `blocked` or `failed`, render the failure category and blocker/failure reason as visible text (styled like the existing `empty-text`/muted paragraph conventions). Also render it in the "Current attempt" panel when the current attempt is blocked.
3. Add a handler test in `internal/web/handler_test.go` (match existing table style): block an attempt with a known reason through the same fixtures existing tests use, GET the ticket page, assert the reason string appears in the HTML.

**Verify**: `go test ./internal/web` → passes, including the new assertion. **STOP if** the ticket-detail view model does not carry blocker data — report which service call needs extending rather than adding an ad-hoc SQL query.

## Packet 3 (P0): Fix the overlapping ticket-actions layout

**Beads**: agent-task-tracker-nq0. **Effort**: S. **Risk**: LOW.

On `/tickets/{id}`, action forms overlap and button labels clip. Measured via Playwright: each form container is 89px wide while its input and button are 147px. The markup is written by `writeTicketActionForm` (`internal/web/handler.go:1846`) inside `<div class="action-grid compact">` (handler.go:1839); the CSS comes from the inline stylesheet in `pageCSS()` (handler.go:2318). The proposed-triage page renders the same form pattern full-width and looks correct — match its layout behavior.

Steps:
1. Find the `.action-grid` / `.compact` rules inside `pageCSS()` and fix the grid so each form gets enough width (e.g. `grid-template-columns: repeat(auto-fit, minmax(180px, 1fr))` and let forms wrap) — controls must not overflow their column at 1200px and 900px viewport widths.
2. Add/extend a `shell_test.go` or handler test only if one already asserts CSS content; otherwise verify visually via the packet-1 Playwright infrastructure: extend the ticket-detail spec with a bounding-box assertion that each action button's box does not intersect its sibling form's box.

**Verify**: browser spec passes; manual screenshot at 1200px shows three separated forms with full labels ("Reopen", "Request review", "Archive"). **STOP if** fixing requires restructuring `writeTicketActionForm` markup semantics (form action URLs must not change — tests and htmx depend on them).

## Packet 4 (P0): Fix CLI attempt-command flag parsing, help, and category errors

**Beads**: agent-task-tracker-cvd. **Effort**: M. **Risk**: MED (CLI contract — keep backward compatible).

Three defects in `internal/cli/cli.go`:

(a) **Positional-then-flags silently drops flags.** `runBlockCommand` (cli.go:1188) calls `parseFlags(flags, args)` then falls back to `flags.Arg(0)` for the attempt ID. Go's stdlib `flag` stops parsing at the first non-flag argument, so `forge block <id> --reason X` — the form documented in README.md — silently ignores `--reason`. The repo already has the correct helper: `splitAttemptIDArg` (cli.go:1964) extracts the positional attempt ID first, then parses the remainder; it is used at cli.go:1017, 1679, 1727, and 1868. Steps: audit EVERY attempt-scoped command (`heartbeat`, `checkpoint`, `complete`, `fail`, `block`, `cancel`, plus `codex` variants) with `grep -n "Arg(0)" internal/cli/cli.go`; convert any that use the parse-then-Arg(0) pattern to `splitAttemptIDArg`. Add a CLI test per converted command in `cli_test.go` asserting the README form (`cmd <id> --flag value`) round-trips the flag value (model on existing tests that call the command runners with fake dependencies).

(b) **`--help` prints no flags.** `newFlagSet` (cli.go:2430) evidently installs a usage that omits `flags.PrintDefaults()`. Make subcommand help print the flag table. Verify: `go run ./cmd/forge block --help 2>&1 | grep -- --reason` → shows the flag with its description.

(c) **Invalid `--category` leaks a raw Postgres error** (`attempts_failure_category_check` SQLSTATE 23514). Valid values (from `sql/migrations/0001_initial_schema.sql:95`): `task_failed`, `blocked`, `needs_human`, `environment_failed`, `permission_required`, `dependency_missing`, `unclear_requirements`. Validate the category in the CLI (and in the service layer if other entrypoints share the gap — check the REST lifecycle route) before the DB write, with an error that lists the valid values. Add the list to the `--category` flag's help text. Test: invalid category → exit code 2 and an error naming valid values; no SQLSTATE text.

**Verify**: `go test ./internal/cli` and `go test ./...` pass; the three grep/CLI checks above. **STOP if** fixing (a) would change behavior of any currently-passing invocation form (both `--attempt-id X` and positional must keep working).

## Packet 5 (P1): Web and CLI polish cluster

**Beads**: agent-task-tracker-yqd. **Effort**: M. **Risk**: LOW. All in `internal/web/handler.go` (+ TUI file for item 6):

1. **Root route**: `GET /` currently 404s (the router's default branch). Redirect `/` to `/workspaces` (which itself bounces to `/login` when unauthenticated). Test: GET `/` → 303/302 to `/workspaces`.
2. **Favicon**: every page 404s `/favicon.ico`. Serve a small inline SVG/ICO from the assets handler (`internal/web/assets.go`, embedded like `htmx-2.0.4.min.js`) and reference it from the shell `<head>` written at handler.go:1724. Test: GET `/favicon.ico` → 200.
3. **Activity feed attribution and order** (`/events` page): each entry shows only event kind + actor + raw JSON with a bare "Ticket" link — add the ticket title to each entry (the events query/view model may need a join; check `EventService`/`ListEvents` in `internal/runtime` and the SQL in `sql/queries/`), and render newest-first to match the "Recent" framing (keep cursor semantics consistent).
4. **Attempt detail depth** (`/attempts/{id}`): the page shows only ID/ticket/summary/artifacts. Add the checkpoints timeline and the submitted metrics (tokens in/out, cost USD, duration) — this data is already collected (`forge codex complete --tokens-in ... --cost-usd ...`) and appears in `forge analytics`, but is invisible on the web. Reuse the ticket-detail checkpoint rendering.
5. **Actor formatting**: "human/-" and "codex/" render with dangling slashes when the id/model half is empty; render just the non-empty part.
6. **TUI stale selection**: while `/` filtering in the queue view, the "Selected" preview keeps the previously highlighted ticket even when it's filtered out (`internal/tui/`); reset selection to the first visible row when the filter changes.

Each item: extend the nearest existing test (handler_test.go for 1-5, TUI test for 6). **Verify**: `go test ./...`; manual spot-checks per item. These are six independent commits — do not batch into one.

## Packet 6 (P1): Login failure throttle

**Beads**: agent-task-tracker-sij. **Full spec**: `plans/022-login-throttle-and-session-hardening.md` — execute as written. Summary: global fixed-window throttle on failed `/login` POSTs (10/minute, 429 over limit, injectable clock via existing `AuthOptions.Now`), plus a README paragraph documenting admin-token rotation as the session revocation path.

## Packet 7 (P1): Production deployment packet

**Beads**: agent-task-tracker-919. **Full spec**: `plans/023-production-deployment-packet.md` — execute as written. Summary: `deploy/compose/` (postgres + migrate one-shot + server + worker + Caddy TLS), `deploy/systemd/` hardened units, `deploy/backup/` pg_dump script + timer, and `docs/deployment.md`, all with placeholder-only secrets, verified by actually booting the compose stack.

---

## Execution order and rationale

1 → 2 → 3 → 4 → 5 → 6 → 7. Packet 1 first so every later packet lands on a
green CI signal; 2-4 are the defects that block the human triage loop and the
documented agent flow; 5-7 harden and package. Packets 2/3 both touch
`internal/web/handler.go` — do them serially, not in parallel worktrees.
After packet 3, the browser suite from packet 1 gains the bounding-box
assertion, so re-run `bun run test` at the end of every later packet.

## What is explicitly NOT in scope

- The production dogfood pilot (`plans/024-production-dogfood-pilot.md`) — it
  is operational, runs after this program, and is the final go/no-go gate.
- JSON envelope redesign for CLI output — noted in agent-task-tracker-yqd but
  breaking output shapes needs a versioning decision from the maintainer;
  leave envelopes as-is beyond the null-status fix if it proves trivial.
- Prometheus/OTel endpoint, server-side sessions, API rate limiting, React
  rewrite, RBAC — all recorded as rejected in `plans/README.md`.

## Program STOP conditions

- Any packet's fix requires schema migration — stop, report; migrations need
  operator review per `docs/release-and-recovery.md`.
- `./scripts/verify.sh` fails for a reason unrelated to your change — stop;
  the baseline was green at planning time.
- A change would alter REST/MCP response shapes — stop; external agents
  depend on them (`docs/phase-2-closeout.md` parity matrix).

## Session close (mandatory, from CLAUDE.md)

After the final packet: close the seven Beads issues with reasons, update all
status rows in `plans/README.md`, run the full gate one last time, then
`git pull --rebase && git push` until `git status` shows up to date.
