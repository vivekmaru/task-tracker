-- name: CreateWorkspace :one
INSERT INTO workspaces (name)
VALUES ($1)
RETURNING *;

-- name: GetWorkspace :one
SELECT *
FROM workspaces
WHERE id = $1;

-- name: CreateProject :one
INSERT INTO projects (workspace_id, name)
VALUES ($1, $2)
RETURNING *;

-- name: GetProject :one
SELECT *
FROM projects
WHERE id = $1;
