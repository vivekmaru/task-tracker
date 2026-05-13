# Forge - Product Requirements Document

**Version:** 0.6 - Product Vision and Phased Build Plan  
**Date:** May 13, 2026  
**Status:** Draft for review  
**Primary decision:** Go monolith, Postgres-owned correctness, CLI/TUI/MCP first, boring server-rendered web stack with polished human UI.

---

## Change Log From Previous Draft

This version incorporates the execution-model and technology-stack rethink:

- Reframed Forge as a **machine-first work ledger**, not an agent framework, project manager, or observability replacement.
- Split the core schema into **Ticket** and **Attempt**.
- Made **Postgres the source of correctness** for atomic claiming, leases, attempts, events, and idempotency.
- Removed GraphQL, TimescaleDB, mandatory Redis, mandatory pgvector, dedicated event bus, and complex realtime dashboard infrastructure from the initial product build.
- Changed implementation recommendation from Python/Next.js to a **Go-first single-binary product**.
- Replaced Next.js with **Go-rendered UI using templ + htmx**; TUI is prioritized before web UI polish.
- Added **Huma** for OpenAPI-first Go API development.
- Added **River** for Postgres-backed background jobs.
- Kept semantic search, learned routing, fairness scoring, and advanced analytics out of the first execution phases.

---

## 1. Executive Summary

Forge is a **machine-first work ledger for autonomous AI agents**.

It provides a durable, structured source of truth for agent work across different harnesses such as Claude Code, Codex, Gemini CLI, OpenCode, GitHub Copilot, Pi Agent, Antigravity, and future agent runtimes.

Forge is designed around one core primitive:

```bash
forge claim-next
```

Agents pull work atomically, execute it, report progress, attach outputs, and complete or fail attempts. Every execution becomes structured, auditable, queryable history.

### Core Promise

Every unit of agent work has:

```text
a ticket
one or more attempts
structured inputs and outputs
durable events
artifacts
metrics
replay/debug context
```

Forge is not an agent framework, not an orchestration engine, and not an observability replacement.

It is the system of record for autonomous work execution.

---

## 2. Problem Statement

Modern agent workflows suffer from:

- Work tracked across markdown files, prompts, chats, GitHub issues, spreadsheets, and ad hoc queues.
- Duplicated effort across different agents and harnesses.
- No reliable claim/retry semantics.
- No durable history of what each agent attempted.
- Poor visibility into cost, outputs, failures, and artifacts.
- No common execution ledger across different harnesses.

Existing tools are usually:

```text
human-first issue trackers
trace-first observability tools
framework-specific workflow engines
ad hoc Redis queues
markdown task files
```

None provide a lightweight, harness-agnostic work ledger purpose-built for autonomous agent fleets.

---

## 3. Product Positioning

Forge should be positioned as:

> A pull-based work ledger for autonomous AI agents.

Avoid positioning Forge as:

- AI Jira
- Jira for agents
- Agent framework
- Workflow orchestrator
- LangSmith replacement
- Project management app
- Memory layer

Forge must not inherit the failure modes of traditional issue trackers:

```text
heavy workflows
slow screens
ceremony-first process
field sprawl
status theater
board maintenance
administrative drag
```

The product should feel fast, lightweight, and useful in the flow of work. It should make the right thing easy without turning execution into issue grooming.

The durable value is:

```text
coordination
execution history
claim correctness
retry safety
auditability
artifact capture
cross-harness interoperability
low-friction execution
```

---

## 4. Product Scope and Boundaries

This document describes the full product direction for Forge. It should be read as a long-horizon product spec with phased delivery, not as one release checklist.

Forge should be built in phases, but the product vision should remain intact:

- A durable work ledger for autonomous agents.
- Correct execution semantics across projects, harnesses, and agent identities.
- A machine-friendly surface first, with human inspection and operations surfaces layered on top.
- A low-ceremony product that reduces coordination friction instead of creating process overhead.
- Beautiful, fast human interfaces for developers who need to inspect, steer, and trust agent work.
- A boring operational core that can grow into richer coordination features after real usage exists.

### 4.1 Product Goals

- Durable source of truth for agent work.
- Atomic pull-based `claim-next`.
- Correct handling of retries, crashes, and abandoned work.
- Clear separation between work definition and execution attempts.
- Append-only event history for audit/debugging.
- Structured input/output capture.
- Artifact attachment support.
- Agent-friendly ticket creation, proposal, and decomposition.
- Claim responses that include enough context for an agent to start safely.
- Durable checkpoints for long-running or interrupted attempts.
- Basic metrics by ticket, attempt, model, harness, and agent.
- CLI-first experience.
- TUI-first human operations experience.
- Beautiful, keyboard-fast TUI and web UI for developer inspection and control.
- MCP tools for native agent integration.
- REST API with OpenAPI spec.
- Simple server-rendered web views for list/detail inspection.

### 4.2 Product Non-Goals

- No push-based orchestration.
- No complex workflow engine.
- No GraphQL.
- No TimescaleDB.
- No mandatory Redis.
- No mandatory pgvector.
- No learned routing in the execution core.
- No fairness scoring in the execution core.
- No complex realtime dashboard infrastructure.
- No React/Next.js frontend in the initial product direction.
- No enterprise-grade RBAC beyond basic workspace/project isolation.
- No attempt to replace observability platforms.
- No Jira-style board administration, sprint machinery, workflow theater, or mandatory process fields.

### 4.3 Build Strategy

The full product should be delivered as a sequence of coherent phases:

1. Establish the execution ledger and correctness core.
2. Add agent-native integration surfaces.
3. Add human operations surfaces.
4. Expand search, analytics, and artifacts.
5. Add advanced coordination only after real usage patterns justify it.

Each phase should leave Forge usable, testable, and internally consistent. Later phases should add surfaces and intelligence without weakening the core ledger semantics.

---

## 5. Target Users and Personas

| Persona | Primary Interface | Key Need |
|---|---|---|
| Specialist Agent | CLI / MCP | Claim work, execute, report output |
| Planner Agent | CLI / MCP | Create and decompose tickets |
| Developer | CLI / TUI / API | Debug failures and inspect attempts |
| Team Lead | TUI / Web UI | See work state and bottlenecks |
| Auditor | API / event log | Inspect durable execution history |

---

## 6. Core Concepts

### 6.1 Workspace

Workspace is the tenant or account boundary.

Even if the first deployable version is single-tenant, every core object should include:

```json
{
  "workspace_id": "uuid"
}
```

### 6.2 Project

Project is the repo, product, or workstream boundary.

```json
{
  "project_id": "uuid"
}
```

### 6.3 Ticket

A ticket is the durable definition of work.

It answers:

```text
What needs to be done?
```

A ticket should not contain execution-specific fields like model, harness, output, trace, cost, lease, or artifacts created during execution.

Agents should be first-class ticket creators.

In practice, planner agents, coding agents, review agents, and debugging agents will often discover follow-up work before a human has written it down. Forge should make this safe by supporting high-quality ticket creation, proposed tickets, decomposition, source attribution, and validation.

Agent-created tickets should answer:

```text
What should be done?
Why does this work exist?
How will another agent know it is done?
What context does the next agent need before starting?
What evidence should be attached when finished?
```

### 6.4 Attempt

An attempt is one execution of a ticket by one agent.

It answers:

```text
Who tried to do it, how, when, with what result?
```

A ticket can have multiple attempts.

Example:

```text
Ticket: Fix failing auth test
  Attempt 1: Claude Code, failed
  Attempt 2: Codex, expired
  Attempt 3: Gemini CLI, succeeded
```

### 6.5 Event

An event is an append-only historical record of what happened.

Events are used for:

- Audit
- Debugging
- Timeline UI
- Replay foundation
- Failure analysis

Forge should maintain current-state tables and append-only events. It should not require full event-sourced rebuild/projection architecture in the execution core.

### 6.6 Claim Context Bundle

A claim context bundle is the agent-ready brief returned by `claim-next`.

It should include:

- Ticket details.
- Attempt details.
- Acceptance criteria.
- Verification commands.
- Relevant repository or workspace context.
- Required tools and permissions.
- Relevant file paths.
- Prior attempts and failure summaries.
- Attached logs, diffs, screenshots, or documents needed to start.
- Expected proof artifacts.
- Human constraints or instructions.

This is the difference between assigning work to an agent and giving the agent enough context to act responsibly.

### 6.7 Attempt Checkpoint

An attempt checkpoint is a durable mid-run note written by an agent.

Checkpoints are useful when:

- The run is long.
- Context may compact or reset.
- The agent is about to switch strategy.
- The user interrupts or redirects work.
- The agent found important partial evidence.
- Another agent may need to resume.

Checkpoints should be concise and operational: what was found, what changed, what remains, and what the next agent should do.

### 6.8 Artifact

An artifact is a file or external object attached to a ticket or attempt.

Examples:

- Code diff
- Generated document
- Logs
- Screenshots
- Dataset
- Test output
- Trace export

---

## 7. End-to-End Flow

### 7.1 Basic Pull-Based Flow

```text
1. Create ticket.
2. Ticket enters todo.
3. Agent calls claim-next.
4. Forge atomically creates an attempt and returns a claim context bundle.
5. Agent executes work.
6. Agent heartbeats, checkpoints, and updates progress.
7. Agent completes, fails, or blocks the attempt.
8. Ticket status updates based on attempt result.
9. Events, checkpoints, and artifacts remain queryable.
```

### 7.2 Crash Recovery Flow

```text
1. Agent claims ticket.
2. Attempt lease is created.
3. Agent crashes or disappears.
4. Lease expires.
5. Attempt becomes expired.
6. Ticket returns to todo.
7. Another agent can claim it.
```

### 7.3 Day-Zero Product Scenario

The first complete product scenario should prove that Forge works as a cross-agent execution ledger.

Example:

```text
1. Developer creates workspace "acme" and project "web-app".
2. Developer creates three tickets:
   - bug: fix failing auth tests
   - documentation: update setup guide
   - review: inspect recent migration changes
3. Codex claims the bug ticket with type=bug and capability=codegen.
4. Claude Code claims the documentation ticket with type=documentation and capability=writing.
5. No agent claims the review ticket until a matching review-capable agent asks for work.
6. Codex heartbeats while running tests.
7. Codex writes a checkpoint after isolating the failing middleware path.
8. Codex fails the first attempt and attaches test output.
9. Forge records the failed attempt, writes events, and returns the ticket to todo because retry policy allows another attempt.
10. Codex retries with a stable idempotency key and claims a new attempt.
11. Codex completes the ticket with output summary, metrics, verification output, and a diff artifact.
12. Codex creates a proposed follow-up ticket for a related flaky test it discovered.
13. Developer opens the TUI or web detail view and sees the ticket, both attempts, checkpoints, events, metrics, artifacts, and proposed follow-up.
```

This scenario should be used as an acceptance path for the first execution phases.

---

## 8. Recommended Technology Stack

### 8.1 Stack Summary

Forge should be built as a **Go-first single-binary product**.

Recommended stack:

| Layer | Recommended Tooling | Reason |
|---|---|---|
| Language | Go | Single binary, strong concurrency, good infra/devtool fit |
| API | Huma + net/http/chi | OpenAPI-first Go API with typed schemas |
| Database | PostgreSQL | Source of truth, transactions, row locks, JSONB |
| DB Driver | pgx | Go-native Postgres driver/toolkit |
| SQL Layer | sqlc | Type-safe Go code from handwritten SQL |
| Migrations | goose initially; Atlas optional | Goose is boring/simple; Atlas useful for schema maturity |
| Jobs | River | Postgres-backed job queue, no Redis required |
| CLI | Cobra | Standard Go CLI framework |
| Config | Viper or lightweight config package | Env/config file support |
| TUI | Bubble Tea + Bubbles + Lip Gloss | Excellent Go terminal UI toolkit |
| MCP | Official MCP Go SDK | Keeps MCP server in same binary/ecosystem |
| Web UI | templ + htmx | Server-rendered, Go-native, low JS complexity |
| Realtime UI experiment | Datastar | Optional later for live dashboard interactions |
| Artifact Storage | Local filesystem + S3-compatible backend | Simple dev; production-ready abstraction |
| Release | GoReleaser | Cross-platform binaries, Homebrew tap, checksums |
| Containers | ko | Build Go containers without Dockerfile complexity |

### 8.2 Why Go

Forge is more of a durable developer/infrastructure tool than an ML application.

It needs:

```text
single binary distribution
CLI/TUI polish
HTTP server
background workers
leases and timers
Postgres transactions
file uploads
agent-safe APIs
low operational footprint
```

That is Go-shaped.

The product should feel installable like:

```bash
brew install forge
forge server
forge tui
```

### 8.3 Why Huma

Forge needs REST, OpenAPI, JSON Schema-like request/response definitions, and agent-readable API docs.

Huma gives a FastAPI-like development experience in Go while keeping the runtime simple.

Use Huma for:

- Route definitions
- Request validation
- Response schemas
- OpenAPI 3.1 generation
- API documentation

### 8.4 Why Postgres + pgx + sqlc

Postgres should own correctness.

Use Postgres for:

```text
tickets
attempts
events
artifacts
idempotency keys
metrics
agent capabilities
leases
transactional claiming
```

Use raw SQL for correctness-critical paths, especially `claim-next`.

sqlc is preferred because it preserves explicit SQL while generating type-safe Go methods.

### 8.5 Why River

Forge needs background work, but not Redis or a separate queue in the initial product architecture.

Use River for:

```text
expire stale attempts
clean old idempotency keys
process artifact cleanup
compute basic aggregates
send webhooks later
scheduled maintenance
```

River keeps background jobs in Postgres, matching the core principle:

```text
Postgres owns truth.
```

### 8.6 Why templ + htmx Instead of Next.js

Forge's web UI requirements are simple:

```text
ticket list
ticket detail
attempt timeline
events
artifacts
basic metrics
```

These do not require React, Next.js, server components, hydration, or a separate Node app.

Use:

```text
Go handlers
templ components
htmx partial updates
simple CSS or Tailwind
```

This is easier for AI agents to build, easier to secure, and easier to deploy.

### 8.7 TUI Before Web Polish

Forge's early users are likely terminal-heavy developers and agent operators.

Prioritize:

```bash
forge tui
```

before a sophisticated web dashboard.

The TUI should provide:

```text
queue view
ticket detail
attempt timeline
agent activity
logs/artifacts overview
```

---

## 9. Technical Architecture

### 9.1 Product Architecture

```text
Single Go binary
  forge server
  forge worker
  forge mcp
  forge tui
  forge <cli commands>

          |
          v

PostgreSQL
  tickets
  attempts
  events
  artifacts
  metrics
  idempotency_keys
  river jobs

          |
          v

Local or S3-compatible artifact storage
```

### 9.2 Package Layout

Recommended repository shape:

```text
cmd/forge/
  main.go

internal/api/
internal/cli/
internal/tui/
internal/mcp/
internal/db/
internal/domain/
internal/services/
internal/storage/
internal/jobs/
internal/config/
internal/auth/

sql/queries/
sql/migrations/
web/templates/
```

### 9.3 Command Modes

The same binary should expose:

```bash
forge server
forge worker
forge mcp
forge tui
forge create
forge propose
forge claim-next
forge heartbeat
forge checkpoint
forge complete
forge fail
forge block
forge attach
forge list
forge get
forge codex <subcommand>
```

### 9.4 Postgres Owns Claim Correctness

`claim-next` must be implemented transactionally in Postgres.

The claim operation must account for:

```text
workspace
project
ticket status
ticket type
tags
dependency completion
required capabilities
allowed harnesses
retry policy
lease availability
```

The intent is that Forge can be used across multiple projects, multiple agent harnesses, and different work types such as feature, bug, documentation, research, planning, and review work.

Harness filtering should be explicit:

- If `allowed_harnesses` is empty or null, any harness may claim the ticket if other filters pass.
- If `allowed_harnesses` is set, only those harnesses may claim the ticket.
- If `required_capabilities` is set, the claiming agent must provide matching capabilities or be registered with matching capabilities.
- Ticket `type` describes the work category; harness describes the runtime doing the work. These should not be conflated.

A representative claim query should look like:

```sql
SELECT t.id
FROM tickets t
WHERE t.workspace_id = $1
  AND t.project_id = $2
  AND t.status = 'todo'
  AND ($3::text IS NULL OR t.type = $3)
  AND ($4::text[] IS NULL OR t.tags && $4)
  AND (
    t.allowed_harnesses IS NULL
    OR cardinality(t.allowed_harnesses) = 0
    OR $5 = ANY(t.allowed_harnesses)
  )
  AND NOT EXISTS (
    SELECT 1
    FROM ticket_dependencies d
    JOIN tickets dep ON dep.id = d.depends_on_ticket_id
    WHERE d.ticket_id = t.id
      AND dep.status != 'done'
  )
  AND (
    t.required_capabilities IS NULL
    OR cardinality(t.required_capabilities) = 0
    OR t.required_capabilities <@ $6::text[]
  )
ORDER BY t.priority DESC, t.created_at ASC
FOR UPDATE SKIP LOCKED
LIMIT 1;
```

Then in the same transaction:

```text
1. Select eligible ticket.
2. Verify retry policy has not been exhausted.
3. Create attempt with agent, harness, model, lease, and idempotency context.
4. Set ticket status = in_progress.
5. Write claimed event.
6. Persist idempotency response if an idempotency key was supplied.
7. Commit.
```

Redis must not be required for correctness in the execution core.

The implementation should have a concurrency test that starts many workers against the same eligible ticket set and proves:

```text
no ticket is claimed twice
dependency-blocked tickets are skipped
harness-restricted tickets are only claimed by allowed harnesses
capability-restricted tickets are only claimed by capable agents
claim order is stable by priority DESC, created_at ASC among eligible rows
```

---

## 10. Data Model

### 10.1 Ticket

```json
{
  "id": "uuid",
  "workspace_id": "uuid",
  "project_id": "uuid",
  "parent_id": "uuid | null",
  "root_id": "uuid",
  "source_attempt_id": "uuid | null",
  "source_artifact_id": "uuid | null",
  "title": "string",
  "description": "string",
  "type": "feature | bug | documentation | research | analysis | planning | review | integration | custom",
  "status": "backlog | todo | in_progress | blocked | needs_review | done | failed | archived",
  "priority": 1,
  "tags": ["string"],
  "acceptance_criteria": ["string"],
  "verification_commands": [
    {
      "command": "pnpm test auth",
      "required": true,
      "purpose": "Prove auth tests pass"
    }
  ],
  "expected_artifacts": ["diff", "test_output"],
  "relevant_paths": ["apps/api/src/auth"],
  "required_tools": ["shell", "git"],
  "required_permissions": ["write-worktree", "network"],
  "environment": {
    "repo_path": "/path/to/repo",
    "package_manager": "pnpm",
    "default_branch": "main"
  },
  "input": "jsonb",
  "input_schema": "string | null",
  "required_capabilities": ["codegen", "testing"],
  "allowed_harnesses": ["codex", "claude-code"],
  "retry_policy": {
    "max_attempts": 3,
    "on_failure": "return_to_todo | mark_failed | needs_review",
    "requires_review_on_success": false
  },
  "created_by": "human | agent | system",
  "created_by_id": "string | null",
  "creation_reason": "string | null",
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

Agent-created tickets should default to `backlog` unless the creating agent or API key is allowed to enqueue directly. This gives teams a safe path for agent-discovered follow-up work without letting noisy agents flood the executable queue.

Ticket dependencies should be stored in a separate relational table rather than only as a JSON array:

```json
{
  "ticket_id": "uuid",
  "depends_on_ticket_id": "uuid",
  "workspace_id": "uuid",
  "project_id": "uuid",
  "created_at": "timestamp"
}
```

This keeps dependency checks queryable and enforceable inside the `claim-next` transaction.

### 10.2 Attempt

```json
{
  "id": "uuid",
  "workspace_id": "uuid",
  "project_id": "uuid",
  "ticket_id": "uuid",
  "agent_id": "string",
  "harness": "claude-code | gemini-cli | copilot | open-code | codex | pi-agent | antigravity | custom",
  "model": "string",
  "status": "running | succeeded | failed | blocked | expired | cancelled",
  "lease_expires_at": "timestamp",
  "last_heartbeat_at": "timestamp | null",
  "progress_percent": 0,
  "current_summary": "string | null",
  "next_step": "string | null",
  "output": "jsonb",
  "output_schema": "string | null",
  "failure_reason": "string | null",
  "failure_category": "task_failed | blocked | needs_human | environment_failed | permission_required | dependency_missing | unclear_requirements | null",
  "blocker": "jsonb | null",
  "trace_id": "string | null",
  "checkpoint_ref": "string | null",
  "started_at": "timestamp",
  "completed_at": "timestamp | null"
}
```

### 10.3 Attempt Metrics

```json
{
  "id": "uuid",
  "attempt_id": "uuid",
  "workspace_id": "uuid",
  "project_id": "uuid",
  "tokens_in": 0,
  "tokens_out": 0,
  "cost_usd": 0.0,
  "duration_seconds": 0.0,
  "retry_count": 0,
  "agent_success_score": 0.0,
  "human_rating": null,
  "created_at": "timestamp"
}
```

### 10.4 Attempt Checkpoint

```json
{
  "id": "uuid",
  "workspace_id": "uuid",
  "project_id": "uuid",
  "ticket_id": "uuid",
  "attempt_id": "uuid",
  "summary": "string",
  "files_touched": ["string"],
  "commands_run": ["string"],
  "next_step": "string | null",
  "risk": "string | null",
  "created_at": "timestamp"
}
```

### 10.5 Ticket Event

```json
{
  "id": "uuid",
  "workspace_id": "uuid",
  "project_id": "uuid",
  "ticket_id": "uuid",
  "attempt_id": "uuid | null",
  "type": "created | proposed | claimed | heartbeat | checkpointed | updated | completed | failed | blocked | expired | reviewed | archived",
  "actor_type": "human | agent | system",
  "actor_id": "string | null",
  "data": "jsonb",
  "created_at": "timestamp"
}
```

### 10.6 Artifact

```json
{
  "id": "uuid",
  "workspace_id": "uuid",
  "project_id": "uuid",
  "ticket_id": "uuid",
  "attempt_id": "uuid | null",
  "type": "code | document | image | dataset | log | diff | trace | test_output | screenshot | handoff | diagnostic | final_response | other",
  "role": "evidence | patch | context | output | diagnostic | handoff",
  "name": "string",
  "url": "string",
  "storage_backend": "local | s3",
  "size_bytes": 0,
  "mime_type": "string",
  "metadata": "jsonb",
  "created_at": "timestamp"
}
```

### 10.7 Idempotency Key

```json
{
  "id": "uuid",
  "workspace_id": "uuid",
  "key": "string",
  "route": "string",
  "request_hash": "string",
  "response_body": "jsonb",
  "created_at": "timestamp",
  "expires_at": "timestamp"
}
```

### 10.8 Agent Capability

Agent capabilities are optional in the earliest execution phase, but the model should anticipate them because they are central to safe cross-harness routing.

```json
{
  "id": "uuid",
  "workspace_id": "uuid",
  "project_id": "uuid | null",
  "agent_id": "string",
  "harness": "string",
  "capabilities": ["research", "codegen", "review"],
  "last_seen_at": "timestamp"
}
```

---

## 11. Execution Semantics

### 11.1 Claiming

`claim-next` should only return eligible tickets.

Eligibility:

```text
status = todo
dependencies are completed
workspace/project matches
type filter matches
tag filter matches
harness filter matches, if provided
capability filter matches, if provided
```

Default ordering:

```text
priority DESC, created_at ASC
```

No learned routing, fairness scoring, or smart scheduling should be part of the execution core.

Claim filters should be interpreted conservatively:

- `workspace` and `project` are required for correctness and tenant isolation.
- `type` selects the category of work, such as bug, feature, documentation, review, or research.
- `harness` selects the runtime, such as Codex, Claude Code, Gemini CLI, or OpenCode.
- `required_capabilities` describes what the agent must be able to do.
- `allowed_harnesses` is only for work that is genuinely harness-specific.

Most tickets should not be harness-specific. Prefer capability and type filters unless the ticket depends on a harness-only feature, environment, or artifact format.

The claim response should include a context bundle:

```json
{
  "ticket": {},
  "attempt": {},
  "acceptance_criteria": [],
  "verification_commands": [],
  "environment": {},
  "relevant_paths": [],
  "prior_attempts": [],
  "checkpoints": [],
  "artifacts": [],
  "expected_artifacts": []
}
```

Agents should not need a second round trip to understand the basic operating context for a claimed ticket.

### 11.2 Leases

Every claimed ticket creates an attempt with a lease.

Example:

```bash
forge claim-next --lease 30m
```

If the agent does not heartbeat before expiry:

```text
attempt.status = expired
ticket.status = todo
```

The ticket becomes claimable again.

### 11.3 Heartbeats

Agents can heartbeat during execution:

```bash
forge heartbeat <attempt-id>
```

This updates:

```text
attempt.last_heartbeat_at
attempt.lease_expires_at
```

Heartbeats should stay cheap and frequent. They prove liveness, not deep progress.

### 11.4 Checkpoints

Agents can checkpoint during execution:

```bash
forge checkpoint <attempt-id> \
  --summary "Identified auth middleware branch causing session expiry" \
  --next "Patch refresh logic and rerun auth tests"
```

This records:

```text
attempt.current_summary
attempt.next_step
attempt.progress_percent, if provided
attempt checkpoint row
checkpointed event
```

Checkpoints should be human-readable and resumable. They should capture meaningful discoveries, partial progress, files touched, commands run, risks, and the recommended next step.

### 11.5 Completion

On success:

```text
attempt.status = succeeded
ticket.status = done or needs_review
```

On failure:

```text
attempt.status = failed
ticket.status = todo or failed
```

Failure behavior depends on retry policy.

### 11.6 Blocking and Human Decisions

Agents should distinguish task failure from blocked execution.

Use `failed` when the agent attempted the work and concluded the task could not be completed successfully.

Use `blocked` when the agent cannot proceed because of an external condition:

```text
missing permission
missing secret
missing dependency
network or sandbox restriction
unclear requirement
human product decision required
environment setup failure
```

Blocked attempts should preserve the ticket and surface the blocker clearly. Depending on policy, the ticket may move to `blocked`, `needs_review`, or back to `todo` for another agent with different permissions.

### 11.7 Retry Policy

Forge should support basic retry limits:

```json
{
  "max_attempts": 3,
  "on_failure": "return_to_todo | mark_failed | needs_review"
}
```

Default:

```text
max_attempts = 3
on_failure = return_to_todo until max attempts, then failed
```

### 11.8 Dead Letter Behavior

When max attempts are exceeded:

```text
ticket.status = failed
```

Future enhancement:

```text
ticket.status = needs_review
```

### 11.9 Agent Ticket Creation

Ticket creation is a core agent workflow, not only a human workflow.

Agents should be able to create tickets when they:

- Decompose a larger ticket into executable subtasks.
- Discover follow-up work while executing an attempt.
- Identify a bug, missing test, or documentation gap.
- Need to hand off work to a better-suited harness or capability.
- Need human review before the work should become claimable.

Forge should support two creation modes:

```text
propose = create in backlog for human or planner review
enqueue = create directly in todo if the actor has permission
```

Agent-created tickets should include:

```text
title
description
type
priority
tags
acceptance criteria
verification commands
expected artifacts
source attempt or artifact
relevant paths
required tools and permissions
required capabilities
dependencies
creation reason
```

Ticket creation should have validation. Forge should warn or reject agent-created tickets that are missing a clear title, acceptance criteria, project, type, or enough context for another agent to start.

### 11.10 State Machine

Ticket and attempt statuses should follow explicit transitions.

Ticket transitions:

| From | Event | To | Notes |
|---|---|---|---|
| `backlog` | ready | `todo` | Human, planner agent, or API marks work claimable |
| `todo` | claim | `in_progress` | Same transaction creates a running attempt |
| `in_progress` | attempt blocks | `blocked` | External condition prevents progress |
| `blocked` | unblock | `todo` | Missing condition has been resolved |
| `blocked` | review required | `needs_review` | Human decision required before retry |
| `in_progress` | attempt succeeds | `done` | Default success path |
| `in_progress` | attempt succeeds and review is required | `needs_review` | Controlled by ticket retry/review policy |
| `in_progress` | attempt fails below retry limit | `todo` | Ticket becomes claimable again |
| `in_progress` | attempt fails at retry limit | `failed` | Dead-letter behavior |
| `in_progress` | attempt expires below retry limit | `todo` | Lease worker returns ticket to queue |
| `in_progress` | attempt expires at retry limit | `failed` | Lease worker applies retry policy |
| `needs_review` | review approves | `done` | Human or authorized reviewer |
| `needs_review` | review rejects | `todo` | Rework creates a future attempt |
| any active status | archive | `archived` | Administrative action |

Attempt transitions:

| From | Event | To | Notes |
|---|---|---|---|
| none | claim | `running` | Created inside claim transaction |
| `running` | heartbeat | `running` | Extends lease and records heartbeat event |
| `running` | checkpoint | `running` | Records resumable progress without extending terminal state |
| `running` | complete | `succeeded` | Terminal attempt status |
| `running` | fail | `failed` | Terminal attempt status |
| `running` | block | `blocked` | Terminal attempt status with blocker details |
| `running` | lease expires | `expired` | Terminal attempt status set by worker |
| `running` | cancel | `cancelled` | Terminal attempt status |

Invalid transitions should be rejected. In particular:

```text
terminal attempts cannot heartbeat, complete, fail, or cancel again
completed tickets cannot be claimed
failed tickets cannot be claimed unless explicitly reopened
blocked tickets cannot be claimed until unblocked or explicitly returned to todo
archived tickets cannot be claimed
only one running attempt may exist for a ticket at a time
```

Every accepted transition should write an append-only event in the same transaction as the state change.

---

## 12. Idempotency

All machine-facing mutation endpoints should support:

```http
Idempotency-Key: <stable-key>
```

Required for:

```text
POST /tickets
POST /tickets/propose
POST /tickets/claim-next
PATCH /tickets/{id}
POST /tickets/{id}/decompose
POST /tickets/{id}/ready
POST /attempts/{id}/checkpoint
POST /attempts/{id}/complete
POST /attempts/{id}/fail
POST /attempts/{id}/block
POST /attempts/{id}/cancel
POST /artifacts
```

Rules:

```text
same key + same request = return original response
same key + different request = conflict
keys expire after retention window
```

This prevents duplicate work when agents retry failed network calls.

Idempotency is most important for `claim-next`.

The risk is not hypothetical complexity in normal operation. The specific failure mode is:

```text
1. Agent sends claim-next.
2. Forge commits the claim and creates an attempt.
3. The network drops before the agent receives the response.
4. The agent retries.
```

With the same `Idempotency-Key`, Forge must return the originally committed ticket and attempt. Without that replay behavior, the retry can look like a fresh claim request and may claim different work.

Claim idempotency rules:

```text
key scope = workspace + actor/API key + route + key
same key + same request hash = replay original response
same key + different request hash = 409 conflict
stored response for claim-next must include ticket_id and attempt_id
idempotency record should be written in the same transaction as the claim
expired idempotency records may be cleaned by background jobs
```

Agents should generate stable idempotency keys for mutating operations they may retry. Human CLI usage can generate keys automatically unless the user explicitly supplies one.

---

## 13. CLI Specification

### 13.1 Installation

```bash
brew install forge
```

Alternative:

```bash
curl -fsSL https://forge.dev/install.sh | sh
```

### 13.2 Create Ticket

```bash
forge create \
  --project my-app \
  --title "Fix failing auth tests" \
  --type bug \
  --description "Investigate and fix the failing auth test suite" \
  --priority 4 \
  --tags "backend,tests" \
  --required-capability codegen \
  --required-capability testing \
  --input '{"repo": "github.com/acme/my-app"}'
```

### 13.3 Propose Ticket

Agents should be able to propose follow-up work without immediately making it claimable:

```bash
forge propose \
  --project my-app \
  --from-attempt <attempt-id> \
  --title "Stabilize flaky auth refresh test" \
  --type bug \
  --description "Codex observed intermittent failure while fixing auth tests" \
  --acceptance "Auth refresh test passes 10 consecutive runs" \
  --verify "pnpm test auth-refresh --repeat 10" \
  --required-capability testing
```

This should create a `backlog` ticket with source attribution.

### 13.4 Create Ticket From Attempt

```bash
forge create \
  --project my-app \
  --from-attempt <attempt-id> \
  --title "Add regression test for expired session refresh" \
  --type bug \
  --acceptance "Regression test fails before fix and passes after fix" \
  --verify "pnpm test auth" \
  --enqueue
```

`--enqueue` should require permission. Without it, agent-created follow-up tickets should default to proposed backlog work.

### 13.5 Claim Next

```bash
forge claim-next \
  --project my-app \
  --type bug \
  --tags backend \
  --harness claude-code \
  --capability codegen \
  --capability testing \
  --model claude-sonnet-4 \
  --lease 30m
```

Output:

```json
{
  "ticket": {},
  "attempt": {},
  "context": {}
}
```

### 13.6 Heartbeat

```bash
forge heartbeat <attempt-id>
```

### 13.7 Checkpoint

```bash
forge checkpoint <attempt-id> \
  --summary "Auth failure isolated to session middleware" \
  --next "Patch refresh logic and rerun pnpm test auth" \
  --files "apps/api/src/auth/session.ts" \
  --command "pnpm test auth"
```

### 13.8 Update Attempt

```bash
forge update-attempt <attempt-id> \
  --progress 50 \
  --output '{"partial_summary": "Auth failure isolated to session middleware"}'
```

### 13.9 Complete Attempt

```bash
forge complete <attempt-id> \
  --output '{"summary": "Fixed failing auth tests"}' \
  --metrics '{"tokens_in": 12000, "tokens_out": 2400, "cost_usd": 0.08}'
```

### 13.10 Fail Attempt

```bash
forge fail <attempt-id> \
  --reason "Could not reproduce failure"
```

### 13.11 Block Attempt

```bash
forge block <attempt-id> \
  --category permission_required \
  --reason "Network access is required to fetch dependency metadata" \
  --needs "Approve network access or provide vendored dependency metadata"
```

### 13.12 Attach Artifact

```bash
forge attach <ticket-id> \
  --attempt <attempt-id> \
  --file ./fix.patch \
  --type diff
```

### 13.13 List Tickets

```bash
forge list \
  --project my-app \
  --status todo \
  --type bug \
  --limit 20
```

### 13.14 Get Ticket

```bash
forge get <ticket-id> --with-attempts --with-events
```

### 13.15 Decompose Ticket

```bash
forge decompose <ticket-id> \
  --children ./subtasks.json
```

Forge should support manual or agent-provided decomposition before automatic decomposition is introduced.

### 13.16 Codex Harness Convenience Commands

Forge should keep Codex integration thin and adapter-like.

Useful convenience commands:

```bash
forge codex claim --project my-app --capability codegen --capability testing
forge codex checkpoint --summary "Found failing middleware branch"
forge codex complete --attach-diff --attach-test-output
forge codex propose --title "Add missing regression coverage"
```

These commands should call the same underlying REST/domain operations as the generic CLI. They should not turn Forge into an orchestration engine.

---

## 14. REST API

Base URL:

```text
/api/v1
```

Huma should generate OpenAPI from the route/schema definitions.

### 14.1 Tickets

| Method | Endpoint | Description |
|---|---|---|
| POST | `/tickets` | Create ticket |
| POST | `/tickets/propose` | Create proposed backlog ticket |
| GET | `/tickets` | List tickets |
| GET | `/tickets/{id}` | Get ticket |
| PATCH | `/tickets/{id}` | Update ticket metadata |
| POST | `/tickets/{id}/decompose` | Create child tickets |
| POST | `/tickets/{id}/ready` | Move backlog ticket to todo |

### 14.2 Claiming

| Method | Endpoint | Description |
|---|---|---|
| POST | `/tickets/claim-next` | Atomically claim next eligible ticket |

Returns:

```json
{
  "ticket": {},
  "attempt": {},
  "context": {}
}
```

### 14.3 Attempts

| Method | Endpoint | Description |
|---|---|---|
| GET | `/attempts/{id}` | Get attempt |
| PATCH | `/attempts/{id}` | Update attempt |
| POST | `/attempts/{id}/heartbeat` | Extend lease |
| POST | `/attempts/{id}/checkpoint` | Record resumable progress |
| POST | `/attempts/{id}/complete` | Complete attempt |
| POST | `/attempts/{id}/fail` | Fail attempt |
| POST | `/attempts/{id}/block` | Mark attempt blocked with blocker details |
| POST | `/attempts/{id}/cancel` | Cancel attempt |

### 14.4 Events

| Method | Endpoint | Description |
|---|---|---|
| GET | `/tickets/{id}/events` | List ticket events |
| GET | `/attempts/{id}/events` | List attempt events |

### 14.5 Artifacts

| Method | Endpoint | Description |
|---|---|---|
| POST | `/artifacts` | Upload/register artifact |
| GET | `/artifacts/{id}` | Get artifact metadata |
| DELETE | `/artifacts/{id}` | Delete artifact metadata/object if permitted |

### 14.6 Analytics

Initial analytics should be basic.

| Method | Endpoint | Description |
|---|---|---|
| GET | `/analytics/summary` | Basic counts and costs |
| GET | `/analytics/by-model` | Aggregate attempts by model |
| GET | `/analytics/by-harness` | Aggregate attempts by harness |

---

## 15. MCP Tools

MCP should be a thin integration layer over the same domain model.

Product tools:

```text
create_ticket
propose_ticket
create_ticket_from_attempt
claim_next_ticket
heartbeat_attempt
checkpoint_attempt
update_attempt
complete_attempt
fail_attempt
block_attempt
get_ticket
list_tickets
attach_artifact
decompose_ticket
register_agent_capabilities
```

Every MCP tool should include JSON Schema descriptions optimized for LLM use.

`claim_next_ticket` should return both:

```text
ticket
attempt
```

because the agent needs the attempt ID for future updates.

`propose_ticket` and `create_ticket_from_attempt` should be optimized for agent-discovered work. Their schemas should encourage acceptance criteria, verification commands, relevant paths, required capabilities, source attempt, and creation reason.

---

## 16. Search and Querying

### 16.1 Initial Search

Use Postgres filters and full-text search.

Support filters:

```text
workspace
project
status
type
priority
tags
created_by
agent_id
harness
model
created_at
updated_at
completed_at
```

Support text search over:

```text
title
description
input
output summaries
event data
artifact names
```

### 16.2 Later Search and Intelligence

Delay:

```text
pgvector
semantic search
historical learning
automatic recommendation
learned model selection
```

These should be introduced after real usage patterns exist.

---

## 17. UI and UX Requirements

### 17.1 CLI

The CLI is the primary interface for agents and developers.

It should be:

```text
fast
scriptable
JSON-friendly
pleasant for humans
stable across harnesses
```

The CLI should avoid ceremony. Common paths should require very few flags, support sensible defaults, and produce output that is easy for both humans and agents to parse.

### 17.2 UX Direction

Forge should feel like a calm execution console for agent work.

The visual direction should come from the product promise:

```text
fast
beautiful
low ceremony
proof-first
dense but breathable
quietly operational
developer-native
```

The UI should emphasize what helps a developer trust and steer autonomous work:

- What is happening now?
- Which agent or harness is doing it?
- What has been tried?
- What is blocked?
- What proof exists?
- What follow-up work did agents discover?
- What action can I take in one keystroke or one click?

Four product surfaces should guide the design:

1. **TUI Queue Console** - the keyboard-first operator console for queues, active attempts, blockers, proposed work, and quick filters.
2. **Web Ticket Detail** - the canonical inspection page for claim context, attempts, checkpoints, artifacts, verification evidence, and follow-up proposals.
3. **Proposed Work Triage** - a lightweight inbox for agent-created tickets, optimized for ready, refine, merge, reject, or enqueue actions.
4. **Execution Ledger** - a calm activity view for agents, harnesses, leases, recent events, blockers, and proof artifacts.

The interface should avoid issue-tracker gravity. No board grooming, sprint rituals, mandatory field sprawl, or modal-heavy admin flows. Forge should make agent execution easier to understand without making developers manage a process machine.

Visual tone:

```text
dark or neutral base
excellent typography
strong hierarchy
restrained color
status color only where useful
timeline and ledger patterns over boards
copyable commands and deep links everywhere
beautiful empty, blocked, loading, and verified states
```

### 17.3 TUI

The TUI should be the first rich human interface.

The TUI should feel like a fast operator console, not a terminal clone of a bloated issue tracker.

Design principles:

```text
beautiful by default
keyboard-first
fast to open
dense but calm
minimal ceremony
clear state at a glance
easy drill-down
no board maintenance tax
```

Required views:

```text
ticket queue
ticket detail
attempt timeline
event timeline
agent/harness activity
artifact list
basic metrics
```

Expected interactions:

```text
claim/inspect/release work
filter by project, status, harness, agent, and type
open ticket detail instantly
scan attempt timelines
compare attempts on one ticket
view blocker details
approve proposed tickets
mark backlog tickets ready
open artifacts and verification evidence
copy commands or IDs without fuss
```

### 17.4 Web UI

The web UI should be simple, server-rendered, and Go-native.

Simple should not mean crude. The web UI should be polished enough that developers trust it as the shared inspection surface for agent work.

Recommended stack:

```text
templ
htmx
simple CSS or Tailwind
```

Required initial views:

```text
ticket list
ticket detail
attempt list
event timeline
artifact list
basic metrics
```

UX requirements:

```text
fast first paint
clean typography
stable layout
high signal density
beautiful empty/loading/error states
clear visual hierarchy for ticket, attempt, event, and artifact timelines
copyable IDs and commands
URLs that are useful in chat, PRs, and handoffs
mobile-readable for quick checks, but desktop-first for real operations
```

Not required in the initial web UI:

```text
live Kanban
React SPA
Next.js app
real-time activity sidebar
success heatmaps
advanced analytics
```

The web UI should avoid Jira-like interaction patterns:

```text
no mandatory board grooming
no modal-heavy editing for common actions
no drag-and-drop as the primary workflow
no sprawling custom-field screens
no slow multi-page flows for routine inspection
```

### 17.5 Datastar Evaluation

Datastar may be worth a later spike for realtime-ish dashboard interactions:

```text
live agent status
live ticket updates
attempt timelines
activity feed
```

Do not commit to it for the initial product build until proven useful.

---

## 18. Security and Permissions

### 18.1 Authentication

Initial product:

```text
API keys for agents
admin token/session auth for humans
```

Later:

```text
JWT
SSO
mTLS
fine-grained service accounts
```

### 18.2 Authorization

Forge should include basic workspace/project scoping from the beginning.

Roles can be simple initially:

```text
admin
developer
agent
viewer
```

Permissions can come later, but the model should anticipate:

```text
ticket:create
ticket:claim
ticket:update
ticket:read
attempt:update
artifact:create
analytics:view
admin:manage
```

### 18.3 Data Protection

The initial product should include:

```text
API key hashing
secret redaction in events
audit events for privileged actions
basic artifact access controls
```

Later:

```text
PII detection
field-level encryption
mTLS
advanced compliance controls
```

---

## 19. Success Metrics

Early product success should measure execution correctness and adoption, not advanced intelligence.

### 19.1 Core Correctness Metrics

```text
claim-next double-claim rate = 0
stale claim recovery works reliably
P50 claim-next latency < 800ms
P95 claim-next latency < 2s
successful attempt completion rate
expired attempt rate
average attempts per completed ticket
```

### 19.2 Product Metrics

```text
number of tickets created per active project
number of attempts recorded per week
number of distinct harnesses used
percentage of completed tickets with artifacts
percentage of completed attempts with metrics
percentage of agent-created tickets accepted into todo
percentage of completed attempts with verification evidence
number of blocked attempts by blocker category
time from opening UI to finding current work state
number of user actions required for common inspect/approve flows
```

### 19.3 Later Metrics

```text
cost per successful ticket
success rate by model
success rate by harness
human review rate
model comparison accuracy
```

---

## 20. Risks and Mitigations

| Risk | Mitigation |
|---|---|
| Agents abandon claimed work | Attempt leases + heartbeat expiry |
| Duplicate claims | Postgres transactions + row locks |
| Duplicate mutations from retries | Idempotency keys |
| Schema becomes bloated | Ticket/Attempt separation + JSONB only where useful |
| Product drifts into orchestration | Keep pull-based ledger positioning |
| Product drifts into Jira-like process bloat | Favor low ceremony, fast defaults, minimal required fields, and execution evidence over workflow theater |
| Agents create noisy or vague tickets | Default agent-created work to backlog; validate acceptance criteria and context |
| Agents lose useful context mid-run | First-class checkpoints and claim context bundles |
| Blocked work is misclassified as failure | Separate blocked attempt status and blocker categories |
| Infra complexity slows delivery | Avoid GraphQL, TimescaleDB, mandatory Redis, event bus |
| Human UI feels like an afterthought | Treat TUI and web polish as product value, not decoration |
| Web UI complexity slows delivery | Use templ + htmx; prioritize TUI first while preserving speed and craft |
| Analytics distracts from execution correctness | Basic metrics only |
| Multi-tenant migration pain later | Add workspace/project IDs from day one |
| MCP ecosystem shifts | Keep REST and CLI as stable core |

---

## 21. Roadmap

### Phase 1 - Execution Core

Goal: make Forge correct as a work ledger before adding richer surfaces.

Deliverables:

- Go monolith binary.
- Postgres schema for workspaces, projects, tickets, attempts, attempt checkpoints, events, dependencies, idempotency keys, and basic metrics.
- Transactional `claim-next`.
- Claim context bundle returned with each claim.
- Lease and heartbeat semantics.
- Attempt checkpoint semantics.
- Attempt completion, failure, cancellation, and expiry.
- Blocked attempt and blocked ticket semantics.
- Append-only event log for all state transitions.
- Basic API key auth and workspace/project scoping.
- Huma REST API for core operations.
- JSON-first CLI for create, propose, claim, heartbeat, checkpoint, complete, fail, block, list, and get.
- River worker for lease expiry and idempotency cleanup.
- Concurrency and crash-recovery test suite.

### Phase 2 - Agent Integration

Goal: make Forge easy for real agents and harnesses to use.

Deliverables:

- MCP server wrapping the same domain operations as REST/CLI.
- Agent capability registration.
- Harness-aware claim filters.
- Stable JSON Schema descriptions for MCP tools.
- CLI ergonomics for agent scripts, including `--json`, `--quiet`, and predictable exit codes.
- Built-in idempotency key generation for CLI mutations.
- Decomposition support for planner agents.
- Ticket creation templates and validation for agent-created work.
- Thin Codex harness convenience commands.
- Documentation for Codex, Claude Code, Gemini CLI, OpenCode, and custom harness integration.

### Phase 3 - Human Operations

Goal: make active work inspectable and operable by developers and leads through fast, beautiful, low-friction interfaces.

Deliverables:

- Beautiful Bubble Tea TUI with queue, ticket detail, attempt timeline, events, artifacts, blockers, proposed tickets, and basic metrics.
- Manual review gates using `needs_review`.
- Reopen and archive flows.
- Polished templ + htmx web views for ticket list, ticket detail, attempt timeline, event timeline, artifact list, proposed tickets, and blockers.
- Admin token/session auth for human views.
- Basic project/workspace administration.
- Copyable/shareable deep links for tickets, attempts, artifacts, and proposed follow-ups.
- Fast filters and keyboard-friendly navigation.

### Phase 4 - Artifacts, Search, and Analytics

Goal: make Forge useful as an execution history, not just a queue.

Deliverables:

- Local artifact storage.
- S3-compatible artifact storage.
- Artifact access controls.
- Full-text search across titles, descriptions, inputs, output summaries, event data, and artifact names.
- Analytics summary by project, status, model, harness, and agent.
- Cost, duration, token, and success-rate reporting where agents provide metrics.
- Artifact browser improvements.
- Webhook support for external notifications.

### Phase 5 - Intelligence and Advanced Coordination

Goal: add higher-level coordination only after the ledger has real usage data.

Deliverables:

- Semantic search with pgvector or equivalent.
- Historical similarity search.
- Model and harness comparison.
- Cost/performance trends.
- Recommendations based on prior attempts.
- Optional Redis for pub/sub or long polling if needed.
- Smart routing and fairness scoring.
- Workflow policies.
- Team workspaces.
- Advanced RBAC.
- External observability integrations.
- Datastar or another live dashboard layer if proven useful.

---

## 22. Phase Tasks

### 22.1 Phase 1 Tasks - Execution Core

1. Create initial Go repository structure.
2. Add configuration loading and server process boot.
3. Add Postgres migrations for workspaces, projects, tickets, ticket dependencies, attempts, attempt checkpoints, events, idempotency keys, API keys, and metrics.
4. Generate sqlc models and queries for core tables.
5. Implement ticket creation, proposal, validation, and listing.
6. Implement ticket quality fields: acceptance criteria, verification commands, expected artifacts, relevant paths, required tools, required permissions, and environment.
7. Implement transactional `claim-next` with project, type, tag, harness, capability, dependency, and retry eligibility.
8. Return a claim context bundle from `claim-next`.
9. Implement attempt heartbeat.
10. Implement attempt checkpoint.
11. Implement attempt complete, fail, block, cancel, and expiry transitions.
12. Implement append-only event writes in the same transactions as state changes.
13. Implement idempotency middleware, including claim response replay.
14. Add River worker for stale lease expiry and idempotency cleanup.
15. Add Huma routes for the core execution API.
16. Add CLI commands for the core execution flow.
17. Add concurrency tests proving no duplicate claims.
18. Add crash-recovery tests proving expired leases return eligible work to the queue.
19. Add blocked-work tests proving blocked tickets do not re-enter the claim queue until resolved.

### 22.2 Phase 2 Tasks - Agent Integration

1. Add MCP server mode.
2. Define MCP tool schemas for create, propose, create from attempt, claim, heartbeat, checkpoint, update, complete, fail, block, list, get, attach, decompose, and register capabilities.
3. Add agent capability registration and lookup.
4. Add ticket templates for common agent-created work: bug, feature, documentation, review, investigation, cleanup, and follow-up.
5. Add harness integration examples.
6. Add CLI idempotency key helpers for agent scripts.
7. Add planner-agent decomposition flow.
8. Add Codex convenience commands for claim, checkpoint, complete with proof artifacts, and propose follow-up.
9. Add integration tests for REST, CLI, and MCP parity.

### 22.3 Phase 3 Tasks - Human Operations

1. Define TUI and web UX principles: fast, beautiful, keyboard-friendly, dense, calm, and low ceremony.
2. Build TUI queue view.
3. Build TUI ticket detail view.
4. Build TUI attempt, checkpoint, blocker, and event timeline views.
5. Add review, ready, reopen, unblock, and archive operations.
6. Add proposed-ticket approval and cleanup flows.
7. Add polished server-rendered ticket list and detail pages.
8. Add shareable deep links for tickets, attempts, artifacts, and proposed tickets.
9. Add human auth flow.
10. Add project/workspace admin screens without turning them into project-management dashboards.
11. Add operator documentation for common failure and retry workflows.

### 22.4 Phase 4 Tasks - Artifacts, Search, and Analytics

1. Implement local artifact storage.
2. Implement S3-compatible artifact storage.
3. Add artifact upload, registration, retrieval, and deletion semantics.
4. Add artifact authorization checks.
5. Add Postgres full-text search.
6. Add analytics summary queries.
7. Add metrics ingestion and aggregation.
8. Add artifact browser views.
9. Add webhook delivery jobs.

### 22.5 Phase 5 Tasks - Intelligence and Advanced Coordination

1. Add semantic search once real usage data exists.
2. Add historical similarity and related-attempt lookup.
3. Add model and harness comparison reports.
4. Add recommendation experiments.
5. Add workflow policy support.
6. Add optional realtime update layer if operator workflows need it.
7. Add advanced RBAC and team workspace controls.
8. Add external observability export integrations.

---

## 23. Product Principle

The first execution phases should optimize for one thing:

> Can multiple agents safely claim, execute, retry, and report work without losing state or corrupting history?

Everything else is secondary.

The architecture principle is:

```text
Postgres owns truth.
Go ships the product.
CLI/TUI are first-class.
Web stack stays boring; human UI feels polished.
MCP is an adapter.
No unnecessary platform layer.
```
