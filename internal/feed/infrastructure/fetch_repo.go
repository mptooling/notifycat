package infrastructure

import (
	"context"
	"fmt"
	"time"

	"github.com/mptooling/notifycat/internal/feed/domain"
	"github.com/mptooling/notifycat/internal/platform/persistence"
)

// FetchRepo adapts persistence.FeedFetches to domain.FetchStore.
type FetchRepo struct {
	fetches *persistence.FeedFetches
}

var _ domain.FetchStore = (*FetchRepo)(nil)

// NewFetchRepo wraps the fetches repository.
func NewFetchRepo(fetches *persistence.FeedFetches) *FetchRepo {
	return &FetchRepo{fetches: fetches}
}

// Record implements domain.FetchStore.
func (repo *FetchRepo) Record(ctx context.Context, tokenID string, fetchedAt time.Time) error {
	err := repo.fetches.Record(ctx, persistence.FeedFetchRecord{
		TokenID:       tokenID,
		FetchedAt:     fetchedAt,
		ReplaceBefore: fetchedAt.Add(-domain.FetchResolution),
	})
	if err != nil {
		return fmt.Errorf("feed: record fetch: %w", err)
	}
	return nil
}
