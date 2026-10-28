-- +goose Up
CREATE TABLE feed_subscriptions (
    id         INTEGER  PRIMARY KEY,
    token_id   TEXT     NOT NULL REFERENCES access_tokens(id) ON DELETE CASCADE,
    repository TEXT     NOT NULL,
    created_at DATETIME NOT NULL,
    UNIQUE (token_id, repository)
);

CREATE TABLE feed_pull_requests (
    repository    TEXT     NOT NULL,
    number        INTEGER  NOT NULL,
    title         TEXT,
    description   TEXT,
    url           TEXT,
    author_id     TEXT,
    source_branch TEXT,
    target_branch TEXT,
    head_sha      TEXT     NOT NULL DEFAULT '',
    created_at    DATETIME,
    updated_at    DATETIME NOT NULL,
    PRIMARY KEY (repository, number)
);

CREATE TABLE feed_deliveries (
    token_id     TEXT     NOT NULL REFERENCES access_tokens(id) ON DELETE CASCADE,
    repository   TEXT     NOT NULL,
    number       INTEGER  NOT NULL,
    delivered_at DATETIME NOT NULL,
    PRIMARY KEY (token_id, repository, number),
    FOREIGN KEY (repository, number) REFERENCES feed_pull_requests(repository, number) ON DELETE CASCADE
);

CREATE INDEX feed_deliveries_pull_request ON feed_deliveries(repository, number);

-- +goose Down
DROP TABLE feed_deliveries;
DROP TABLE feed_pull_requests;
DROP TABLE feed_subscriptions;
