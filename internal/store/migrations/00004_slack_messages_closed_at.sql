-- +goose Up
ALTER TABLE slack_messages ADD COLUMN closed_at DATETIME;

-- Backfill updated_at to each open row's registration time. There is no
-- created_at column, but the Slack message ts was assigned when we posted the
-- PR-open notification, so it is the row's creation time. Deriving updated_at
-- from it gives the stuck-PR digest a correct age baseline instead of the
-- 00003 migration-run timestamp. closed_at is brand new here (all NULL), so
-- this touches every registered PR; the GLOB guard skips any non-numeric ts.
UPDATE slack_messages
   SET updated_at = datetime(CAST(ts AS REAL), 'unixepoch')
 WHERE closed_at IS NULL AND ts GLOB '[0-9]*';

CREATE INDEX idx_slack_messages_closed_at ON slack_messages(closed_at);

-- +goose Down
DROP INDEX idx_slack_messages_closed_at;
ALTER TABLE slack_messages DROP COLUMN closed_at;
