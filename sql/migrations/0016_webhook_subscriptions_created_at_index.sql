-- +goose Up
CREATE INDEX idx_webhook_subscriptions_scope_created_at ON webhook_subscriptions(workspace_id, project_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_webhook_subscriptions_scope_created_at;
