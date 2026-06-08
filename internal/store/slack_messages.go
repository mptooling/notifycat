package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SlackMessages persists the Slack message TS associated with each PR.
type SlackMessages struct {
	db *gorm.DB
}

// NewSlackMessages constructs a SlackMessages repository bound to db.
func NewSlackMessages(db *gorm.DB) *SlackMessages {
	return &SlackMessages{db: db}
}

// Save upserts the message by composite key (pr_number, gh_repository). The
// `updated_at` column is bumped on every Save (both insert and update paths)
// to drive stale-row cleanup.
func (r *SlackMessages) Save(ctx context.Context, m SlackMessage) error {
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "pr_number"}, {Name: "gh_repository"}},
		DoUpdates: clause.AssignmentColumns([]string{"ts", "updated_at"}),
	}).Create(&m).Error
	if err != nil {
		return fmt.Errorf("store: save slack message: %w", err)
	}
	return nil
}

// Touch bumps updated_at to the current time for the given PR, recording
// review/comment activity so the stuck-PR digest can tell idle PRs from active
// ones. A missing row is a no-op (idempotent).
func (r *SlackMessages) Touch(ctx context.Context, repository string, prNumber int) error {
	res := r.db.WithContext(ctx).
		Model(&SlackMessage{}).
		Where("pr_number = ? AND gh_repository = ?", prNumber, repository).
		UpdateColumn("updated_at", time.Now())
	if res.Error != nil {
		return fmt.Errorf("store: touch slack message: %w", res.Error)
	}
	return nil
}

// MarkClosed records that the PR has been merged/closed so the stuck-PR digest
// skips it. A missing row is a no-op (idempotent).
func (r *SlackMessages) MarkClosed(ctx context.Context, repository string, prNumber int) error {
	res := r.db.WithContext(ctx).
		Model(&SlackMessage{}).
		Where("pr_number = ? AND gh_repository = ?", prNumber, repository).
		UpdateColumn("closed_at", time.Now())
	if res.Error != nil {
		return fmt.Errorf("store: mark slack message closed: %w", res.Error)
	}
	return nil
}

// FindStuck returns every open (closed_at IS NULL) message whose updated_at is
// strictly older than cutoff — the PRs that saw no activity since cutoff.
// Results are ordered oldest-first for stable digest output. An empty result
// is (nil, nil).
func (r *SlackMessages) FindStuck(ctx context.Context, cutoff time.Time) ([]SlackMessage, error) {
	var rows []SlackMessage
	err := r.db.WithContext(ctx).
		Where("closed_at IS NULL AND updated_at < ?", cutoff).
		Order("updated_at asc").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("store: find stuck slack messages: %w", err)
	}
	return rows, nil
}

// DeleteStaleBefore removes every row whose updated_at is strictly older than
// cutoff. Returns the number of rows deleted. An empty table returns (0, nil).
func (r *SlackMessages) DeleteStaleBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res := r.db.WithContext(ctx).
		Where("updated_at < ?", cutoff).
		Delete(&SlackMessage{})
	if res.Error != nil {
		return 0, fmt.Errorf("store: delete stale slack messages: %w", res.Error)
	}
	return res.RowsAffected, nil
}

// Get returns the message for the given (repository, prNumber), or ErrNotFound.
func (r *SlackMessages) Get(ctx context.Context, repository string, prNumber int) (SlackMessage, error) {
	var m SlackMessage
	err := r.db.WithContext(ctx).
		Where("pr_number = ? AND gh_repository = ?", prNumber, repository).
		First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return SlackMessage{}, ErrNotFound
	}
	if err != nil {
		return SlackMessage{}, fmt.Errorf("store: get slack message: %w", err)
	}
	return m, nil
}

// Delete removes the message; missing rows are a no-op (idempotent).
func (r *SlackMessages) Delete(ctx context.Context, repository string, prNumber int) error {
	err := r.db.WithContext(ctx).
		Where("pr_number = ? AND gh_repository = ?", prNumber, repository).
		Delete(&SlackMessage{}).Error
	if err != nil {
		return fmt.Errorf("store: delete slack message: %w", err)
	}
	return nil
}
