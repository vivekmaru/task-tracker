-- name: CreateWorkspace :one
INSERT INTO workspaces (name)
VALUES ($1)
RETURNING *;

-- name: GetWorkspace :one
SELECT *
FROM workspaces
WHERE id = $1;

-- name: ListWorkspaces :many
SELECT *
FROM workspaces
ORDER BY name ASC;

-- name: CreateProject :one
INSERT INTO projects (workspace_id, name)
VALUES ($1, $2)
RETURNING *;

-- name: GetProject :one
SELECT *
FROM projects
WHERE id = $1;

-- name: ListProjectsByWorkspace :many
SELECT *
FROM projects
WHERE workspace_id = $1
ORDER BY name ASC;

-- name: UpsertWorkspaceMember :one
INSERT INTO workspace_members (workspace_id, actor_type, actor_id, role)
VALUES ($1, $2, $3, $4)
ON CONFLICT (workspace_id, actor_type, actor_id)
DO UPDATE SET role = EXCLUDED.role, updated_at = now()
RETURNING *;

-- name: ListWorkspaceMembers :many
SELECT *
FROM workspace_members
WHERE workspace_id = $1
ORDER BY
    CASE role
        WHEN 'owner' THEN 1
        WHEN 'admin' THEN 2
        WHEN 'member' THEN 3
        WHEN 'viewer' THEN 4
        ELSE 5
    END,
    actor_type ASC,
    actor_id ASC;

-- name: UpdateWorkspaceMemberRole :one
UPDATE workspace_members
SET role = $4, updated_at = now()
WHERE workspace_id = $1
  AND actor_type = $2
  AND actor_id = $3
RETURNING *;

-- name: DeleteWorkspaceMember :exec
DELETE FROM workspace_members
WHERE workspace_id = $1
  AND actor_type = $2
  AND actor_id = $3;
