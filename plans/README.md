# Forge Production Readiness Implementation Plans

Generated from the production-readiness audit on 2026-07-12 at commit `1601f86`. Beads epic `agent-task-tracker-vds` is the durable task source of truth; these files are self-contained executor specifications.

These are agent-sized branches/PRs, not calendar-week phases. Independent packets may run in parallel after their dependencies are satisfied. Each executor must read its plan fully, run the drift check, obey STOP conditions, run all verification commands, update this index, close its Beads issue only after completion, and push its branch before handoff.

## Execution order and status

| Plan | Beads | Packet | Priority | Size | Depends on | Status |
|---|---|---|---|---|---|---|
| 001 | vds.1 | Production quality gate | P0 | S | — | DONE |
| 002 | vds.2 | Fix attempt cancellation | P0 | S | 001, 003 | DONE |
| 003 | vds.3 | PostgreSQL integration harness | P0 | M | 001 | DONE |
| 004 | vds.4 | Fence lease expiry | P0 | M | 003 | DONE |
| 005 | vds.5 | Transactional ticket writes | P0 | M | 003 | DONE |
| 006 | vds.6 | Transactional decomposition and terminal writes | P0 | M | 005 | DONE |
| 007 | vds.7 | Authentication boundary | P0 | M | 001 | DONE |
| 008 | vds.8 | Webhook security and fencing | P0 | L | 003, 007 | DONE |
| 009 | vds.9 | Migration and sqlc integrity | P0 | M | 003 | DONE |
| 010 | vds.10 | Relational scope integrity | P0 | L | 009 | DONE |
| 011 | vds.11 | Typed REST resource routes | P0 | L | 007, 009 | DONE |
| 012 | vds.12 | REST execution lifecycle | P0 | L | 004, 006, 011 | DONE |
| 013 | vds.13 | MCP stdio server | P1 | M | 012 | DONE |
| 014 | vds.14 | Production server envelope | P0 | M | 007 | DONE |
| 015 | vds.15 | Release and recovery path | P1 | L | 001, 009, 014 | DONE |
| 016 | vds.16 | Web shell and scope | P1 | L | 001 | DONE |
| 017 | vds.17 | Proof-first web workflow | P1 | L | 006, 016 | DONE |
| 018 | vds.18 | Web accessibility/browser tests | P1 | M | 016, 017 | DONE |
| 019 | vds.19 | TUI operator workflow | P1 | M | 001, 003 | DONE |
| 020 | vds.20 | Production acceptance pilot | P0 | M | 002, 008, 010, 012, 013, 015, 018, 019 | DONE |

Status values: `TODO`, `IN PROGRESS`, `DONE`, `BLOCKED: reason`, `REJECTED: rationale`.

## Reconcile 2026-07-18 (commit `9f8d948`)

Plans 001-020 verified DONE on current HEAD: `go build`, `go vet`, `go test ./...` (420 tests), and `go test -tags=integration ./internal/integration` (20 tests against real PostgreSQL) all pass; epic `agent-task-tracker-vds` closed 20/20. One DONE caveat: the GitHub Actions `browser` job is red on `main` (the `forge_browser` database is never created before `forge server` boots — see plan 021), so plan 018's browser gate passes locally but not in CI.

## Production launch wave (2026-07-18 audit)

Written non-interactively from the follow-up audit; top findings by leverage were planned by default. Beads issues are the durable task source.

| Plan | Beads | Packet | Priority | Size | Depends on | Status |
|---|---|---|---|---|---|---|
| 021 | 0v5 | Fix browser CI bootstrap and cover core web workflows | P0 | M | — | DONE |
| 022 | sij | Login failure throttle and session revocation docs | P2 | S | — | TODO |
| 023 | 919 | Production deployment packet (compose/systemd/TLS/backups) | P1 | M | — | TODO |
| 024 | dxl | Production dogfood pilot and go/no-go verdict | P0 | L | 023 (and 021 for an honest release gate) | TODO |

Recommended order: 021 → 023 → 022 (anytime) → 024.

### Ready-state program (2026-07-18 UI probe)

A hands-on probe (CLI as agent; web + TUI as human) confirmed the execution
core and found surface defects. `plans/025-ready-state-program.md` is the
single handover program for an executor agent: it sequences 021/022/023 with
four new probe-driven packets. Execute 025 instead of picking individual
plans below when running the full path to ready.

| Plan | Beads | Packet | Priority | Size | Depends on | Status |
|---|---|---|---|---|---|---|
| 025 | 0v5, sij, 919, 4lh, nq0, cvd, yqd | Ready-state program (packets 1-7) | P0 | L | — | IN PROGRESS (packets 1-5 done) |

Probe defects folded into 025: web blocker reason not rendered (4lh), ticket
action forms overlap (nq0), CLI positional-arg flag drops + help + category
errors (cvd), polish cluster incl. root-route 404, favicon, activity feed
attribution, attempt metrics (yqd).

### Additional findings considered and rejected (2026-07-18)

- Prometheus/OTel metrics endpoint: `/livez`, `/readyz`, structured logs, and the webhook observability export cover v0.1 single-tenant needs; revisit only if the dogfood pilot (024) shows an operational gap.
- Server-side session store/revocation: stateless HMAC sessions are acceptable for the trusted single-operator model; token rotation is the documented revocation path (plan 022 documents it).
- API rate limiting on `/api/v1`: trusted single-tenant bearer boundary; deferred with RBAC.
- Web/TUI visual polish push: README self-identifies the gap, but it is product iteration, not a production blocker; let pilot friction drive it.

## Practical execution waves

With multiple isolated branches/worktrees, the maximum useful concurrency is:

1. **Wave A**: 001.
2. **Wave B** after 001: 003, 007, 016.
3. **Wave C**: 002, 004, 005, 008, 009, 014, 019 as their direct dependencies clear.
4. **Wave D**: 006, 010, 011, 015, 017.
5. **Wave E**: 012 and 018.
6. **Wave F**: 013.
7. **Release gate**: 020.

Do not parallelize packets that modify the same hotspot unless they are isolated in worktrees and deliberately rebased in dependency order. In particular, 005→006, 011→012, and 016→017→018 are intentional serial chains.

## Coverage of the 12 audit findings

- Quality gate and Go advisories: 001.
- Broken cancellation: 002.
- Real PostgreSQL correctness: 003.
- Lease race: 004.
- Partial multi-write commits: 005-006.
- API authentication, unsafe defaults, and CSRF: 007.
- Webhook SSRF, stale ownership, and retention: 008.
- Migration/sqlc drift and relational scope: 009-010.
- Placeholder REST and non-serving MCP: 011-013.
- Process, release, and recovery readiness: 014-015.
- Broken web workflow and maintainability/accessibility: 016-018.
- Non-scrollable/incomplete TUI: 019.
- Product validation and release proof: 020.

## Decisions intentionally preserved

- Go monolith, PostgreSQL correctness, templ/htmx web, and Bubble Tea TUI remain.
- v0.1 is trusted, single-tenant, self-hosted.
- No React/Next rewrite, mandatory Redis, event bus, WebSockets, enterprise RBAC, or advanced coordination expansion.
- UI remains dense, calm, proof-first, keyboard-friendly, and anti-Jira.
- At-least-once webhook delivery remains; fencing prevents stale state regression but does not claim exactly-once delivery.

## Findings considered and rejected

- React/Next rewrite: conflicts with the intentional architecture and adds no production prerequisite.
- Redis or a dedicated event bus: unnecessary for the current PostgreSQL-backed correctness model.
- Realtime dashboard infrastructure: not required for the v0.1 inspection workflows.
- Enterprise multi-tenant RBAC: deferred until trusted single-tenant production behavior is proven.
- Advanced analytics or learned routing: deferred until the production acceptance pilot creates real usage evidence.
