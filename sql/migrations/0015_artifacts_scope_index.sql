-- +goose Up
CREATE INDEX idx_artifacts_scope_created_at ON artifacts(workspace_id, project_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_artifacts_scope_created_at;
