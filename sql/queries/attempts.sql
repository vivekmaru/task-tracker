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

-- name: HeartbeatAttempt :one
WITH updated_attempt AS (
    UPDATE attempts a
    SET lease_expires_at = sqlc.arg(lease_expires_at)::timestamptz,
        last_heartbeat_at = sqlc.arg(heartbeat_at)::timestamptz
    WHERE a.id = sqlc.arg(attempt_id)::uuid
      AND a.status = 'running'
    RETURNING *
),
heartbeat_event AS (
    INSERT INTO ticket_events (
        workspace_id,
        project_id,
        ticket_id,
        attempt_id,
        type,
        actor_type,
        actor_id,
        data
    )
    SELECT
        a.workspace_id,
        a.project_id,
        a.ticket_id,
        a.id,
        'heartbeat',
        'agent',
        a.agent_id,
        jsonb_build_object(
            'lease_expires_at', a.lease_expires_at,
            'heartbeat_at', a.last_heartbeat_at
        )
    FROM updated_attempt a
    RETURNING id
)
SELECT *
FROM updated_attempt;

-- name: CheckpointAttempt :one
WITH updated_attempt AS (
    UPDATE attempts a
    SET progress_percent = sqlc.arg(progress_percent)::integer,
        current_summary = sqlc.arg(summary)::text,
        next_step = sqlc.narg(next_step)::text
    WHERE a.id = sqlc.arg(attempt_id)::uuid
      AND a.status = 'running'
    RETURNING *
),
created_checkpoint AS (
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
    SELECT
        a.workspace_id,
        a.project_id,
        a.ticket_id,
        a.id,
        sqlc.arg(summary)::text,
        sqlc.arg(files_touched)::text[],
        sqlc.arg(commands_run)::text[],
        sqlc.narg(next_step)::text,
        sqlc.narg(risk)::text
    FROM updated_attempt a
    RETURNING *
),
checkpoint_event AS (
    INSERT INTO ticket_events (
        workspace_id,
        project_id,
        ticket_id,
        attempt_id,
        type,
        actor_type,
        actor_id,
        data
    )
    SELECT
        a.workspace_id,
        a.project_id,
        a.ticket_id,
        a.id,
        'checkpointed',
        'agent',
        a.agent_id,
        jsonb_build_object(
            'checkpoint_id', c.id,
            'progress_percent', a.progress_percent
        )
    FROM updated_attempt a
    JOIN created_checkpoint c ON c.attempt_id = a.id
    RETURNING id
)
SELECT *
FROM created_checkpoint;

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
