-- +goose Up
CREATE TABLE github_slack_mapping (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    repository    TEXT NOT NULL UNIQUE,
    slack_channel TEXT NOT NULL,
    mentions      TEXT NOT NULL DEFAULT '[]'
);

CREATE TABLE slack_messages (
    pr_number     INTEGER NOT NULL,
    gh_repository TEXT    NOT NULL,
    ts            TEXT    NOT NULL,
    PRIMARY KEY (pr_number, gh_repository)
);

-- +goose Down
DROP TABLE slack_messages;
DROP TABLE github_slack_mapping;
