# Plan 008: Secure and fence webhook delivery

> **Executor instructions**: Preserve documented at-least-once delivery. The goal is to prevent unsafe destinations and stale ownership updates, not to promise exactly-once delivery.
>
> **Drift check**: `git diff --stat 1601f86..HEAD -- internal/jobs internal/api/router.go internal/config sql/migrations sql/queries/webhooks.sql internal/integration docs/observability-export.md`

## Status

- **Priority**: P0
- **Effort**: L
- **Risk**: MED
- **Depends on**: Plans 003 and 007
- **Category**: security, correctness, performance
- **Planned at**: commit `1601f86`, 2026-07-12
- **Beads**: `agent-task-tracker-vds.8`

## Why this matters

Subscription validation accepts any HTTP(S) host, and workers connect directly to stored URLs. Delivery leases have no ownership token, allowing a stale worker to overwrite a newer result after lease expiry. Heartbeat events and terminal deliveries also have no retention boundary.

## Current state

- `internal/api/router.go:542-553` validates only scheme and host.
- `internal/jobs/webhooks.go:90-96` disables redirects and sets a timeout; preserve both protections.
- `sql/queries/webhooks.sql:41-89` claims rows using `locked_until` only.
- Success/failure updates at `sql/queries/webhooks.sql:91-120` match only by delivery ID.
- `internal/jobs/maintenance.go` has no event or webhook retention cleanup.

## Commands

| Purpose | Command | Expected |
|---|---|---|
| Jobs/API tests | `rtk go test ./internal/jobs ./internal/api ./internal/config` | pass |
| Integration | `rtk go test -tags=integration ./internal/integration -run 'TestWebhook' -count=10` | pass |
| Full gate | `rtk ./scripts/verify.sh` | exit 0 |

## Scope

**In scope**: destination policy/config, connect-time IP validation, claim-token migration/query changes, stale-worker handling, terminal delivery retention, heartbeat export defaults, docs, and tests.

**Out of scope**: exactly-once delivery, inbound webhooks, new observability payload schemas, or general network sandboxing.

## Git workflow

- Branch: `feat/production-008-webhook-hardening`
- Commit: `Harden webhook delivery`

## Steps

1. Add a default-deny destination policy for loopback, link-local, multicast, unspecified, and private IPs. Support explicit operator allowlisting for trusted internal sinks by exact hostname or CIDR; never infer trust from a URL string.
2. Enforce the policy at creation and at connect time with a custom transport/dial path so DNS rebinding cannot bypass it. Preserve proxy behavior only if the final destination is still policy-checked.
   - **Verify**: tests cover IPv4/IPv6 loopback, private ranges, mixed DNS answers, allowed internal sink, public sink, redirect, and DNS change between retries.
3. Add a forward migration for a claim token/version. Set a fresh token when claiming and require matching ID plus token in success/failure updates. A zero-row update means ownership was lost, not delivery failure.
   - **Verify**: concurrent integration test proves an expired claimant cannot overwrite the new claimant.
4. Add configurable retention for terminal deliveries and make heartbeat export opt-in rather than implicit when a subscription provides no event filters. Preserve ticket-event audit history unless a separate documented retention setting is enabled.
   - **Verify**: maintenance tests cover cutoff boundaries and never delete pending/delivering rows.
5. Update observability documentation with egress, allowlist, at-least-once, fencing, and retention semantics.

## Done criteria

- [ ] Default configuration cannot reach local/private metadata or service endpoints.
- [ ] Explicit allowlists support intentional private sinks.
- [ ] Stale workers cannot mutate newer delivery ownership.
- [ ] Terminal delivery storage is bounded by documented retention.
- [ ] Redirect refusal and response-size limits remain intact.

## STOP conditions

- Supported deployments require a transparent proxy whose final target cannot be validated.
- Existing subscriptions use private targets and there is no migration/default policy approved by the operator.
- Claim-token migration exposes inconsistent currently delivering rows that cannot be safely reset to pending.

## Maintenance notes

Review every new outbound network feature against the same destination policy. At-least-once consumers must continue using event and delivery IDs for deduplication.

