-- +goose Up
CREATE INDEX idx_attempts_ticket_id_started_at ON attempts(ticket_id, started_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_attempts_ticket_id_started_at;
