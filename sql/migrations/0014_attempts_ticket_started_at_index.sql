-- +goose Up
CREATE INDEX idx_attempts_ticket_started_at ON attempts(ticket_id, started_at DESC);
DROP INDEX IF EXISTS idx_attempts_ticket_id;

-- +goose Down
CREATE INDEX idx_attempts_ticket_id ON attempts(ticket_id);
DROP INDEX IF EXISTS idx_attempts_ticket_started_at;
