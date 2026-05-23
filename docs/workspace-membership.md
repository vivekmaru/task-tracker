# Workspace Membership Foundation

Forge now stores workspace membership in `workspace_members`. The table is scoped by `workspace_id` and actor identity so later authorization checks can answer who belongs to a workspace without changing the core execution tables.

## Roles

Membership roles are intentionally small:

- `owner` - accountable workspace owner.
- `admin` - workspace administrator.
- `member` - regular participant.
- `viewer` - read-oriented participant.

The service accepts `human`, `agent`, and `system` actor types. Actor IDs are caller-defined stable identifiers such as a human user ID, API key actor, or agent name.

## Current Limits

This is a durable foundation, not full RBAC enforcement:

- Existing ticket, claim, artifact, search, analytics, and web paths still rely on the current workspace/project scoping and admin-token boundary.
- Membership changes do not yet emit audit events.
- The service does not enforce last-owner protection or automatic owner creation when a workspace is created.
- No public REST, CLI, TUI, or web management surface is added in this slice.

## Future RBAC Boundary

Future authorization work should use `workspace_members` as the workspace-level subject table, then layer permission checks near route/runtime entry points. Keep those checks out of low-level query code so execution services can continue to accept explicit workspace/project scope and remain easy to test.

Likely next permissions include:

- `ticket:create`
- `ticket:claim`
- `ticket:update`
- `ticket:read`
- `attempt:update`
- `artifact:create`
- `analytics:view`
- `admin:manage`
