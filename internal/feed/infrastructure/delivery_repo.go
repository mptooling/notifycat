package infrastructure

import (
	"context"
	"fmt"

	"github.com/mptooling/notifycat/internal/feed/domain"
	"github.com/mptooling/notifycat/internal/platform/persistence"
)

// DeliveryRepo adapts persistence.FeedDeliveries to domain.DeliveryStore.
type DeliveryRepo struct {
	deliveries *persistence.FeedDeliveries
}

var _ domain.DeliveryStore = (*DeliveryRepo)(nil)

// NewDeliveryRepo wraps the deliveries repository.
func NewDeliveryRepo(deliveries *persistence.FeedDeliveries) *DeliveryRepo {
	return &DeliveryRepo{deliveries: deliveries}
}

// Claim implements domain.DeliveryStore.
func (repo *DeliveryRepo) Claim(ctx context.Context, claim domain.Claim) ([]domain.PullRequest, error) {
	rows, err := repo.deliveries.Claim(ctx, persistence.FeedClaim{
		TokenID:      claim.TokenID,
		Repositories: claim.Repositories,
		DeliveredAt:  claim.DeliveredAt,
	})
	if err != nil {
		return nil, fmt.Errorf("feed: claim pull requests: %w", err)
	}
	return pullRequestsFromRows(rows), nil
}
