-- name: CreateTicketEvent :one
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
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListTicketEventsByTicket :many
SELECT *
FROM ticket_events
WHERE ticket_id = $1
ORDER BY created_at ASC, id ASC;

-- name: ListRecentTicketEvents :many
SELECT *
FROM (
    SELECT *
    FROM ticket_events
    WHERE (sqlc.narg('workspace_id')::uuid IS NULL OR workspace_id = sqlc.narg('workspace_id')::uuid)
      AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id')::uuid)
      AND (sqlc.narg('ticket_id')::uuid IS NULL OR ticket_id = sqlc.narg('ticket_id')::uuid)
      AND (sqlc.narg('attempt_id')::uuid IS NULL OR attempt_id = sqlc.narg('attempt_id')::uuid)
    ORDER BY created_at DESC, id DESC
    LIMIT sqlc.arg('limit_count')::integer
) recent
ORDER BY created_at ASC, id ASC;

-- name: ListTicketEventsAfterCursor :many
SELECT *
FROM ticket_events
WHERE (sqlc.narg('workspace_id')::uuid IS NULL OR workspace_id = sqlc.narg('workspace_id')::uuid)
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id')::uuid)
  AND (sqlc.narg('ticket_id')::uuid IS NULL OR ticket_id = sqlc.narg('ticket_id')::uuid)
  AND (sqlc.narg('attempt_id')::uuid IS NULL OR attempt_id = sqlc.narg('attempt_id')::uuid)
  AND created_at >= sqlc.arg('after_created_at')::timestamptz
ORDER BY created_at ASC, id ASC
LIMIT sqlc.arg('limit_count')::integer;
