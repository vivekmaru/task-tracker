# Human Operations

Phase 3 makes Forge inspectable and operable for developers and leads. The goal is not to build a general project-management dashboard. The goal is to make agent execution easy to trust: what is ready, what is running, what is blocked, what proof exists, and what follow-up work agents discovered.

This document is the UX contract for the Phase 3 implementation tickets.

## Principles

- TUI first, web polish second.
- Dense but calm surfaces, optimized for repeated inspection.
- Keyboard-first navigation in the TUI.
- Server-rendered web pages with useful URLs for shared inspection.
- Timeline and ledger patterns over boards.
- Proof, blockers, checkpoints, and attempts are first-class content.
- Copyable IDs, commands, and links everywhere they help handoff.
- Beautiful empty, loading, blocked, and verified states.
- No Jira-style ceremony: no board grooming, sprint ritual, required field sprawl, drag-and-drop primary workflows, or modal-heavy routine actions.

The visual direction is in `docs/ux-mockups/`:

- `tui-queue-console-v1.png` and `tui-queue-console-v2.png` for the first operator console.
- `web-ticket-detail.png` for the canonical shared ticket inspection page.
- `proposed-work-triage.png` for reviewing agent-created follow-up.
- `execution-ledger.png` for calm activity and proof inspection.

## Product Surfaces

### TUI Queue Console

The TUI queue console is the first rich human interface. It should open quickly with `forge tui` and make the current work state obvious without requiring a browser.

Initial content:

- queue summary by status
- ticket list with status, type, priority, title, harness or capability signals, and recency
- quick filters for status, type, project, harness, agent, and text where available
- selected-ticket preview with acceptance criteria, verification commands, current attempt, blocker, and proof count
- empty and error states that explain the next useful action

Initial interactions:

- move selection
- filter
- open ticket detail
- copy ticket ID
- quit

Claiming, releasing, approving, reopening, and archiving should be added only after shared runtime operations exist for those actions.

### TUI Ticket Detail And Timelines

Ticket detail should answer whether a developer can trust the current work state.

Show:

- ticket identity, status, type, priority, tags, and source attribution
- acceptance criteria and verification commands
- relevant paths, expected artifacts, required tools, permissions, capabilities, and harness restrictions
- current and prior attempts
- checkpoints, events, blockers, and proof artifacts as timelines
- commands or IDs that can be copied for handoff

Timelines should be ordered, compact, and readable. Terminal states and blocked states should be visually distinct without noisy decoration.

### Human Operation Transitions

Human operations must be runtime-backed, event-writing actions. UI code should not invent state transitions.

Phase 3 operations:

- mark proposed or backlog work ready
- reopen work when a terminal state needs another attempt
- unblock work when the blocker is resolved
- archive work that should leave the active queue
- mark work as needing review or reviewed

Each operation should have service-level validation and tests before TUI or web callers depend on it.

### Proposed Work Triage

Agent-created tickets should feel like a review inbox, not backlog grooming.

The triage flow should support:

- ready or enqueue
- refine
- merge with existing work
- reject
- archive

The interface should preserve the source attempt, source artifact, creation reason, acceptance criteria, verification commands, and relevant paths. Agents should be rewarded for preserving context, not for creating a lot of vague tickets.

### Web Inspection Surface

The web UI is the shared inspection surface for links in chats, PRs, and handoffs. It should use Go handlers with templ plus htmx where partial updates help. It should not become a React or Next.js app.

Initial pages:

- ticket list
- ticket detail
- attempt timeline
- event timeline
- artifact list
- proposed ticket detail or triage page
- basic workspace and project inspection

The first web slice should reuse the same information architecture proven by the TUI. Desktop use matters most, but pages should remain readable on mobile for quick checks.

## View Order

Build in this order:

1. TUI queue console foundation.
2. TUI ticket detail.
3. TUI attempt, checkpoint, blocker, event, and artifact timelines.
4. Runtime-backed human operation transitions.
5. Proposed work triage.
6. Server-rendered web ticket list and detail.
7. Shareable deep links.
8. Human auth.
9. Project and workspace admin screens.

This order keeps the first human interface useful while preserving the product direction: terminal-heavy developers get the fastest path first, and the web UI becomes a polished shared inspection surface after the interaction model is clear.

## Non-Goals For Phase 3

- Kanban boards.
- Sprint planning.
- Drag-and-drop workflow management.
- Custom-field administration.
- Advanced analytics.
- Real-time dashboard infrastructure.
- Full artifact storage beyond metadata inspection.
- Semantic search.
- Multi-tenant RBAC beyond the initial human auth boundary.

Those ideas either belong to later phases or should be skipped if they add ceremony without helping developers trust agent execution.

## Verification Expectations

For each Phase 3 implementation slice:

- Use shared runtime, service, or query surfaces; do not duplicate business logic inside UI packages.
- Add deterministic tests for view models, route output, state transitions, or handlers.
- Run the focused package tests plus `go test ./...` before closing the Beads issue.
- Keep UX docs current when implementation changes the view order, operation contract, or important non-goal.

## Beads Breakdown

Phase 3 is tracked in Beads as:

- `agent-task-tracker-phase-3.1` - Define human operations UX contract.
- `agent-task-tracker-phase-3.2` - Build TUI queue view foundation.
- `agent-task-tracker-phase-3.3` - Build TUI ticket detail view.
- `agent-task-tracker-phase-3.4` - Add TUI attempt and event timelines.
- `agent-task-tracker-phase-3.5` - Add human operation transitions.
- `agent-task-tracker-phase-3.6` - Add proposed work triage flow.
- `agent-task-tracker-phase-3.7` - Add server-rendered ticket list and detail pages.
- `agent-task-tracker-phase-3.8` - Add shareable deep links.
- `agent-task-tracker-phase-3.9` - Add human auth flow.
- `agent-task-tracker-phase-3.10` - Add project and workspace admin screens.
