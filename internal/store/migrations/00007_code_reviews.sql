-- +goose Up
CREATE TABLE code_reviews (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    pull_request_id INTEGER  NOT NULL REFERENCES pull_requests(id) ON DELETE CASCADE,
    slack_user_id   TEXT     NOT NULL,
    slack_user_name TEXT,
    started_at      DATETIME NOT NULL,
    finished_at     DATETIME
);

-- Single active reviewer per PR: only open rows (finished_at IS NULL) take part
-- in the uniqueness constraint, so a second concurrent Start on the same PR is
-- rejected by the DB, while finished rows fall out of the index and free the PR
-- to be reviewed again. SQLite supports partial indexes.
CREATE UNIQUE INDEX idx_code_reviews_active
    ON code_reviews(pull_request_id)
    WHERE finished_at IS NULL;

CREATE INDEX idx_code_reviews_pull_request_id ON code_reviews(pull_request_id);

-- +goose Down
DROP TABLE code_reviews;
