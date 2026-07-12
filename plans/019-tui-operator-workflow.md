# Plan 019: Finish the keyboard-first TUI operator workflow

> **Executor instructions**: Keep the first production TUI inspection-first. Do not add mutating actions until their interaction model is separately planned and approved.
>
> **Drift check**: `git diff --stat 1601f86..HEAD -- internal/tui internal/cli/cli.go go.mod go.sum docs/human-operations.md`

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: MED
- **Depends on**: Plans 001 and 003
- **Category**: ui, feature
- **Planned at**: commit `1601f86`, 2026-07-12
- **Beads**: `agent-task-tracker-vds.19`

## Why this matters

The queue renders all rows and the detail view renders all history into unbounded strings, with no viewport or resize handling. Advertised copy/filter behavior is missing and detail loading has no feedback. Real production histories make the primary operator surface inaccessible.

## Current state

- `internal/tui/queue.go:48-58` stores no dimensions, viewport, filter, loading, or refresh state.
- `queue.go:149-187` ignores `tea.WindowSizeMsg`.
- `queue.go:190-263` renders every ticket and advertises unimplemented keys.
- `internal/tui/detail.go:47-113` renders the full history with no scrolling.
- The product contract requires move, filter, open, copy, refresh, and quit with dense calm output.

## Commands

| Purpose | Command | Expected |
|---|---|---|
| TUI tests | `rtk go test ./internal/tui ./internal/cli` | pass |
| Race | `rtk go test -race ./internal/tui` | pass |
| Full gate | `rtk ./scripts/verify.sh` | exit 0 |

## Scope

**In scope**: Bubbles list/viewport or equivalent, terminal sizing, scroll/page behavior, async loading/error state, refresh, text/status/type filtering, copy via OSC52 with fallback, key map/help, tests, docs.

**Out of scope**: claim/unblock/archive mutations, mouse-only interaction, realtime push, multi-pane pixel matching, or broad theme redesign.

## Suggested executor toolkit

- Follow Bubble Tea/Bubbles model-update-view conventions already used in the repo.
- Reuse the existing OSC52 dependency if compatible rather than adding a second clipboard library.

## Git workflow

- Branch: `feat/production-019-tui-workflow`
- Commit: `Finish TUI operator workflow`

## Steps

1. Introduce explicit width/height and queue/detail viewport state. Handle resize, keep the selected row visible, and reserve predictable space for title, preview, and help.
2. Make detail a scrollable viewport with position feedback and keys for line/page/top/bottom navigation. Long unbroken IDs and URLs must wrap or truncate with a copy path.
3. Store the lister and options needed for async refresh. Show loading immediately, ignore stale responses using request sequence IDs, and preserve selection/filter where possible.
4. Implement `/` text search, `f` status/type filter mode, `r` refresh, `c` copy selected ticket ID/link, `enter` open, `b` back, `?` help, and `q` quit. Advertise only implemented keys for the current state.
5. Add tests using `tea.WindowSizeMsg`, 1/50/200-ticket queues, long descriptions/history, slow/stale detail loads, empty/error states, copy success/fallback, filter reset, and repeated resize.
6. Update human operations docs with the final key map and terminal-size support.

## Done criteria

- [ ] Every queue row and detail section is reachable at supported terminal sizes.
- [ ] Resize keeps selection/content coherent.
- [ ] Every displayed key has tested behavior.
- [ ] Loading, empty, and error states provide the next useful action.
- [ ] Read-only scope is preserved.

## STOP conditions

- A supported terminal cannot provide required size events.
- Copy support would require shelling out with user-controlled content.
- Filtering requires loading an unbounded database result into memory.

## Maintenance notes

Mutation actions should be a later packet using the same explicit key-map/state-machine approach and shared runtime transitions.

