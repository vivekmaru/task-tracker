-- name: CreateArtifact :one
INSERT INTO artifacts (
    workspace_id,
    project_id,
    ticket_id,
    attempt_id,
    type,
    role,
    name,
    url,
    storage_backend,
    size_bytes,
    mime_type,
    metadata
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: ListArtifactsByTicket :many
SELECT *
FROM artifacts
WHERE ticket_id = $1
ORDER BY created_at ASC;

-- name: ListArtifactsByAttempt :many
SELECT *
FROM artifacts
WHERE attempt_id = $1
ORDER BY created_at ASC;

-- name: GetArtifact :one
SELECT *
FROM artifacts
WHERE id = $1;
