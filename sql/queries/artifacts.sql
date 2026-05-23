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

-- name: ListArtifactsByScope :many
SELECT *
FROM artifacts
WHERE workspace_id = sqlc.arg(workspace_id)
  AND project_id = sqlc.arg(project_id)
  AND (sqlc.narg(ticket_id)::uuid IS NULL OR ticket_id = sqlc.narg(ticket_id)::uuid)
ORDER BY created_at DESC
LIMIT sqlc.arg(limit_count)
OFFSET sqlc.arg(offset_count);

-- name: GetArtifact :one
SELECT *
FROM artifacts
WHERE id = $1;

-- name: DeleteArtifact :exec
DELETE FROM artifacts
WHERE id = $1;
