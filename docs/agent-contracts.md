# Agent Operation Contracts

Forge keeps agent-facing operation definitions in `internal/contracts`. The package is the shared catalog for REST, CLI, and MCP work so later adapters do not invent separate names, descriptions, or JSON Schema payloads.

The catalog currently defines the Phase 2 surface:

- `create_ticket`
- `propose_ticket`
- `create_ticket_from_attempt`
- `claim_next_ticket`
- `heartbeat_attempt`
- `checkpoint_attempt`
- `update_ticket`
- `complete_attempt`
- `fail_attempt`
- `block_attempt`
- `list_tickets`
- `get_ticket`
- `attach_artifact`
- `decompose_ticket`
- `register_agent_capabilities`

Each operation includes:

- a stable operation name for MCP tools
- REST operation ID and CLI command bindings where that surface already exists
- an input JSON Schema
- an output JSON Schema
- an agent-readable summary and description

Agent-created ticket flows should prefer `propose_ticket` and `create_ticket_from_attempt`. Their schemas deliberately ask for acceptance criteria, verification commands, relevant paths, and creation reason so agents can create useful work without adding Jira-style ceremony.

## Workflow Policy Guardrails

Forge now has a small workflow policy service in `internal/services/policy.go`. It is not RBAC; it is a shared guardrail layer that evaluates common ticket and attempt workflow inputs and returns an `allow`, `warn`, or `deny` decision with human-readable reasons.

Current defaults:

- Agent-created work may not be directly enqueued unless the caller has enqueue authority.
- Thin agent-created tickets are warnings, not denials: missing source attribution, creation reason, acceptance criteria, or verification commands should be surfaced for triage.
- Claim requests deny excessive leases; the default maximum claim lease is two hours.
- Ticket-aware claim checks can deny disallowed harnesses or missing required capabilities, and warn when required tools or permissions need human attention.
- Attempt checkpoint and terminal transitions deny non-running attempts when attempt status is supplied.
- Archived tickets deny workflow transitions until reopened; done tickets produce a warning so callers can ask for an explicit reason.

The first runtime integration is `claim_next_ticket`: `Runtime` constructs the policy service and `ClaimService` denies claim requests that violate claim-level policy before touching storage. Other callers can use `PolicyService.Evaluate` directly when they already have richer ticket or attempt context.

See [Harness Integration Examples](harness-integration.md) for copy-pasteable CLI and MCP-oriented flows for Codex, Claude Code, Gemini CLI, OpenCode, and custom agents.

See [Phase 2 Closeout](phase-2-closeout.md) for the current REST, CLI, and MCP parity matrix, test commands, and known adapter boundaries.
