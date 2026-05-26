# Observability Export Foundation

Forge can export execution ledger events through the durable webhook delivery path. This is the first observability foundation: it emits structured JSON for ticket events and snapshots attempt-scoped metadata and metrics when deliveries are enqueued.

## Configuration

Exports use scoped webhook subscriptions and durable webhook deliveries.

Create and inspect a subscription for a workspace/project scope:

```bash
forge observability subscriptions create \
  --workspace-id "$WORKSPACE_ID" \
  --project-id "$PROJECT_ID" \
  --endpoint-url https://observability.example.test/forge/events \
  --secret shared-secret \
  --event-types claimed,checkpointed,completed,failed,blocked \
  --description "External observability sink" \
  --json

forge observability subscriptions list \
  --workspace-id "$WORKSPACE_ID" \
  --project-id "$PROJECT_ID" \
  --json
```

An empty `event_types` array subscribes to all ticket events in that scope. The endpoint must be `http://` or `https://`. If `secret` is set, deliveries include `X-Forge-Signature-SHA256`, an HMAC-SHA256 over the exact JSON request body.

Ticket event inserts enqueue `webhook_deliveries` automatically. The enqueue trigger snapshots attempt metadata and attempt metrics into the delivery payload when those rows exist, so retries preserve the event-time view instead of reading mutable attempt state later. The webhook worker claims pending deliveries, posts the observability payload, records response metadata, and retries failures with exponential backoff up to `max_attempts`.

## Payload

Webhook requests are JSON with `X-Forge-Payload-Schema: forge.observability.v1`.

```json
{
  "schema_version": "forge.observability.v1",
  "source": "forge",
  "signal": "ticket_event",
  "workspace_id": "00000000-0000-0000-0000-000000000001",
  "project_id": "00000000-0000-0000-0000-000000000002",
  "ticket_id": "00000000-0000-0000-0000-000000000003",
  "attempt_id": "00000000-0000-0000-0000-000000000004",
  "event": {
    "id": "00000000-0000-0000-0000-000000000047",
    "type": "completed",
    "actor_type": "agent",
    "actor_id": "codex-worker",
    "data": {"output_schema": "summary.v1"},
    "occurred_at": "2026-05-23T07:30:00Z"
  },
  "attempt": {
    "id": "00000000-0000-0000-0000-000000000004",
    "agent_id": "codex-worker",
    "harness": "codex",
    "model": "gpt-5",
    "status": "succeeded",
    "progress_percent": 100,
    "trace_id": "trace-123",
    "checkpoint_ref": "checkpoint-456",
    "started_at": "2026-05-23T07:27:00Z",
    "completed_at": "2026-05-23T07:29:50Z"
  },
  "metrics": {
    "tokens_in": 1200,
    "tokens_out": 345,
    "total_tokens": 1545,
    "cost_usd": 0.123456,
    "duration_seconds": 170.25,
    "retry_count": 2
  }
}
```

The `attempt` section is omitted for ticket-only events. The `metrics` section is omitted when an attempt has no recorded metrics yet.

## Limits

- This is an export foundation, not a full observability platform. Forge does not yet provide sink management UI, OpenTelemetry exporters, dashboards, or aggregation.
- Subscription creation and listing are available through the JSON CLI; API and web management surfaces can be added later without changing the payload contract.
- Delivery is at least once. Consumers should deduplicate by `event.id` or the `X-Forge-Event-ID` header.
- Payload data comes from ticket events plus event-time snapshots of attempts and attempt metrics. Search analytics, policy decisions, claims comparisons, artifacts, and UI state are intentionally out of scope.
- The current worker type supports the durable claim/post/retry path. A long-lived scheduler around it is a separate operations concern.
