package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PullRequests persists tracked PRs and their per-channel messenger messages.
type PullRequests struct {
	db *gorm.DB
}

// NewPullRequests constructs a PullRequests repository bound to db.
func NewPullRequests(db *gorm.DB) *PullRequests {
	return &PullRequests{db: db}
}

// AddMessage records one posted message, creating the PR row on first sight.
// Insertion is idempotent on (pull_request_id, channel): re-adding the same
// channel for the same PR is a no-op, which makes the open fan-out safe to
// replay after a partial failure or GitHub redelivery.
func (r *PullRequests) AddMessage(ctx context.Context, repository string, prNumber int, channel, messageID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		pr := PullRequest{Repository: repository, PRNumber: prNumber, CreatedAt: now, UpdatedAt: now}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "gh_repository"}, {Name: "pr_number"}},
			DoNothing: true,
		}).Create(&pr).Error; err != nil {
			return fmt.Errorf("store: ensure pull request: %w", err)
		}
		if pr.ID == 0 { // conflict path: load the existing row's id
			if err := tx.Where("gh_repository = ? AND pr_number = ?", repository, prNumber).
				First(&pr).Error; err != nil {
				return fmt.Errorf("store: load pull request: %w", err)
			}
		}
		msg := Message{PullRequestID: pr.ID, Channel: channel, MessageID: messageID}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "pull_request_id"}, {Name: "channel"}},
			DoNothing: true,
		}).Create(&msg).Error; err != nil {
			return fmt.Errorf("store: add message: %w", err)
		}
		return nil
	})
}

// Messages returns the PR's messages, or ErrNotFound when the PR is unknown.
func (r *PullRequests) Messages(ctx context.Context, repository string, prNumber int) ([]Message, error) {
	var pr PullRequest
	err := r.db.WithContext(ctx).Preload("Messages").
		Where("gh_repository = ? AND pr_number = ?", repository, prNumber).
		First(&pr).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get messages: %w", err)
	}
	return pr.Messages, nil
}

// Touch bumps updated_at, recording activity. Missing PR is a no-op.
func (r *PullRequests) Touch(ctx context.Context, repository string, prNumber int) error {
	res := r.db.WithContext(ctx).Model(&PullRequest{}).
		Where("gh_repository = ? AND pr_number = ?", repository, prNumber).
		UpdateColumn("updated_at", time.Now())
	if res.Error != nil {
		return fmt.Errorf("store: touch pull request: %w", res.Error)
	}
	return nil
}

// MarkClosed sets closed_at. Missing PR is a no-op.
func (r *PullRequests) MarkClosed(ctx context.Context, repository string, prNumber int) error {
	res := r.db.WithContext(ctx).Model(&PullRequest{}).
		Where("gh_repository = ? AND pr_number = ?", repository, prNumber).
		UpdateColumn("closed_at", time.Now())
	if res.Error != nil {
		return fmt.Errorf("store: mark pull request closed: %w", res.Error)
	}
	return nil
}

// Delete removes the PR and (by cascade) its messages. Missing PR is a no-op.
func (r *PullRequests) Delete(ctx context.Context, repository string, prNumber int) error {
	err := r.db.WithContext(ctx).
		Where("gh_repository = ? AND pr_number = ?", repository, prNumber).
		Delete(&PullRequest{}).Error
	if err != nil {
		return fmt.Errorf("store: delete pull request: %w", err)
	}
	return nil
}

// FindStuck returns open PRs idle since before cutoff, oldest first. Messages
// are deliberately not preloaded: the digest routes from config, not from where
// a PR's message happens to live.
func (r *PullRequests) FindStuck(ctx context.Context, cutoff time.Time) ([]PullRequest, error) {
	var rows []PullRequest
	err := r.db.WithContext(ctx).
		Where("closed_at IS NULL AND updated_at < ?", cutoff).
		Order("updated_at asc").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("store: find stuck pull requests: %w", err)
	}
	return rows, nil
}

// ListOpen returns every not-yet-closed PR, ordered for stable output.
func (r *PullRequests) ListOpen(ctx context.Context) ([]PullRequest, error) {
	var rows []PullRequest
	err := r.db.WithContext(ctx).
		Where("closed_at IS NULL").
		Order("gh_repository asc, pr_number asc").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("store: list open pull requests: %w", err)
	}
	return rows, nil
}

// ListOpenWithMessages returns every not-yet-closed PR with its messages
// preloaded, ordered for stable output. It backs the relocate tooling, which
// needs to see where each open PR's messages currently live.
func (r *PullRequests) ListOpenWithMessages(ctx context.Context) ([]PullRequest, error) {
	var rows []PullRequest
	err := r.db.WithContext(ctx).Preload("Messages").
		Where("closed_at IS NULL").
		Order("gh_repository asc, pr_number asc").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("store: list open pull requests with messages: %w", err)
	}
	return rows, nil
}

// MoveMessage retargets the PR's message row in fromChannel at toChannel and
// the newly posted messageID. Retargeting the existing row rather than deleting
// and re-inserting keeps the (pull_request_id, channel) uniqueness a single
// statement. ErrNotFound when the PR has no message in fromChannel.
func (r *PullRequests) MoveMessage(ctx context.Context, repository string, prNumber int, fromChannel, toChannel, messageID string) error {
	res := r.messageRows(ctx, repository, prNumber, fromChannel).
		UpdateColumns(map[string]any{"channel": toChannel, "message_id": messageID})
	if res.Error != nil {
		return fmt.Errorf("store: move message: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// RemoveMessage drops the PR's message row for channel, leaving the Slack
// message itself alone. ErrNotFound when there is no such row.
func (r *PullRequests) RemoveMessage(ctx context.Context, repository string, prNumber int, channel string) error {
	res := r.messageRows(ctx, repository, prNumber, channel).Delete(&Message{})
	if res.Error != nil {
		return fmt.Errorf("store: remove message: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// messageRows scopes a query to one PR's message row in a single channel.
func (r *PullRequests) messageRows(ctx context.Context, repository string, prNumber int, channel string) *gorm.DB {
	prID := r.db.Model(&PullRequest{}).Select("id").
		Where("gh_repository = ? AND pr_number = ?", repository, prNumber)
	return r.db.WithContext(ctx).Model(&Message{}).
		Where("channel = ? AND pull_request_id = (?)", channel, prID)
}

// DeleteStaleBefore removes PRs idle since before cutoff (messages cascade).
func (r *PullRequests) DeleteStaleBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res := r.db.WithContext(ctx).
		Where("updated_at < ?", cutoff).
		Delete(&PullRequest{})
	if res.Error != nil {
		return 0, fmt.Errorf("store: delete stale pull requests: %w", res.Error)
	}
	return res.RowsAffected, nil
}
