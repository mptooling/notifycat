-- +goose Up
CREATE TABLE pull_requests (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    gh_repository TEXT     NOT NULL,
    pr_number     INTEGER  NOT NULL,
    created_at    DATETIME NOT NULL,
    updated_at    DATETIME NOT NULL,
    closed_at     DATETIME
);
CREATE UNIQUE INDEX idx_pull_requests_repo_number ON pull_requests(gh_repository, pr_number);
CREATE INDEX idx_pull_requests_updated_at ON pull_requests(updated_at);
CREATE INDEX idx_pull_requests_closed_at ON pull_requests(closed_at);

CREATE TABLE messages (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    pull_request_id INTEGER NOT NULL REFERENCES pull_requests(id) ON DELETE CASCADE,
    channel         TEXT    NOT NULL,
    message_id      TEXT    NOT NULL
);
CREATE UNIQUE INDEX idx_messages_pr_channel ON messages(pull_request_id, channel);

-- +goose Down
DROP TABLE messages;
DROP TABLE pull_requests;
