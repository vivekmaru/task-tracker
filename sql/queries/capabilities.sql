-- name: UpsertAgentCapability :one
INSERT INTO agent_capabilities (
    workspace_id,
    project_id,
    agent_id,
    harness,
    model,
    transports,
    capabilities,
    tool_names,
    artifact_roles,
    preferred_claim,
    metadata,
    last_seen_at
) VALUES (
    sqlc.arg(workspace_id),
    sqlc.arg(project_id),
    sqlc.arg(agent_id),
    sqlc.arg(harness),
    sqlc.arg(model),
    sqlc.arg(transports),
    sqlc.arg(capabilities),
    sqlc.arg(tool_names),
    sqlc.arg(artifact_roles),
    sqlc.arg(preferred_claim),
    sqlc.arg(metadata),
    sqlc.arg(last_seen_at)
)
ON CONFLICT (workspace_id, project_id, agent_id, harness)
DO UPDATE SET
    model = EXCLUDED.model,
    transports = EXCLUDED.transports,
    capabilities = EXCLUDED.capabilities,
    tool_names = EXCLUDED.tool_names,
    artifact_roles = EXCLUDED.artifact_roles,
    preferred_claim = EXCLUDED.preferred_claim,
    metadata = EXCLUDED.metadata,
    last_seen_at = EXCLUDED.last_seen_at,
    updated_at = now()
RETURNING *;

-- name: GetAgentCapability :one
SELECT *
FROM agent_capabilities
WHERE workspace_id = sqlc.arg(workspace_id)
  AND project_id = sqlc.arg(project_id)
  AND agent_id = sqlc.arg(agent_id)
  AND harness = sqlc.arg(harness);

-- name: ListAgentCapabilities :many
SELECT *
FROM agent_capabilities
WHERE workspace_id = sqlc.arg(workspace_id)
  AND project_id = sqlc.arg(project_id)
  AND (sqlc.narg(harness)::text IS NULL OR harness = sqlc.narg(harness))
  AND (sqlc.narg(capability)::text IS NULL OR sqlc.narg(capability) = ANY(capabilities))
ORDER BY harness, agent_id;
