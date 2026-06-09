-- +goose Up
-- Add composite index to optimize the ListAttemptCheckpointsByTicket query,
-- which filters by ticket_id and orders by created_at. This prevents full table
-- scans as the append-only attempt_checkpoints table grows.
CREATE INDEX idx_attempt_checkpoints_ticket_id ON attempt_checkpoints(ticket_id, created_at);

-- +goose Down
DROP INDEX IF EXISTS idx_attempt_checkpoints_ticket_id;
