-- name: CreateTicket :one
INSERT INTO tickets (
    workspace_id,
    project_id,
    parent_id,
    root_id,
    source_attempt_id,
    source_artifact_id,
    title,
    description,
    type,
    status,
    priority,
    tags,
    acceptance_criteria,
    verification_commands,
    expected_artifacts,
    relevant_paths,
    required_tools,
    required_permissions,
    environment,
    input,
    input_schema,
    required_capabilities,
    allowed_harnesses,
    retry_policy,
    created_by,
    created_by_id,
    creation_reason
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
    $11, $12, $13, $14, $15, $16, $17, $18, $19, $20,
    $21, $22, $23, $24, $25, $26, $27
)
RETURNING *;

-- name: GetTicket :one
SELECT *
FROM tickets
WHERE id = $1;

-- name: UpdateTicket :one
UPDATE tickets
SET title = COALESCE(sqlc.narg('title')::text, title),
    description = COALESCE(sqlc.narg('description')::text, description),
    tags = CASE
        WHEN sqlc.arg('update_tags')::boolean THEN sqlc.arg('tags')::text[]
        ELSE tags
    END,
    acceptance_criteria = CASE
        WHEN sqlc.arg('update_acceptance_criteria')::boolean THEN sqlc.arg('acceptance_criteria')::text[]
        ELSE acceptance_criteria
    END,
    verification_commands = CASE
        WHEN sqlc.arg('update_verification_commands')::boolean THEN sqlc.arg('verification_commands')::jsonb
        ELSE verification_commands
    END,
    relevant_paths = CASE
        WHEN sqlc.arg('update_relevant_paths')::boolean THEN sqlc.arg('relevant_paths')::text[]
        ELSE relevant_paths
    END,
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: TransitionTicket :one
WITH updated_ticket AS (
    UPDATE tickets
    SET status = sqlc.arg('status')::text,
        updated_at = now()
    WHERE id = sqlc.arg('id')::uuid
      AND status = ANY(sqlc.arg('allowed_statuses')::text[])
    RETURNING *
),
transition_event AS (
    INSERT INTO ticket_events (
        workspace_id,
        project_id,
        ticket_id,
        type,
        actor_type,
        actor_id,
        data
    )
    SELECT
        workspace_id,
        project_id,
        id,
        sqlc.arg('type')::text,
        sqlc.arg('actor_type')::text,
        sqlc.narg('actor_id')::text,
        sqlc.arg('data')::jsonb
    FROM updated_ticket
    RETURNING id
)
SELECT *
FROM updated_ticket;

-- name: ListTickets :many
SELECT *
FROM tickets
WHERE workspace_id = $1
  AND project_id = $2
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('type')::text IS NULL OR type = sqlc.narg('type')::text)
ORDER BY priority ASC, created_at ASC
LIMIT sqlc.arg('limit')::integer
OFFSET sqlc.arg('offset')::integer;

-- name: ListProposedTickets :many
SELECT *
FROM tickets
WHERE workspace_id = $1
  AND project_id = $2
  AND status = 'backlog'
  AND created_by = 'agent'
  AND (sqlc.narg('type')::text IS NULL OR type = sqlc.narg('type')::text)
ORDER BY priority ASC, created_at ASC
LIMIT sqlc.arg('limit')::integer
OFFSET sqlc.arg('offset')::integer;

-- name: CreateTicketDependency :one
INSERT INTO ticket_dependencies (
    ticket_id,
    depends_on_ticket_id,
    workspace_id,
    project_id
)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListTicketDependencies :many
SELECT *
FROM ticket_dependencies
WHERE ticket_id = $1
ORDER BY created_at ASC;
