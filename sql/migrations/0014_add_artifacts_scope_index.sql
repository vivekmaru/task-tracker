-- +goose Up
CREATE INDEX idx_artifacts_scope ON artifacts(workspace_id, project_id, ticket_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_artifacts_scope;
