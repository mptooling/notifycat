package infrastructure

import (
	"context"
	"time"

	"github.com/mptooling/notifycat/internal/digest/domain"
	"github.com/mptooling/notifycat/internal/platform/persistence"
)

// ScheduledAnnouncementsRepo adapts the store's ScheduledNotifications
// repository to the digest's ScheduledAnnouncements port.
type ScheduledAnnouncementsRepo struct {
	scheduled *persistence.ScheduledNotifications
}

// NewScheduledAnnouncementsRepo wraps the store's ScheduledNotifications repository.
func NewScheduledAnnouncementsRepo(scheduled *persistence.ScheduledNotifications) *ScheduledAnnouncementsRepo {
	return &ScheduledAnnouncementsRepo{scheduled: scheduled}
}

// AnnouncedSince implements domain.ScheduledAnnouncements.
func (r *ScheduledAnnouncementsRepo) AnnouncedSince(ctx context.Context, cutoff time.Time) ([]domain.AnnouncedPR, error) {
	items, err := r.scheduled.AnnouncedSince(ctx, cutoff)
	if err != nil {
		return nil, err
	}
	announced := make([]domain.AnnouncedPR, 0, len(items))
	for _, item := range items {
		announced = append(announced, domain.AnnouncedPR{
			Repository: item.Repository,
			PRNumber:   item.PRNumber,
			Channel:    item.Channel,
		})
	}
	return announced, nil
}

var _ domain.ScheduledAnnouncements = (*ScheduledAnnouncementsRepo)(nil)
