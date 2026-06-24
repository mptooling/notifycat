// Package store owns the database schema, GORM models, repositories, and the
// goose-driven migration runner. Callers depend on small interfaces declared
// where they consume the store (handler packages); only this package needs to
// know about GORM.
package store

import (
	"errors"
	"time"
)

// ErrNotFound is returned when a lookup matches no row.
var ErrNotFound = errors.New("store: not found")

// SlackMessage tracks the Slack message thread-timestamp for one PR in one
// repository. (PRNumber, Repository) is the composite primary key — replaying
// the same PR event simply updates the TS.
//
// UpdatedAt is bumped on Save (autoUpdateTime) and on Touch (review/comment
// activity); it drives both the scheduled cleanup of stale rows and the
// stuck-PR digest's idle detection. ClosedAt is set when the PR is
// merged/closed so the digest skips it; nil means the PR is still open.
type SlackMessage struct {
	PRNumber   int        `gorm:"column:pr_number;primaryKey"`
	Repository string     `gorm:"column:gh_repository;primaryKey"`
	TS         string     `gorm:"column:ts;not null"`
	UpdatedAt  time.Time  `gorm:"column:updated_at;autoUpdateTime;not null"`
	ClosedAt   *time.Time `gorm:"column:closed_at"`
}

// TableName pins the table name to match the migration; do not rely on GORM's
// pluralisation heuristics.
func (SlackMessage) TableName() string { return "slack_messages" }

// RepoMapping is the value object handlers and validators consume — a GitHub
// repository routed to a Slack channel with an optional mentions list. The
// source of truth lives in mappings.yaml (loaded by internal/mappings); the
// type stays here so consumers don't have to know who produced it.
type RepoMapping struct {
	Repository   string
	SlackChannel string
	Mentions     []string
}
