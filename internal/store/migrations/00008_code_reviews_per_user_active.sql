-- +goose Up
-- Allow multiple concurrent reviewers per PR while still rejecting a second
-- ACTIVE review by the SAME user: a repeat click is a DB conflict, a different
-- user is free to join. Replaces the single-active-per-PR index from 00007.
DROP INDEX idx_code_reviews_active;
CREATE UNIQUE INDEX idx_code_reviews_active
    ON code_reviews(pull_request_id, slack_user_id)
    WHERE finished_at IS NULL;

-- +goose Down
DROP INDEX idx_code_reviews_active;
CREATE UNIQUE INDEX idx_code_reviews_active
    ON code_reviews(pull_request_id)
    WHERE finished_at IS NULL;
