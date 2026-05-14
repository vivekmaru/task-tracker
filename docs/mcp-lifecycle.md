# MCP Server Lifecycle

Forge MCP mode is intentionally a thin adapter over the same runtime used by the API and CLI.

Current lifecycle:

1. `forge mcp` loads normal Forge configuration.
2. It validates the database-backed runtime configuration.
3. It opens the shared runtime composition.
4. It registers MCP tool metadata from `internal/contracts`.
5. It reports startup success with the registered tool count.

This phase does not execute MCP tool calls yet. Tool handlers and protocol serving belong to `P2-06`, and should delegate to runtime services rather than duplicating business logic inside the MCP package.
