-- +goose Up
CREATE TABLE access_invites (
    id          TEXT     PRIMARY KEY,
    user        TEXT     NOT NULL,
    secret_hash TEXT     NOT NULL,
    expires_at  DATETIME NOT NULL,
    used_at     DATETIME,
    created_at  DATETIME NOT NULL
);

CREATE TABLE access_tokens (
    id           TEXT     PRIMARY KEY,
    user         TEXT     NOT NULL,
    secret_hash  TEXT     NOT NULL,
    created_at   DATETIME NOT NULL,
    last_used_at DATETIME
);

-- +goose Down
DROP TABLE access_tokens;
DROP TABLE access_invites;
