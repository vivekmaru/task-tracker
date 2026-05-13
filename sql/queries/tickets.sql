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
