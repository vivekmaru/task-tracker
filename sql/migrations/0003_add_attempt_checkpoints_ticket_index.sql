-- +goose Up
CREATE INDEX idx_attempt_checkpoints_ticket_id ON attempt_checkpoints(ticket_id, created_at);

-- +goose Down
DROP INDEX IF EXISTS idx_attempt_checkpoints_ticket_id;
