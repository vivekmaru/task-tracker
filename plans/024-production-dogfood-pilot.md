# Plan 024: Run the production dogfood pilot and render the go/no-go verdict

> **Executor instructions**: This is an operational plan, not a code plan —
> the executor is the operator (possibly assisted by agents). Follow it step
> by step; record evidence for every claim. If anything in "STOP conditions"
> occurs, stop and report. When done, update the status row in
> `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat 9f8d948..HEAD -- docs/ scripts/production-acceptance.sh README.md`
> On drift, re-read the changed docs before proceeding; the pilot protocol
> below must not contradict them.

## Status

- **Priority**: P0
- **Effort**: L (elapsed time; low hands-on)
- **Risk**: LOW (pilot data only; production candidate DB must be backed up per runbook)
- **Depends on**: plans/023-production-deployment-packet.md; plan 021 (green CI) should land first so the release gate is honest
- **Category**: direction
- **Beads**: agent-task-tracker-dxl
- **Planned at**: commit `9f8d948`, 2026-07-18

## Why this matters

Everything up to here proves mechanics: the 20-packet production-readiness program is complete, the quality gate is comprehensive, and `scripts/production-acceptance.sh` passes from a clean environment in ~32 seconds (see `docs/production-acceptance-report.md`). What has never happened is Forge operating as a production system: a deployed instance, real agent workload over days, and the PRD's own success metrics measured from ledger data. The PRD (§19) explicitly says early success is measured by execution correctness and adoption, not features. This pilot is the go/no-go gate for calling Forge "in production," and its friction output seeds the next product wave (per `docs/phase-5-closeout.md` "Recommended Next Wave": start from real dogfood friction).

## Current state

- Deployment examples and checklist: `docs/deployment.md` + `deploy/` (produced by plan 023).
- Acceptance mechanics: `scripts/production-acceptance.sh` (quality gate, integration races, recovery drill, browser smoke; writes a redacted report).
- Metrics sources already shipped: `forge analytics summary|by-model|by-harness|trends` (CLI), `/api/v1` analytics routes, and the append-only `ticket_events` ledger in Postgres.
- PRD success metrics to measure — `PRD.md:1875-1907`:
  - 19.1 correctness: double-claim rate = 0; stale claim recovery works; P50 claim-next < 800ms; P95 < 2s; completion rate; expired attempt rate; attempts per completed ticket.
  - 19.2 product: tickets per active project; attempts per week; distinct harnesses; % completed tickets with artifacts; % completed attempts with metrics; % agent-created tickets accepted; % attempts with verification evidence; blocked attempts by category; time-to-current-state in UI; actions per common flow.
- Harness integration recipes for the agents that will generate load: `docs/harness-integration.md` (Codex, Claude Code, Gemini CLI, OpenCode, custom).

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Acceptance gate | `FORGE_TEST_DATABASE_URL=... FORGE_RECOVERY_DATABASE_URL=... ./scripts/production-acceptance.sh` | "production acceptance passed" |
| Deploy | per `docs/deployment.md` checklist | `/readyz` 200 |
| Metrics | `forge analytics summary --json --workspace-id ... --project-id ...` | JSON with attempt counts/metrics |
| Ledger queries | `psql "$FORGE_DATABASE_URL"` read-only queries against `tickets`, `attempts`, `ticket_events` | rows |

## Scope

**In scope**:
- One deployed Forge instance (compose or systemd, per plan 023) with HTTPS, secure cookies, and scheduled backups enabled.
- Real work routed through it (this repo's own development is the natural workload — Forge-on-Forge).
- `docs/production-pilot-report.md` (create — redacted like `docs/production-acceptance-report.md`: no URLs, tokens, ticket bodies).
- Friction tickets filed IN the pilot Forge instance and mirrored to Beads (`bd create`) for anything requiring code changes.

**Out of scope**:
- Any code change during the pilot window beyond P0 fixes (a code freeze keeps the measurement honest; file, don't fix).
- New metrics infrastructure — measure with existing analytics CLI + read-only SQL only.

## Pilot protocol (the steps)

### Step 1: Entry gate

Green CI on main (plan 021 landed), `./scripts/production-acceptance.sh` passes, deploy per `docs/deployment.md`, backups verified once by restoring into a scratch database (`scripts/recovery-smoke.sh` pattern).

**Verify**: acceptance output, `/readyz` 200, one successful restore drill; record all three in the report skeleton.

### Step 2: Define the window and workload

Window: 14 calendar days OR until 50 tickets reach a terminal state, whichever is later. Workload: all agent-executed development tasks for this repository are created as Forge tickets and claimed via `forge claim-next`/`forge codex claim` by at least 2 distinct harnesses (e.g. Codex + Claude Code, per `docs/harness-integration.md`). Human triage happens in the web UI/TUI only — no side channels.

**Verify**: pilot charter section written in the report before day 1.

### Step 3: Operate and observe

Daily: skim `/tickets` queue and `forge analytics summary`. Log every operator friction moment (>3 actions for a common flow, confusing state, missing link, slow page) as a ticket tagged `pilot-friction`. Weekly: run the backup restore drill once.

### Step 4: Measure

At window close, compute each PRD §19.1 and §19.2 metric from the ledger with read-only SQL and the analytics CLI. Two require instrumented observation rather than SQL — "time from opening UI to finding current work state" and "actions for common inspect/approve flows": measure by stopwatch/screen-recording on 3 representative flows and record medians.

**Verify**: every metric in the report has either a number or an explicit "not measurable because X".

### Step 5: Verdict and handoff

Go criteria (ALL must hold): zero double-claims in the ledger (`SELECT` on overlapping running attempts per ticket → 0 rows); P95 claim-next latency < 2s; zero data-loss incidents; backups restorable; no unresolved P0 friction. Render GO (Forge is production for this team) or NO-GO (list blocking findings). Either way: file the top 5 friction items as prioritized Beads issues, and propose the next product wave grounded in them.

**Verify**: `docs/production-pilot-report.md` complete and redacted; friction issues exist in `bd list`.

## Done criteria

- [ ] Pilot ran its full defined window on a deployed instance with HTTPS + backups
- [ ] `docs/production-pilot-report.md` exists with every §19.1/§19.2 metric addressed and a GO/NO-GO verdict
- [ ] Zero-double-claim query result recorded in the report
- [ ] ≥ 2 distinct harnesses appear in `forge analytics by-harness`
- [ ] Top friction items filed in Beads with priorities
- [ ] `plans/README.md` status row updated

## STOP conditions

- Any suspected data loss or double-claim during the pilot: stop the pilot clock, snapshot the database, investigate before continuing.
- The deployment packet (plan 023) proves unusable for a real deploy: stop and route back to plan 023 with specifics.
- Backup restore drill fails: stop; a system whose backups don't restore must not carry the pilot.

## Maintenance notes

- The pilot report becomes the baseline: rerun the metric queries after major releases and diff.
- The friction backlog — not this advisor's guesses — should drive the next roadmap wave (Phase 5 closeout says the same).
