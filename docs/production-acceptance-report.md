# Production acceptance pilot

- Started (UTC): 2026-07-12T21:13:27Z
- Duration seconds: 32
- Release binary: forge dev commit=none build_date=unknown
- Quality gate: pass
- REST and MCP lifecycle: pass
- Claim race, lease fencing, terminal atomicity: pass
- Webhook ownership fencing: pass
- Recovery drill: pass
- Browser accessibility smoke: pass when Bun dependencies are installed

The pilot uses disposable databases and deliberately omits database URLs,
tokens, ticket bodies, artifact contents, and webhook secrets.
