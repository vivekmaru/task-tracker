-- name: CreateAttempt :one
INSERT INTO attempts (
    workspace_id,
    project_id,
    ticket_id,
    agent_id,
    harness,
    model,
    status,
    lease_expires_at,
    last_heartbeat_at,
    progress_percent,
    current_summary,
    next_step,
    output,
    output_schema,
    failure_reason,
    failure_category,
    blocker,
    trace_id,
    checkpoint_ref,
    completed_at
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
    $11, $12, $13, $14, $15, $16, $17, $18, $19, $20
)
RETURNING *;

-- name: GetAttempt :one
SELECT *
FROM attempts
WHERE id = $1;

-- name: ListAttemptsByTicket :many
SELECT *
FROM attempts
WHERE ticket_id = $1
ORDER BY started_at DESC;

-- name: CreateAttemptCheckpoint :one
INSERT INTO attempt_checkpoints (
    workspace_id,
    project_id,
    ticket_id,
    attempt_id,
    summary,
    files_touched,
    commands_run,
    next_step,
    risk
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: ListAttemptCheckpointsByAttempt :many
SELECT *
FROM attempt_checkpoints
WHERE attempt_id = $1
ORDER BY created_at ASC;

-- name: ListAttemptCheckpointsByTicket :many
SELECT *
FROM attempt_checkpoints
WHERE ticket_id = $1
ORDER BY created_at ASC;
