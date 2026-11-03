-- +goose Up
ALTER TABLE feed_subscriptions ADD COLUMN type TEXT NOT NULL DEFAULT 'solo';
ALTER TABLE feed_pull_requests ADD COLUMN party_reviewers INTEGER NOT NULL DEFAULT 1;
ALTER TABLE feed_pull_requests ADD COLUMN recorded_at DATETIME;
ALTER TABLE feed_pull_requests ADD COLUMN party_deadline_at DATETIME;
ALTER TABLE feed_deliveries ADD COLUMN party BOOLEAN NOT NULL DEFAULT 0;

CREATE TABLE feed_party_loads (
    token_id   TEXT    NOT NULL REFERENCES access_tokens(id) ON DELETE CASCADE,
    repository TEXT    NOT NULL,
    day        TEXT    NOT NULL,
    taken      INTEGER NOT NULL,
    PRIMARY KEY (token_id, repository, day)
);

CREATE TABLE feed_polls (
    token_id  TEXT     PRIMARY KEY REFERENCES access_tokens(id) ON DELETE CASCADE,
    polled_at DATETIME NOT NULL
);

-- +goose Down
DROP TABLE feed_polls;
DROP TABLE feed_party_loads;
ALTER TABLE feed_deliveries DROP COLUMN party;
ALTER TABLE feed_pull_requests DROP COLUMN party_deadline_at;
ALTER TABLE feed_pull_requests DROP COLUMN recorded_at;
ALTER TABLE feed_pull_requests DROP COLUMN party_reviewers;
ALTER TABLE feed_subscriptions DROP COLUMN type;
