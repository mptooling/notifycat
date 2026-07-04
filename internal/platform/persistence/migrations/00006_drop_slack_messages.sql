-- +goose Up
DROP TABLE slack_messages;

-- +goose Down
CREATE TABLE slack_messages (
    pr_number     INTEGER  NOT NULL,
    gh_repository TEXT     NOT NULL,
    ts            TEXT     NOT NULL,
    updated_at    DATETIME,
    closed_at     DATETIME,
    PRIMARY KEY (pr_number, gh_repository)
);
