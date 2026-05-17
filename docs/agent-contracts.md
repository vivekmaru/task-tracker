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

See [Harness Integration Examples](harness-integration.md) for copy-pasteable CLI and MCP-oriented flows for Codex, Claude Code, Gemini CLI, OpenCode, and custom agents.
