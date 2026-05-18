-- +goose Up
ALTER TABLE slack_messages ADD COLUMN updated_at DATETIME;
UPDATE slack_messages SET updated_at = CURRENT_TIMESTAMP WHERE updated_at IS NULL;
CREATE INDEX idx_slack_messages_updated_at ON slack_messages(updated_at);

-- +goose Down
DROP INDEX idx_slack_messages_updated_at;
ALTER TABLE slack_messages DROP COLUMN updated_at;
