# MCP stdio lifecycle

Forge serves its existing tool catalog over standard MCP stdio with `forge mcp
--config /absolute/path/forge.json`. Stdout is reserved exclusively for MCP
protocol frames; diagnostics and database errors are never written there.

The server runs until the client closes stdin, receives process cancellation, or
the runtime fails. Tool failures return an MCP tool error with a generic
message rather than SQL, file-system, or configuration details.

## Client configuration

All hosts use the same local command and explicit config path:

```json
{
  "command": "/absolute/path/to/forge",
  "args": ["mcp", "--config", "/absolute/path/to/forge.json"]
}
```

- Codex: add the object as an stdio MCP server in your Codex configuration.
- Claude Code: place it under `mcpServers.forge` in `.mcp.json`.
- Gemini CLI: place it under `mcpServers.forge` in its MCP configuration.
- Other MCP hosts: use the equivalent command/args fields.

The local process is trusted with the database credentials in this config. Do
not expose this stdio server over a network socket.
