package infrastructure

import (
	"context"
	"time"

	"github.com/mptooling/notifycat/internal/maintenance/domain"
	"github.com/mptooling/notifycat/internal/store"
)

// PRRepository adapts the store's PullRequests repository to the maintenance
// domain ports (StaleMessageDeleter, OpenLister, Closer, Deleter). It maps
// store persistence models to maintenance domain DTOs at the boundary, so no
// gorm-tagged type ever crosses a port.
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

// ListOpen implements domain.OpenLister, mapping store rows to domain PRRows.
func (r *PRRepository) ListOpen(ctx context.Context) ([]domain.PRRow, error) {
	rows, err := r.pullRequests.ListOpen(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.PRRow, len(rows))
	for i, row := range rows {
		out[i] = domain.PRRow{Repository: row.Repository, PRNumber: row.PRNumber}
	}
	return out, nil
}

// MarkClosed implements domain.Closer.
func (r *PRRepository) MarkClosed(ctx context.Context, repository string, prNumber int) error {
	return r.pullRequests.MarkClosed(ctx, repository, prNumber)
}

// Delete implements domain.Deleter.
func (r *PRRepository) Delete(ctx context.Context, repository string, prNumber int) error {
	return r.pullRequests.Delete(ctx, repository, prNumber)
}
