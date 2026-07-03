package infrastructure

import (
	"context"
	"time"

	"github.com/mptooling/notifycat/internal/store"
)

// PRRepository adapts the store's PullRequests repository to the maintenance
// domain ports. It maps store persistence models to maintenance domain DTOs at
// the boundary, so no gorm-tagged type ever crosses a port. The reconcile ports
// (OpenLister/Closer/Deleter) are added alongside StaleMessageDeleter.
type PRRepository struct {
	pullRequests *store.PullRequests
}

// NewPRRepository wraps the store's PullRequests repository.
func NewPRRepository(pullRequests *store.PullRequests) *PRRepository {
	return &PRRepository{pullRequests: pullRequests}
}

// DeleteStaleBefore implements domain.StaleMessageDeleter.
func (r *PRRepository) DeleteStaleBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	return r.pullRequests.DeleteStaleBefore(ctx, cutoff)
}
