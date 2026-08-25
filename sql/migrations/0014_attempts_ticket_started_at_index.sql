-- +goose Up
DROP INDEX IF EXISTS idx_attempts_ticket_id;
CREATE INDEX idx_attempts_ticket_started_at ON attempts(ticket_id, started_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_attempts_ticket_started_at;
CREATE INDEX idx_attempts_ticket_id ON attempts(ticket_id);
