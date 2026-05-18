-- +goose Up
DROP TABLE github_slack_mapping;

-- +goose Down
CREATE TABLE github_slack_mapping (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    repository    TEXT NOT NULL UNIQUE,
    slack_channel TEXT NOT NULL,
    mentions      TEXT NOT NULL DEFAULT '[]'
);
