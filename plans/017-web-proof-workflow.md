# Plan 017: Finish the proof-first web workflow

> **Executor instructions**: Optimize the journey `queue → ticket → attempt/checkpoint → proof → intervention`. Do not add boards, sprint views, or generic project-management administration.
>
> **Drift check**: `git diff --stat 1601f86..HEAD -- internal/web internal/runtime internal/services sql/queries internal/integration docs/human-operations.md`

## Status

- **Priority**: P1
- **Effort**: L
- **Risk**: MED
- **Depends on**: Plans 006 and 016
- **Category**: ui, performance
- **Planned at**: commit `1601f86`, 2026-07-12
- **Beads**: `agent-task-tracker-vds.17`

## Why this matters

The canonical ticket page omits checkpoints, bounded history, and clear current-attempt/blocker emphasis. Ticket, search, and artifact lists expose only the first bounded slice with no pager. Destructive and productive actions share equal emphasis. These failures prevent the UI from answering whether agent work can be trusted.

## Current state

- `internal/web/handler.go:996-1014` loads attempts, events, and artifacts sequentially but omits checkpoints.
- `handler.go:1293-1403` renders one list slice without previous/next navigation.
- `handler.go:1430-1457` renders basic context/actions/timeline rather than the mockup's trust hierarchy.
- `handler.go:1523-1525` confirms artifact deletion, but ticket archive and proposal reject submit immediately.
- Attempt/checkpoint/event queries return unbounded ticket history.

## Commands

| Purpose | Command | Expected |
|---|---|---|
| Web/runtime | `rtk go test ./internal/web ./internal/runtime ./internal/services` | pass |
| Integration | `rtk go test -tags=integration ./internal/integration -run 'TestWebProof|TestWebPagination'` | pass |
| Full gate | `rtk ./scripts/verify.sh` | exit 0 |

## Scope

**In scope**: bounded ticket inspection read model, checkpoint/current-attempt/blocker/proof rendering, list pagination, safe action hierarchy/confirmation, scope-preserving links, tests, UX docs.

**Out of scope**: charts, realtime/WebSockets, inline rich editing, new analytics, visual theme overhaul, or accessibility browser suite owned by Plan 018.

## Git workflow

- Branch: `feat/production-017-web-proof-workflow`
- Commit: `Finish proof-first web workflow`

## Steps

1. Introduce a shared `TicketInspection` read model containing ticket, current attempt, bounded recent prior attempts/checkpoints/events/artifacts, total counts, cursors/offsets, and section-level errors. Avoid all-or-nothing page failure.
2. Add bounded queries or request parameters for each history section. Default to recent evidence; show total counts and explicit `View older` links/cursors.
3. Render ticket hierarchy in this order: identity/state, current attempt and lease, blocker, acceptance/verification, latest checkpoint, proof artifacts, prior attempts, event history, proposed follow-up, actions.
4. Add checkpoints with summary, files, commands, risk, next step, and attempt link. Make local/S3 proof opening behavior explicit.
5. Implement `limit+1` pagination for ticket, search, artifact, proposed, and applicable event lists. Preserve scope/filter query parameters and show visible range/next/previous state.
6. Classify actions as primary, secondary, or destructive. Separate archive/reject, require a reason where domain rules benefit, and add confirmation without making ordinary transitions modal-heavy.
7. Add integration fixtures with more than one page and long ticket history; assert no evidence becomes unreachable.

## Done criteria

- [ ] Ticket detail includes current attempt, blocker, checkpoints, proofs, and prior history.
- [ ] Every bounded list has discoverable navigation.
- [ ] History fetch/render cost is bounded by default.
- [ ] Destructive actions are distinct and confirmed.
- [ ] Partial section failure preserves the rest of the page with recovery guidance.

## STOP conditions

- Bounded history would hide evidence without a total count and expansion path.
- A new read model duplicates mutation/business logic.
- UX work requires realtime infrastructure to be usable.

## Maintenance notes

The ticket inspection model should serve future TUI or protocol consumers only if it stays transport-neutral. Reviewers should scrutinize query counts and scope preservation.

