package infrastructure

import (
	"context"
	"errors"
	"fmt"

	"github.com/mptooling/notifycat/internal/feed/domain"
	"github.com/mptooling/notifycat/internal/platform/persistence"
)

// SubscriptionRepo adapts persistence.FeedSubscriptions to domain.SubscriptionStore.
type SubscriptionRepo struct {
	subscriptions *persistence.FeedSubscriptions
}

var _ domain.SubscriptionStore = (*SubscriptionRepo)(nil)

// NewSubscriptionRepo wraps the subscriptions repository.
func NewSubscriptionRepo(subscriptions *persistence.FeedSubscriptions) *SubscriptionRepo {
	return &SubscriptionRepo{subscriptions: subscriptions}
}

// List implements domain.SubscriptionStore.
func (repo *SubscriptionRepo) List(ctx context.Context, tokenID string) ([]domain.Subscription, error) {
	rows, err := repo.subscriptions.List(ctx, tokenID)
	if err != nil {
		return nil, fmt.Errorf("feed: list subscriptions: %w", err)
	}
	subscriptions := make([]domain.Subscription, 0, len(rows))
	for _, row := range rows {
		subscriptions = append(subscriptions, subscriptionFromRow(row))
	}
	return subscriptions, nil
}

// Add implements domain.SubscriptionStore.
func (repo *SubscriptionRepo) Add(ctx context.Context, newSubscriptions domain.NewSubscriptions) (int, error) {
	added, err := repo.subscriptions.Add(ctx, persistence.FeedSubscriptionBatch{
		TokenID:      newSubscriptions.TokenID,
		Repositories: newSubscriptions.Repositories,
		CreatedAt:    newSubscriptions.CreatedAt,
	})
	if err != nil {
		return 0, fmt.Errorf("feed: add subscriptions: %w", err)
	}
	return added, nil
}

// Delete implements domain.SubscriptionStore.
func (repo *SubscriptionRepo) Delete(ctx context.Context, tokenID string, id int64) (domain.Subscription, error) {
	row, err := repo.subscriptions.Delete(ctx, tokenID, id)
	if errors.Is(err, persistence.ErrNotFound) {
		return domain.Subscription{}, domain.ErrSubscriptionNotFound
	}
	if err != nil {
		return domain.Subscription{}, fmt.Errorf("feed: delete subscription: %w", err)
	}
	return subscriptionFromRow(row), nil
}

func subscriptionFromRow(row persistence.FeedSubscription) domain.Subscription {
	return domain.Subscription{
		ID:         row.ID,
		TokenID:    row.TokenID,
		Repository: row.Repository,
		CreatedAt:  row.CreatedAt,
	}
}
