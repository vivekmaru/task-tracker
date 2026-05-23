-- +goose Up
ALTER TABLE tickets
    DROP CONSTRAINT IF EXISTS tickets_type_check,
    ADD CONSTRAINT tickets_type_check CHECK (type IN ('feature', 'task', 'bug', 'documentation', 'research', 'analysis', 'planning', 'review', 'integration', 'investigation', 'cleanup', 'follow_up', 'custom'));

-- +goose Down
ALTER TABLE tickets
    DROP CONSTRAINT IF EXISTS tickets_type_check,
    ADD CONSTRAINT tickets_type_check CHECK (type IN ('feature', 'bug', 'documentation', 'research', 'analysis', 'planning', 'review', 'integration', 'investigation', 'cleanup', 'follow_up', 'custom'));
