-- +goose Up
-- One queued PR announcement per channel for repos on the scheduled
-- notification strategy. sent_at stays NULL until a tick posts the channel's
-- list; the row is kept after the send so a webhook redelivery is not queued
-- again, and so the digest can skip a PR announced today.
CREATE TABLE scheduled_notifications (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    pull_request_id INTEGER  NOT NULL REFERENCES pull_requests(id) ON DELETE CASCADE,
    channel         TEXT     NOT NULL,
    mentions        TEXT     NOT NULL,
    title           TEXT     NOT NULL,
    url             TEXT     NOT NULL,
    author          TEXT     NOT NULL,
    queued_at       DATETIME NOT NULL,
    sent_at         DATETIME
);
CREATE UNIQUE INDEX idx_scheduled_notifications_pr_channel ON scheduled_notifications(pull_request_id, channel);
CREATE INDEX idx_scheduled_notifications_sent_at ON scheduled_notifications(sent_at);

-- +goose Down
DROP TABLE scheduled_notifications;
