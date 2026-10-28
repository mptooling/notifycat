package application

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/mptooling/notifycat/internal/feed/domain"
)

// PullRequests implements domain.PullRequests.
type PullRequests struct {
	subscriptions domain.SubscriptionStore
	pullRequests  domain.PullRequestStore
	deliveries    domain.DeliveryStore
	catalog       domain.RepositoryCatalog
	clock         domain.Clock
	logger        *slog.Logger
}

var _ domain.PullRequests = (*PullRequests)(nil)

// NewPullRequests builds PullRequests.
func NewPullRequests(params domain.PullRequestsParams) *PullRequests {
	return &PullRequests{
		subscriptions: params.Subscriptions,
		pullRequests:  params.PullRequests,
		deliveries:    params.Deliveries,
		catalog:       params.Catalog,
		clock:         params.Clock,
		logger:        params.Logger,
	}
}

// Next implements domain.PullRequests.
func (feed *PullRequests) Next(ctx context.Context, subscriber domain.Subscriber) ([]domain.PullRequest, error) {
	repositories, err := feed.resolveRepositories(ctx, subscriber)
	if err != nil {
		return nil, err
	}
	if len(repositories) == 0 {
		return []domain.PullRequest{}, nil
	}
	delivered, err := feed.deliveries.Claim(ctx, domain.Claim{
		TokenID:      subscriber.TokenID,
		Repositories: repositories,
		DeliveredAt:  feed.clock.Now().UTC(),
	})
	if err != nil {
		return nil, fmt.Errorf("claim pull requests: %w", err)
	}
	if len(delivered) > 0 {
		feed.logger.InfoContext(ctx, "feed pull requests delivered", "user", subscriber.User, "token_id", subscriber.TokenID, "count", len(delivered))
	}
	return delivered, nil
}

// Since implements domain.PullRequests.
func (feed *PullRequests) Since(ctx context.Context, subscriber domain.Subscriber, since time.Time) ([]domain.PullRequest, error) {
	repositories, err := feed.resolveRepositories(ctx, subscriber)
	if err != nil {
		return nil, err
	}
	if len(repositories) == 0 {
		return []domain.PullRequest{}, nil
	}
	changed, err := feed.pullRequests.ChangedSince(ctx, repositories, since.UTC())
	if err != nil {
		return nil, fmt.Errorf("pull requests changed since: %w", err)
	}
	return changed, nil
}

func (feed *PullRequests) resolveRepositories(ctx context.Context, subscriber domain.Subscriber) ([]string, error) {
	subscribed, err := feed.subscriptions.List(ctx, subscriber.TokenID)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}
	var repositories []string
	followsAll := false
	for _, subscription := range subscribed {
		if subscription.Repository == domain.AllRepositories {
			followsAll = true
			continue
		}
		repositories = append(repositories, subscription.Repository)
	}
	if followsAll {
		stored, err := feed.pullRequests.Repositories(ctx)
		if err != nil {
			return nil, fmt.Errorf("list stored repositories: %w", err)
		}
		for _, repository := range stored {
			if !slices.Contains(repositories, repository) {
				repositories = append(repositories, repository)
			}
		}
	}
	return feed.catalog.Configured(repositories), nil
}
