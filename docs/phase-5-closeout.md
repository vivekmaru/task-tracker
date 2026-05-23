# Phase 5 Closeout

Phase 5 added Forge's first intelligence and advanced-coordination layer while
preserving the product's low-ceremony execution loop. The work stayed grounded
in deterministic ledger data instead of adding opaque scheduling or heavy
realtime infrastructure.

## What Shipped

| Area | Status | Operator Surface |
|---|---|---|
| Model and harness comparison | shipped | `forge analytics by-model`, `forge analytics by-harness` |
| Cost and performance trends | shipped | `forge analytics trends` |
| Related work lookup | shipped | `forge related` |
| Recommendation experiments | shipped | `forge recommendations` |
| Workflow policy guardrails | shipped | claim policy service, documented in `docs/agent-contracts.md` |
| Realtime event feed foundation | shipped | cursor-polled event service/API foundation |
| External observability export | shipped | durable webhook delivery path, documented in `docs/observability-export.md` |
| Team workspace membership foundation | shipped | workspace member service/table, documented in `docs/workspace-membership.md` |

## Current Limits

- Search and recommendation features use Postgres full-text and deterministic
  ledger signals. Forge does not yet ship pgvector embeddings or learned
  ranking.
- Recommendations are advisory. They rank claimable `todo` work but do not
  replace transactional `claim-next`.
- Workspace membership is durable state, not full RBAC enforcement across every
  route and runtime entry point.
- The event feed is cursor-polled. There is no dedicated websocket, Redis, or
  Datastar live dashboard layer.
- Observability export uses webhook subscriptions and delivery retries, not a
  full observability platform or sink-management UI.

## Verification

Run the focused Phase 5 surfaces with:

```bash
go test ./internal/services ./internal/cli ./internal/runtime ./internal/jobs
```

Run the full suite before handoff:

```bash
go test ./...
```

Useful manual smoke checks:

```bash
forge analytics summary --workspace-id "$WORKSPACE_ID" --project-id "$PROJECT_ID"
forge analytics trends --workspace-id "$WORKSPACE_ID" --project-id "$PROJECT_ID"
forge related --ticket-id "$TICKET_ID"
forge recommendations --workspace-id "$WORKSPACE_ID" --project-id "$PROJECT_ID" --harness codex --capability codegen
```

## Recommended Next Wave

The next product wave should start from real dogfood friction, not from adding a
larger coordination system by default. The strongest candidates are:

1. tighten the Forge-on-Forge loop until agents can start from a clean checkout
   with fewer manual local setup steps;
2. promote the most useful Phase 5 CLI surfaces into the web/TUI only where they
   help operators decide what to trust or claim next;
3. consider vector-backed semantic search only after the ledger has enough
   accumulated attempts, checkpoints, and artifacts to prove it improves over
   the current full-text and recommendation surfaces;
4. expand RBAC from `workspace_members` only around concrete permission checks
   that are blocking real usage.
