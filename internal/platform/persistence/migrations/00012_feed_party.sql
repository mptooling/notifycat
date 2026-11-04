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
    PRIMARY KEY (day, repository, token_id)
);

CREATE TABLE feed_fetches (
    token_id   TEXT     PRIMARY KEY REFERENCES access_tokens(id) ON DELETE CASCADE,
    fetched_at DATETIME NOT NULL
);

CREATE INDEX feed_subscriptions_repository_type ON feed_subscriptions(repository, type, token_id);

-- +goose Down
DROP INDEX feed_subscriptions_repository_type;
DROP TABLE feed_fetches;
DROP TABLE feed_party_loads;
ALTER TABLE feed_deliveries DROP COLUMN party;
ALTER TABLE feed_pull_requests DROP COLUMN party_deadline_at;
ALTER TABLE feed_pull_requests DROP COLUMN recorded_at;
ALTER TABLE feed_pull_requests DROP COLUMN party_reviewers;
ALTER TABLE feed_subscriptions DROP COLUMN type;
