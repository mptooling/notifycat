// Package store owns the database schema, GORM models, repositories, and the
// goose-driven migration runner. Callers depend on small interfaces declared
// where they consume the store (handler packages); only this package needs to
// know about GORM.
package store

import "errors"

// ErrNotFound is returned when a lookup matches no row.
var ErrNotFound = errors.New("store: not found")

// SlackMessage tracks the Slack message thread-timestamp for one PR in one
// repository. (PRNumber, Repository) is the composite primary key — replaying
// the same PR event simply updates the TS.
type SlackMessage struct {
	PRNumber   int    `gorm:"column:pr_number;primaryKey"`
	Repository string `gorm:"column:gh_repository;primaryKey"`
	TS         string `gorm:"column:ts;not null"`
}

// TableName pins the table name to match the migration; do not rely on GORM's
// pluralisation heuristics.
func (SlackMessage) TableName() string { return "slack_messages" }

// RepoMapping maps a GitHub repository to a Slack channel and a list of
// mentions to prepend to the notification message.
type RepoMapping struct {
	ID           uint     `gorm:"column:id;primaryKey;autoIncrement"`
	Repository   string   `gorm:"column:repository;uniqueIndex;not null"`
	SlackChannel string   `gorm:"column:slack_channel;not null"`
	Mentions     []string `gorm:"column:mentions;serializer:json;not null"`
}

// TableName pins the table name to match the migration.
func (RepoMapping) TableName() string { return "github_slack_mapping" }
