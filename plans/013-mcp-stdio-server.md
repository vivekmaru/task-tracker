# Plan 013: Serve Forge as a real MCP stdio server

> **Executor instructions**: Use the official maintained Go MCP SDK available at implementation time and pin its exact version. Stdout is protocol-only; diagnostics go to stderr.
>
> **Drift check**: `git diff --stat 1601f86..HEAD -- internal/mcp internal/cli/cli.go cmd/forge go.mod go.sum internal/integration docs/mcp-lifecycle.md docs/harness-integration.md`

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: MED
- **Depends on**: Plan 012
- **Category**: feature, api
- **Planned at**: commit `1601f86`, 2026-07-12
- **Beads**: `agent-task-tracker-vds.13`

## Why this matters

Forge registers MCP tool metadata and handlers but `forge mcp` exits after stating that protocol serving is not implemented. A real stdio transport is the lowest-friction cross-harness integration for local coding agents.

## Current state

- `internal/mcp/server.go` builds the tool catalog and delegates representative calls to runtime methods.
- `internal/cli/cli.go:312-328` opens runtime, registers tools, prints a non-serving message, and exits.
- MCP is a local trusted-process surface; Plan 007 HTTP authentication does not apply, but database/config access still must be explicit.

## Commands

| Purpose | Command | Expected |
|---|---|---|
| MCP unit | `rtk go test ./internal/mcp ./internal/cli` | pass |
| Process integration | `rtk go test -tags=integration ./internal/integration -run TestMCPStdio` | pass |
| Full gate | `rtk ./scripts/verify.sh` | exit 0 |

## Scope

**In scope**: pinned official MCP SDK, stdio transport, initialize/list/call lifecycle, graceful cancellation, protocol-safe logging, process integration test, docs.

**Out of scope**: HTTP MCP transport, MCP Tasks extension, A2A, remote authentication, or new Forge tools.

## Git workflow

- Branch: `feat/production-013-mcp-stdio`
- Commit: `Serve Forge over MCP stdio`

## Steps

1. Replace or adapt the internal server wrapper to the official SDK while preserving existing operation names, schemas, and runtime handlers.
2. Make `forge mcp --config ...` run the stdio server until EOF, signal, or context cancellation. Do not print startup banners or logs to stdout.
3. Map runtime/domain errors to MCP tool errors without stack traces, SQL, or secrets. Preserve structured result payloads.
4. Add a subprocess integration test that launches the built Forge binary, performs initialize, lists tools, invokes create/claim/checkpoint/complete against the test PostgreSQL database, and shuts down cleanly.
5. Document client configuration examples for Codex, Claude Code, Gemini CLI, and a generic MCP host using the same binary/config path.

## Done criteria

- [ ] A standard MCP client initializes and lists all intended tools.
- [ ] The execution loop works over stdio.
- [ ] Stdout contains protocol frames only.
- [ ] EOF and SIGTERM close database resources cleanly.
- [ ] Tool names and schemas remain compatible or changes are documented explicitly.

## STOP conditions

- No maintained official Go SDK supports the required protocol version.
- Existing tool schemas are incompatible with SDK validation.
- The subprocess test observes non-protocol stdout.

## Maintenance notes

Evaluate the experimental MCP Tasks extension only after this base transport is stable and the production pilot demonstrates a need for deferred MCP operations.

