package store

import (
	"context"
	"errors"
	"fmt"

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

// Save upserts the message by composite key (pr_number, gh_repository).
func (r *SlackMessages) Save(ctx context.Context, m SlackMessage) error {
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "pr_number"}, {Name: "gh_repository"}},
		DoUpdates: clause.AssignmentColumns([]string{"ts"}),
	}).Create(&m).Error
	if err != nil {
		return fmt.Errorf("store: save slack message: %w", err)
	}
	return nil
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
