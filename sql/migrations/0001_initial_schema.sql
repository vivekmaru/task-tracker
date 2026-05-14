-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE workspaces (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (name)
);

CREATE TABLE projects (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, name)
);

CREATE TABLE tickets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    parent_id uuid REFERENCES tickets(id) ON DELETE SET NULL,
    root_id uuid,
    source_attempt_id uuid,
    source_artifact_id uuid,
    title text NOT NULL,
    description text NOT NULL DEFAULT '',
    type text NOT NULL,
    status text NOT NULL DEFAULT 'backlog',
    priority integer NOT NULL DEFAULT 2,
    tags text[] NOT NULL DEFAULT '{}'::text[],
    acceptance_criteria text[] NOT NULL DEFAULT '{}'::text[],
    verification_commands jsonb NOT NULL DEFAULT '[]'::jsonb,
    expected_artifacts text[] NOT NULL DEFAULT '{}'::text[],
    relevant_paths text[] NOT NULL DEFAULT '{}'::text[],
    required_tools text[] NOT NULL DEFAULT '{}'::text[],
    required_permissions text[] NOT NULL DEFAULT '{}'::text[],
    environment jsonb NOT NULL DEFAULT '{}'::jsonb,
    input jsonb NOT NULL DEFAULT '{}'::jsonb,
    input_schema text,
    required_capabilities text[] NOT NULL DEFAULT '{}'::text[],
    allowed_harnesses text[] NOT NULL DEFAULT '{}'::text[],
    retry_policy jsonb NOT NULL DEFAULT '{"max_attempts":3,"on_failure":"return_to_todo","requires_review_on_success":false}'::jsonb,
    created_by text NOT NULL,
    created_by_id text,
    creation_reason text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (type IN ('feature', 'bug', 'documentation', 'research', 'analysis', 'planning', 'review', 'integration', 'custom')),
    CHECK (status IN ('backlog', 'todo', 'in_progress', 'blocked', 'needs_review', 'done', 'failed', 'archived')),
    CHECK (priority BETWEEN 0 AND 4),
    CHECK (created_by IN ('human', 'agent', 'system'))
);

CREATE TABLE ticket_dependencies (
    ticket_id uuid NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    depends_on_ticket_id uuid NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (ticket_id, depends_on_ticket_id),
    CHECK (ticket_id <> depends_on_ticket_id)
);

CREATE TABLE attempts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    ticket_id uuid NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    agent_id text NOT NULL,
    harness text NOT NULL,
    model text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'running',
    lease_expires_at timestamptz NOT NULL,
    last_heartbeat_at timestamptz,
    progress_percent integer NOT NULL DEFAULT 0,
    current_summary text,
    next_step text,
    output jsonb NOT NULL DEFAULT '{}'::jsonb,
    output_schema text,
    failure_reason text,
    failure_category text,
    blocker jsonb,
    trace_id text,
    checkpoint_ref text,
    started_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    CHECK (status IN ('running', 'succeeded', 'failed', 'blocked', 'expired', 'cancelled')),
    CHECK (progress_percent BETWEEN 0 AND 100),
    CHECK (
        failure_category IS NULL
        OR failure_category IN (
            'task_failed',
            'blocked',
            'needs_human',
            'environment_failed',
            'permission_required',
            'dependency_missing',
            'unclear_requirements'
        )
    )
);

CREATE TABLE attempt_checkpoints (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    ticket_id uuid NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    attempt_id uuid NOT NULL REFERENCES attempts(id) ON DELETE CASCADE,
    summary text NOT NULL,
    files_touched text[] NOT NULL DEFAULT '{}'::text[],
    commands_run text[] NOT NULL DEFAULT '{}'::text[],
    next_step text,
    risk text,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE ticket_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    ticket_id uuid NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    attempt_id uuid REFERENCES attempts(id) ON DELETE SET NULL,
    type text NOT NULL,
    actor_type text NOT NULL,
    actor_id text,
    data jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (type IN ('created', 'proposed', 'claimed', 'heartbeat', 'checkpointed', 'updated', 'completed', 'failed', 'blocked', 'expired', 'reviewed', 'archived')),
    CHECK (actor_type IN ('human', 'agent', 'system'))
);

CREATE TABLE artifacts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    ticket_id uuid NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    attempt_id uuid REFERENCES attempts(id) ON DELETE SET NULL,
    type text NOT NULL,
    role text NOT NULL,
    name text NOT NULL,
    url text NOT NULL,
    storage_backend text NOT NULL,
    size_bytes bigint NOT NULL DEFAULT 0,
    mime_type text NOT NULL DEFAULT '',
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (type IN ('code', 'document', 'image', 'dataset', 'log', 'diff', 'trace', 'test_output', 'screenshot', 'handoff', 'diagnostic', 'final_response', 'other')),
    CHECK (role IN ('evidence', 'patch', 'context', 'output', 'diagnostic', 'handoff')),
    CHECK (storage_backend IN ('local', 's3')),
    CHECK (size_bytes >= 0)
);

CREATE TABLE idempotency_keys (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    actor_id text NOT NULL,
    key text NOT NULL,
    route text NOT NULL,
    request_hash text NOT NULL,
    response_body jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    UNIQUE (workspace_id, actor_id, route, key)
);

CREATE TABLE agent_capabilities (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    agent_id text NOT NULL,
    harness text NOT NULL,
    model text NOT NULL DEFAULT '',
    transports text[] NOT NULL DEFAULT '{}'::text[],
    capabilities text[] NOT NULL DEFAULT '{}'::text[],
    tool_names text[] NOT NULL DEFAULT '{}'::text[],
    artifact_roles text[] NOT NULL DEFAULT '{}'::text[],
    preferred_claim jsonb NOT NULL DEFAULT '{}'::jsonb,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, project_id, agent_id, harness)
);

CREATE TABLE api_keys (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name text NOT NULL,
    key_hash text NOT NULL,
    actor_type text NOT NULL,
    actor_id text NOT NULL,
    scopes text[] NOT NULL DEFAULT '{}'::text[],
    created_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    revoked_at timestamptz,
    UNIQUE (workspace_id, key_hash),
    CHECK (actor_type IN ('human', 'agent', 'system'))
);

CREATE TABLE attempt_metrics (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    attempt_id uuid NOT NULL REFERENCES attempts(id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    tokens_in bigint NOT NULL DEFAULT 0,
    tokens_out bigint NOT NULL DEFAULT 0,
    cost_usd numeric(12, 6) NOT NULL DEFAULT 0,
    duration_seconds numeric(12, 3) NOT NULL DEFAULT 0,
    retry_count integer NOT NULL DEFAULT 0,
    agent_success_score numeric(5, 4),
    human_rating integer,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (tokens_in >= 0),
    CHECK (tokens_out >= 0),
    CHECK (cost_usd >= 0),
    CHECK (duration_seconds >= 0),
    CHECK (retry_count >= 0),
    CHECK (human_rating IS NULL OR human_rating BETWEEN 1 AND 5)
);

CREATE INDEX idx_projects_workspace_id ON projects(workspace_id);

CREATE INDEX idx_tickets_claim_queue
    ON tickets(workspace_id, project_id, status, priority DESC, created_at ASC)
    WHERE status = 'todo';
CREATE INDEX idx_tickets_tags ON tickets USING gin(tags);
CREATE INDEX idx_tickets_required_capabilities ON tickets USING gin(required_capabilities);
CREATE INDEX idx_tickets_allowed_harnesses ON tickets USING gin(allowed_harnesses);
CREATE INDEX idx_tickets_parent_id ON tickets(parent_id);

CREATE INDEX idx_ticket_dependencies_ticket_id ON ticket_dependencies(ticket_id);
CREATE INDEX idx_ticket_dependencies_depends_on_ticket_id ON ticket_dependencies(depends_on_ticket_id);

CREATE UNIQUE INDEX idx_attempts_running_by_ticket
    ON attempts(ticket_id)
    WHERE status = 'running';
CREATE INDEX idx_attempts_ticket_id ON attempts(ticket_id);
CREATE INDEX idx_attempts_lease_expiry ON attempts(status, lease_expires_at)
    WHERE status = 'running';
CREATE INDEX idx_attempts_agent_harness ON attempts(workspace_id, project_id, agent_id, harness);

CREATE INDEX idx_attempt_checkpoints_attempt_id ON attempt_checkpoints(attempt_id, created_at);

CREATE INDEX idx_ticket_events_ticket_id ON ticket_events(ticket_id, created_at);
CREATE INDEX idx_ticket_events_attempt_id ON ticket_events(attempt_id, created_at);
CREATE INDEX idx_ticket_events_type ON ticket_events(workspace_id, project_id, type, created_at);

CREATE INDEX idx_artifacts_ticket_id ON artifacts(ticket_id, created_at);
CREATE INDEX idx_artifacts_attempt_id ON artifacts(attempt_id, created_at);

CREATE INDEX idx_idempotency_keys_lookup ON idempotency_keys(workspace_id, actor_id, route, key);
CREATE INDEX idx_idempotency_keys_expires_at ON idempotency_keys(expires_at);

CREATE INDEX idx_agent_capabilities_agent ON agent_capabilities(workspace_id, project_id, agent_id);
CREATE INDEX idx_agent_capabilities_capabilities ON agent_capabilities USING gin(capabilities);

CREATE INDEX idx_api_keys_workspace_id ON api_keys(workspace_id);
CREATE INDEX idx_attempt_metrics_attempt_id ON attempt_metrics(attempt_id);

-- +goose Down
DROP TABLE IF EXISTS attempt_metrics;
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS agent_capabilities;
DROP TABLE IF EXISTS idempotency_keys;
DROP TABLE IF EXISTS artifacts;
DROP TABLE IF EXISTS ticket_events;
DROP TABLE IF EXISTS attempt_checkpoints;
DROP TABLE IF EXISTS attempts;
DROP TABLE IF EXISTS ticket_dependencies;
DROP TABLE IF EXISTS tickets;
DROP TABLE IF EXISTS projects;
DROP TABLE IF EXISTS workspaces;
