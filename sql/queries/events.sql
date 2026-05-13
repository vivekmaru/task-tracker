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
ORDER BY created_at ASC;
