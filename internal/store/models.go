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

// Reactions is the resolved per-repo reaction-emoji set (Slack emoji names
// without colons). Enabled gates whether close/review reactions are added at
// all. Empty BotReview disables the bot-reviewer marker.
type Reactions struct {
	Enabled       bool
	NewPR         string
	MergedPR      string
	ClosedPR      string
	Approved      string
	Commented     string
	RequestChange string
	BotReview     string
}

// PullRequest is one tracked PR. (Repository, PRNumber) is the natural key;
// CreatedAt is kept for later statistics, UpdatedAt is the activity clock
// (bumped on open and every review/comment) driving digest idle-detection and
// cleanup, and ClosedAt (nil = open) marks merged/closed so the digest skips it.
type PullRequest struct {
	ID         uint       `gorm:"primaryKey"`
	Repository string     `gorm:"column:gh_repository;not null"`
	PRNumber   int        `gorm:"column:pr_number;not null"`
	CreatedAt  time.Time  `gorm:"column:created_at;not null"`
	UpdatedAt  time.Time  `gorm:"column:updated_at;not null"`
	ClosedAt   *time.Time `gorm:"column:closed_at"`
	Messages   []Message  `gorm:"foreignKey:PullRequestID;constraint:OnDelete:CASCADE"`
}

// TableName pins the table name; do not rely on GORM pluralization.
func (PullRequest) TableName() string { return "pull_requests" }

// Message is one posted messenger message for a PR. (PullRequestID, Channel) is
// unique — at most one message per channel per PR. Channel is a room in the
// messenger; MessageID is the messenger's id for the post (Slack's ts).
type Message struct {
	ID            uint   `gorm:"primaryKey"`
	PullRequestID uint   `gorm:"column:pull_request_id;not null"`
	Channel       string `gorm:"column:channel;not null"`
	MessageID     string `gorm:"column:message_id;not null"`
}

// TableName pins the table name.
func (Message) TableName() string { return "messages" }

// CodeReview is one review "session" for a PR: who started reviewing it and
// when, with FinishedAt nil while the review is in progress and set once the
// reviewer's GitHub review lands. It hangs off a PullRequest (cascade-deleted
// with it) and a partial unique index enforces at most one active (FinishedAt
// IS NULL) review per PR — finished rows accumulate as history.
type CodeReview struct {
	ID            uint       `gorm:"primaryKey"`
	PullRequestID uint       `gorm:"column:pull_request_id;not null"`
	SlackUserID   string     `gorm:"column:slack_user_id;not null"`
	SlackUserName string     `gorm:"column:slack_user_name"`
	StartedAt     time.Time  `gorm:"column:started_at;not null"`
	FinishedAt    *time.Time `gorm:"column:finished_at"`
}

// TableName pins the table name.
func (CodeReview) TableName() string { return "code_reviews" }

// Target is one fan-out destination resolved for a PR: a channel and the
// mentions to ping there. Produced by the mappings resolver, consumed by the
// open handler.
type Target struct {
	Channel  string
	Mentions []string
}

// RepoMapping is the value object handlers and validators consume — a GitHub
// repository routed to a Slack channel with an optional mentions list, and
// resolved behavioral config (global defaults merged with org/* and org/repo
// overrides). The source of truth for routing lives in config.yaml's mappings:
// section (loaded by internal/config / internal/mappings); the type stays here
// so consumers don't have to know who produced it.
type RepoMapping struct {
	Repository   string
	SlackChannel string
	Mentions     []string
	// Resolved per-repo behavioral config (global config.yaml defaults merged
	// with org/* and org/repo overrides). Formatting-only — not part of
	// validation or the lock.
	Reactions        Reactions
	IgnoreAIReviews  bool
	DependabotFormat bool
}
