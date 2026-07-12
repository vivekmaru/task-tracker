-- +goose Up
ALTER TABLE webhook_deliveries
    ADD COLUMN claim_token uuid;

UPDATE webhook_deliveries
SET status = 'pending',
    locked_until = NULL,
    claim_token = NULL
WHERE status = 'delivering';

CREATE INDEX idx_webhook_deliveries_claim_token
    ON webhook_deliveries(id, claim_token)
    WHERE status = 'delivering';

-- +goose Down
DROP INDEX IF EXISTS idx_webhook_deliveries_claim_token;
ALTER TABLE webhook_deliveries DROP COLUMN IF EXISTS claim_token;
