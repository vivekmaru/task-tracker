# Phase 2 Closeout

Phase 2 makes Forge usable by real agent harnesses without turning the product into a heavy workflow system. The durable contract lives in `internal/contracts`; REST route registration, CLI commands, and MCP tools should import that catalog instead of inventing names or payload shapes.

## What Is Testable Now

Run the Phase 2 parity and adapter checks with:

```bash
go test ./internal/contracts ./internal/api ./internal/cli ./internal/mcp
```

Run the full suite before handoff:

```bash
go test ./...
```

Current adapter status:

| Operation | REST | CLI | MCP | Notes |
|---|---:|---:|---:|---|
| `create_ticket` | yes | yes | yes | All three adapters call shared runtime services. |
| `propose_ticket` | yes | yes | yes | Agent-created proposals should include acceptance criteria, verification commands, relevant paths, and creation reason. |
| `create_ticket_from_attempt` | no | via `forge codex follow-up` | yes | MCP has the direct operation; Codex exposes a scoped convenience command. |
| `claim_next_ticket` | yes | yes | yes | Claim requests support harness, capabilities, lease, and idempotency key semantics. |
| `heartbeat_attempt` | yes | yes | yes | All implemented adapters use the attempt lease runtime operation. |
| `checkpoint_attempt` | yes | yes | yes | Checkpoints capture resumable progress, files, commands, next step, and risk. |
| `update_ticket` | yes | no | yes | REST and MCP expose typed ticket metadata patching; generic CLI update is not implemented yet. |
| `complete_attempt` | yes | yes | yes | Generic CLI completes attempts; Codex convenience completion can attach proof artifacts atomically. |
| `fail_attempt` | yes | yes | yes | Generic CLI and MCP expose failure reason and category. |
| `block_attempt` | yes | yes | yes | Generic CLI and MCP expose blocker context; Codex block can attach proof artifacts atomically. |
| `list_tickets` | yes | yes | yes | List supports workspace, project, status, type, offset, and limit where implemented. |
| `get_ticket` | yes | yes | yes | CLI also supports `--kind attempt`; the contract operation is ticket-focused. |
| `attach_artifact` | yes | yes | yes | Registers artifact metadata for tickets and attempts. |
| `decompose_ticket` | yes | no | yes | REST and MCP call the shared decomposition service; generic CLI decomposition is not implemented yet. |
| `register_agent_capabilities` | no | no | yes | MCP is the implemented adapter for capability registration today. |

REST resource and execution lifecycle routes are executable typed handlers protected by the API bearer/header token boundary. `Idempotency-Key` is the canonical claim replay header. Request bodies are limited to 1 MiB, and the handler passes the HTTP request context to runtime services so disconnect cancellation reaches PostgreSQL. `TestRESTResourceWorkflow` and `TestRESTExecutionAndIdempotency` prove PostgreSQL-backed resource and execution flows; parity tests continue to verify operation IDs.

## Parity Coverage

The closeout coverage is intentionally adapter-aware:

- `internal/contracts` checks that every Phase 2 operation appears in the shared catalog and has the expected REST, CLI, and MCP bindings.
- `internal/api` checks that OpenAPI operation IDs for REST-bound operations match `internal/contracts`.
- `internal/cli` checks that every contract-declared CLI command is a known runtime command.
- `internal/mcp` checks that every contract operation has an MCP tool handler and representative calls delegate to shared runtime services.

When adding a new adapter command or route, update `internal/contracts` first, then add or extend the adapter parity test. Empty bindings are allowed only when that adapter is not implemented for the operation.

## How Agents Should Use Forge

Agents should keep the loop small:

1. Claim one ticket with a stable harness name, agent ID, capabilities, lease, and retry-safe idempotency key.
2. Execute in the harness-native environment.
3. Checkpoint when progress becomes useful for handoff or recovery.
4. Complete with proof, fail with a concrete reason, or block with specific evidence.
5. Propose follow-up work when the attempt discovers adjacent work.

Prefer capability and type filters over harness-specific tickets. Use harness restrictions only when work depends on a harness-only environment, artifact format, or tool.

See `docs/harness-integration.md` for copy-pasteable Codex, Claude Code, Gemini CLI, OpenCode, and custom-agent flows.
