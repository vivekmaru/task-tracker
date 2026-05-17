# MCP Server Lifecycle

Forge MCP mode is intentionally a thin adapter over the same runtime used by the API and CLI.

Current lifecycle:

1. `forge mcp` loads normal Forge configuration.
2. It validates the database-backed runtime configuration.
3. It opens the shared runtime composition.
4. It registers MCP tool metadata from `internal/contracts`.
5. It wires MCP tool calls to the shared runtime services.
6. It reports startup success with the registered tool count.

The MCP package now has runtime-backed handlers for the Phase 2 operation catalog. Protocol transport remains intentionally thin: it should expose the registered tools and delegate execution to `mcp.Server.Call` rather than duplicating business logic inside the transport layer.
