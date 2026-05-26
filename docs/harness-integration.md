# Harness Integration Examples

Forge harness integrations should feel like a fast execution loop, not a project-management ritual. A harness needs only enough structure to claim work, leave useful progress, attach proof, and create follow-up work when it learns something new.

Use these examples as copy-paste starting points. They assume:

- `forge` is built and on `PATH`, or replace `forge` with `go run ./cmd/forge`.
- `FORGE_DATABASE_URL` or `--config forge.json` points at the Forge database.
- `WORKSPACE_ID` and `PROJECT_ID` are known UUIDs.
- Shell examples use `jq` only to extract IDs from JSON. Harnesses can read JSON directly.

```bash
export WORKSPACE_ID="00000000-0000-0000-0000-000000000002"
export PROJECT_ID="00000000-0000-0000-0000-000000000003"
```

## Harness Contract

Every harness should follow the same small loop:

1. Claim one ticket with a stable harness name, agent ID, capabilities, and lease.
2. Do the work outside Forge in the harness-native environment.
3. Checkpoint when state changes, before risky edits, or before handoff.
4. Complete with proof, or block with a specific reason and captured evidence.
5. Create proposed follow-up work when the agent discovers adjacent work.

Keep required fields sparse. Prefer concise acceptance criteria, verification commands, and relevant paths over labels, boards, or workflow ceremony.

Before claiming, an agent can ask Forge for deterministic next-work
recommendations. Recommendations only consider claimable `todo` tickets and
include transparent ranking reasons, so the result is useful for planning
without turning routing into a hidden model decision:

```bash
forge recommendations \
  --workspace-id "$WORKSPACE_ID" \
  --project-id "$PROJECT_ID" \
  --harness "codex" \
  --capability codegen \
  --capability tests \
  --limit 5
```

Codex can use the scoped shortcut, which defaults the harness filter to
`codex`:

```bash
forge codex recommendations \
  --workspace-id "$WORKSPACE_ID" \
  --project-id "$PROJECT_ID" \
  --capability codegen \
  --capability tests \
  --limit 5
```

## Codex

Codex has thin convenience commands that set `harness=codex` and use attempt-derived scope for proof and follow-up work.

Claim:

```bash
claim_json=$(forge codex claim \
  --workspace-id "$WORKSPACE_ID" \
  --project-id "$PROJECT_ID" \
  --agent-id "codex:${CODEX_MODEL:-local}" \
  --capability codegen \
  --capability tests \
  --capability git \
  --lease 30m \
  --idempotency-key "codex:${WORKSPACE_ID}:${PROJECT_ID}:${PWD}")

attempt_id=$(printf '%s' "$claim_json" | jq -r '.attempt_id')
ticket_id=$(printf '%s' "$claim_json" | jq -r '.ticket_id')
```

The claim response includes an agent-ready context bundle. Use the decoded
`context.ticket` fields as the starting brief instead of inspecting raw database
rows:

```bash
printf '%s' "$claim_json" | jq -r '.context.ticket.title'
printf '%s' "$claim_json" | jq -r '.context.ticket.verification_commands[]?'
printf '%s' "$claim_json" | jq -r '.context.ticket.relevant_paths[]?'
```

`context.attempt`, `context.checkpoints`, and `context.artifacts` provide the
compact handoff state for resumptions and retries.

Checkpoint:

```bash
forge codex checkpoint "$attempt_id" \
  --summary "Found the failing boundary and added a focused regression test" \
  --progress 45 \
  --file internal/cli/cli.go \
  --file internal/cli/cli_test.go \
  --command "go test ./internal/cli" \
  --next "Finish the implementation and run the full suite"
```

Complete with proof:

```bash
forge codex complete "$attempt_id" \
  --summary "Implemented the fix and verified the CLI regression suite" \
  --proof "local://artifacts/go-test-cli.txt" \
  --proof-type test_output \
  --mime-type text/plain
```

Block with evidence:

```bash
forge codex block "$attempt_id" \
  --reason "Missing production API token required to reproduce the failing webhook" \
  --category permission_required \
  --proof "local://artifacts/webhook-repro.log" \
  --proof-type log \
  --mime-type text/plain
```

Create a proposed follow-up from the current attempt:

```bash
forge codex propose \
  --attempt-id "$attempt_id" \
  --type bug \
  --title "Handle empty webhook retry payload" \
  --description "Discovered while testing the webhook failure path." \
  --acceptance "Empty retry payload returns a typed validation error" \
  --verify "go test ./internal/webhooks" \
  --path internal/webhooks \
  --reason "Codex discovered adjacent failing input while completing another ticket"
```

Inspect and approve proposed work for a later claim:

```bash
forge proposed list --workspace "$workspace_id" --project "$project_id" --json
forge proposed ready "$ticket_id" --actor-type agent --actor-id codex --reason "Verified scope is worth a follow-up"
forge proposed enqueue "$ticket_id" --actor-type agent --actor-id codex --reason "Approved for immediate work"
forge proposed reject "$ticket_id" --actor-type agent --actor-id codex --reason "Already covered by active work"
forge proposed archive "$ticket_id" --actor-type agent --actor-id codex --reason "No longer relevant"
```

## Claude Code

Claude Code can use the generic JSON CLI surface directly. Use `harness=claude-code` so eligibility, metrics, and later history can distinguish it from Codex.

```bash
claim_json=$(forge claim-next --json \
  --workspace-id "$WORKSPACE_ID" \
  --project-id "$PROJECT_ID" \
  --harness claude-code \
  --agent-id "claude-code:${USER:-local}" \
  --capability codegen \
  --capability refactor \
  --capability tests \
  --lease 30m \
  --idempotency-key "claude-code:${WORKSPACE_ID}:${PROJECT_ID}:${PWD}")

attempt_id=$(printf '%s' "$claim_json" | jq -r '.attempt_id')
```

```bash
forge checkpoint --json "$attempt_id" \
  --summary "Refactor shape identified; no behavior changes yet" \
  --progress 30 \
  --file internal/services/tickets.go \
  --command "go test ./internal/services" \
  --next "Apply the smaller refactor and rerun service tests"
```

```bash
forge complete --json "$attempt_id" \
  --summary "Refactor completed and service tests pass"
```

When Claude Code discovers new work, prefer a proposed ticket instead of silently expanding scope:

```bash
forge propose --json \
  --workspace-id "$WORKSPACE_ID" \
  --project-id "$PROJECT_ID" \
  --title "Add regression coverage for ticket retry ordering" \
  --type bug \
  --description "Observed missing coverage while refactoring ticket services." \
  --acceptance "Retry ordering has focused unit coverage" \
  --verify "go test ./internal/services" \
  --created-by agent \
  --created-by-id claude-code \
  --reason "Discovered during an active Claude Code attempt"
```

## Gemini CLI

Gemini CLI should use the same generic path with `harness=gemini-cli`. Keep checkpoints terse and bias toward verification commands the next agent can run unchanged.

```bash
claim_json=$(forge claim-next --json \
  --workspace-id "$WORKSPACE_ID" \
  --project-id "$PROJECT_ID" \
  --harness gemini-cli \
  --agent-id "gemini-cli:${USER:-local}" \
  --capability docs \
  --capability tests \
  --lease 30m \
  --idempotency-key "gemini-cli:${WORKSPACE_ID}:${PROJECT_ID}:${PWD}")

attempt_id=$(printf '%s' "$claim_json" | jq -r '.attempt_id')
```

```bash
forge checkpoint --json "$attempt_id" \
  --summary "Drafted docs and verified referenced commands exist" \
  --progress 70 \
  --file docs/harness-integration.md \
  --command "go test ./internal/cli" \
  --next "Run full tests and finalize wording"
```

```bash
forge complete --json "$attempt_id" \
  --summary "Documentation slice completed with command coverage"
```

## OpenCode

OpenCode can identify itself with `harness=opencode` and should attach logs or terminal transcripts as artifacts when the environment matters.

```bash
claim_json=$(forge claim-next --json \
  --workspace-id "$WORKSPACE_ID" \
  --project-id "$PROJECT_ID" \
  --harness opencode \
  --agent-id "opencode:${USER:-local}" \
  --capability codegen \
  --capability shell \
  --lease 30m \
  --idempotency-key "opencode:${WORKSPACE_ID}:${PROJECT_ID}:${PWD}")

attempt_id=$(printf '%s' "$claim_json" | jq -r '.attempt_id')
ticket_id=$(printf '%s' "$claim_json" | jq -r '.ticket_id')
```

```bash
forge attach --json \
  --workspace-id "$WORKSPACE_ID" \
  --project-id "$PROJECT_ID" \
  --ticket-id "$ticket_id" \
  --attempt-id "$attempt_id" \
  --type log \
  --role evidence \
  --name "opencode-test-output.txt" \
  --url "local://artifacts/opencode-test-output.txt" \
  --mime-type text/plain
```

```bash
forge block --json "$attempt_id" \
  --reason "Local simulator is unavailable in this environment" \
  --category environment_unavailable
```

## Custom Agents

Custom agents can choose CLI, REST, or MCP, but they should keep operation semantics aligned with the shared contract in `internal/contracts`.

Minimum custom loop:

```bash
forge claim-next --json \
  --workspace-id "$WORKSPACE_ID" \
  --project-id "$PROJECT_ID" \
  --harness "custom-agent" \
  --agent-id "custom-agent:worker-1" \
  --capability codegen \
  --lease 20m
```

Equivalent MCP operation names:

- `claim_next_ticket`
- `checkpoint_attempt`
- `complete_attempt`
- `block_attempt`
- `attach_artifact`
- `propose_ticket`
- `create_ticket_from_attempt`

For REST adapters, use the same JSON payload shapes as the contract catalog and keep retries idempotent where the operation supports idempotency keys.

## Ticket Creation Guidance

Agent-created tickets are useful when they preserve context without stealing focus from the current attempt. A good generated ticket has:

- a specific title
- a short description of what was observed
- one or more acceptance criteria
- one or more verification commands
- relevant paths when known
- a creation reason tied to the source attempt

Avoid creating tickets for vague cleanup, broad redesign, or speculative backlog grooming. Forge should make discovered work easy to capture, not turn every observation into ceremony.
