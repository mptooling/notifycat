package infrastructure

import (
	"context"
	"time"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/domain"
	"github.com/mptooling/notifycat/internal/platform/persistence"
)

// ScheduledQueueRepo adapts the store's ScheduledNotifications repository to
// the notification ScheduledQueue port.
type ScheduledQueueRepo struct {
	scheduled *persistence.ScheduledNotifications
}

// NewScheduledQueueRepo wraps the store's ScheduledNotifications repository.
func NewScheduledQueueRepo(scheduled *persistence.ScheduledNotifications) *ScheduledQueueRepo {
	return &ScheduledQueueRepo{scheduled: scheduled}
}

// Enqueue implements domain.ScheduledQueue.
func (r *ScheduledQueueRepo) Enqueue(ctx context.Context, entry domain.ScheduledEntry) error {
	return r.scheduled.Enqueue(ctx, persistence.ScheduledItem{
		Repository: entry.Repository,
		PRNumber:   entry.PR.Number,
		Channel:    entry.Channel,
		Mentions:   entry.Mentions,
		Title:      entry.PR.Title,
		URL:        entry.PR.URL,
		Author:     entry.PR.Author,
	})
}

// Pending implements domain.ScheduledQueue.
func (r *ScheduledQueueRepo) Pending(ctx context.Context) ([]domain.ScheduledEntry, error) {
	items, err := r.scheduled.Pending(ctx)
	if err != nil {
		return nil, err
	}
	entries := make([]domain.ScheduledEntry, 0, len(items))
	for _, item := range items {
		entries = append(entries, domain.ScheduledEntry{
			ID:         item.ID,
			Repository: item.Repository,
			PR:         kernel.PR{Number: item.PRNumber, Title: item.Title, URL: item.URL, Author: item.Author},
			Channel:    item.Channel,
			Mentions:   item.Mentions,
			QueuedAt:   item.QueuedAt,
		})
	}
	return entries, nil
}

// MarkSent implements domain.ScheduledQueue.
func (r *ScheduledQueueRepo) MarkSent(ctx context.Context, ids []uint, sentAt time.Time) error {
	return r.scheduled.MarkSent(ctx, ids, sentAt)
}

var _ domain.ScheduledQueue = (*ScheduledQueueRepo)(nil)
