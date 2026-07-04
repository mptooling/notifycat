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

// FindStuck returns open PRs idle since before cutoff, messages preloaded,
// oldest first.
func (r *PullRequests) FindStuck(ctx context.Context, cutoff time.Time) ([]PullRequest, error) {
	var rows []PullRequest
	err := r.db.WithContext(ctx).Preload("Messages").
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
