package persistence

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ScheduledItem is a scheduled notification with its PR's natural key.
type ScheduledItem struct {
	ID         uint
	Repository string
	PRNumber   int
	Channel    string
	Mentions   []string
	Title      string
	URL        string
	Author     string
	QueuedAt   time.Time
}

// ScheduledNotifications persists the queue of the scheduled notification
// strategy. Callers name a PR by its natural key; the pull_request_id is
// resolved internally.
type ScheduledNotifications struct {
	db *gorm.DB
}

// NewScheduledNotifications constructs a ScheduledNotifications repository bound to db.
func NewScheduledNotifications(db *gorm.DB) *ScheduledNotifications {
	return &ScheduledNotifications{db: db}
}

// scheduledRow is a queue row joined with its PR's natural key.
type scheduledRow struct {
	ScheduledNotification
	Repository string `gorm:"column:gh_repository"`
	PRNumber   int    `gorm:"column:pr_number"`
}

// Enqueue queues the item, creating the PR row on first sight so the close and
// draft handlers find it. It is idempotent on (PR, channel), also after the
// row was sent: a webhook redelivery never announces a PR twice in a channel.
func (r *ScheduledNotifications) Enqueue(ctx context.Context, item ScheduledItem) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		pr, err := ensurePullRequest(tx, item.Repository, item.PRNumber)
		if err != nil {
			return err
		}
		mentions := item.Mentions
		if mentions == nil {
			mentions = []string{}
		}
		notification := ScheduledNotification{
			PullRequestID: pr.ID,
			Channel:       item.Channel,
			Mentions:      mentions,
			Title:         item.Title,
			URL:           item.URL,
			Author:        item.Author,
			QueuedAt:      time.Now(),
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "pull_request_id"}, {Name: "channel"}},
			DoNothing: true,
		}).Create(&notification).Error; err != nil {
			return fmt.Errorf("store: enqueue scheduled notification: %w", err)
		}
		return nil
	})
}

// Pending returns the unsent items of open PRs, oldest first.
func (r *ScheduledNotifications) Pending(ctx context.Context) ([]ScheduledItem, error) {
	var rows []scheduledRow
	err := r.joined(ctx).
		Where("pull_requests.closed_at IS NULL AND scheduled_notifications.sent_at IS NULL").
		Order("scheduled_notifications.queued_at asc, scheduled_notifications.id asc").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("store: list pending scheduled notifications: %w", err)
	}
	return toScheduledItems(rows), nil
}

// MarkSent stamps the items as sent at sentAt.
func (r *ScheduledNotifications) MarkSent(ctx context.Context, ids []uint, sentAt time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	err := r.db.WithContext(ctx).Model(&ScheduledNotification{}).
		Where("id IN ?", ids).
		UpdateColumn("sent_at", sentAt).Error
	if err != nil {
		return fmt.Errorf("store: mark scheduled notifications sent: %w", err)
	}
	return nil
}

// AnnouncedSince returns the items still waiting for a send or sent at or
// after cutoff.
func (r *ScheduledNotifications) AnnouncedSince(ctx context.Context, cutoff time.Time) ([]ScheduledItem, error) {
	var rows []scheduledRow
	err := r.joined(ctx).
		Where("scheduled_notifications.sent_at IS NULL OR scheduled_notifications.sent_at >= ?", cutoff).
		Order("scheduled_notifications.id asc").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("store: list announced scheduled notifications: %w", err)
	}
	return toScheduledItems(rows), nil
}

func (r *ScheduledNotifications) joined(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).Model(&ScheduledNotification{}).
		Select("scheduled_notifications.*, pull_requests.gh_repository, pull_requests.pr_number").
		Joins("JOIN pull_requests ON pull_requests.id = scheduled_notifications.pull_request_id")
}

func toScheduledItems(rows []scheduledRow) []ScheduledItem {
	items := make([]ScheduledItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, ScheduledItem{
			ID:         row.ID,
			Repository: row.Repository,
			PRNumber:   row.PRNumber,
			Channel:    row.Channel,
			Mentions:   row.Mentions,
			Title:      row.Title,
			URL:        row.URL,
			Author:     row.Author,
			QueuedAt:   row.QueuedAt,
		})
	}
	return items
}
