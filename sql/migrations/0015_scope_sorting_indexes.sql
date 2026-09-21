-- +goose Up
CREATE INDEX idx_artifacts_scope_created_at ON artifacts(workspace_id, project_id, created_at DESC);
CREATE INDEX idx_tickets_scope_priority_created ON tickets(workspace_id, project_id, priority ASC, created_at ASC);
CREATE INDEX idx_webhook_subscriptions_scope_created_at ON webhook_subscriptions(workspace_id, project_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_webhook_subscriptions_scope_created_at;
DROP INDEX IF EXISTS idx_tickets_scope_priority_created;
DROP INDEX IF EXISTS idx_artifacts_scope_created_at;
