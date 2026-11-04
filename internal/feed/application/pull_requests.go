package application

import (
	"cmp"
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
	polls         domain.PollStore
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
		polls:         params.Polls,
		catalog:       params.Catalog,
		clock:         params.Clock,
		logger:        params.Logger,
	}
}

type subscribedRepositories struct {
	solo  []string
	party []string
}

// Next implements domain.PullRequests.
func (feed *PullRequests) Next(ctx context.Context, subscriber domain.Subscriber) ([]domain.PullRequest, error) {
	now := feed.clock.Now().UTC()
	repositories, err := feed.resolveRepositories(ctx, subscriber)
	if err != nil {
		return nil, err
	}
	solo, err := feed.claimSolo(ctx, subscriber, repositories.solo, now)
	if err != nil {
		return nil, err
	}
	party, err := feed.claimParty(ctx, subscriber, repositories.party, now)
	if err != nil {
		return nil, err
	}
	if err := feed.recordPoll(ctx, subscriber, now); err != nil {
		return nil, err
	}
	feed.logDelivery(ctx, subscriber, solo, party)
	return inDeliveryOrder(solo, party.Delivered), nil
}

func (feed *PullRequests) claimSolo(ctx context.Context, subscriber domain.Subscriber, repositories []string, now time.Time) ([]domain.PullRequest, error) {
	if len(repositories) == 0 {
		return nil, nil
	}
	delivered, err := feed.deliveries.Claim(ctx, domain.Claim{
		TokenID:      subscriber.TokenID,
		Repositories: repositories,
		DeliveredAt:  now,
	})
	if err != nil {
		return nil, fmt.Errorf("claim pull requests: %w", err)
	}
	return delivered, nil
}

func (feed *PullRequests) claimParty(ctx context.Context, subscriber domain.Subscriber, repositories []string, now time.Time) (domain.PartyClaimResult, error) {
	if len(repositories) == 0 {
		return domain.PartyClaimResult{}, nil
	}
	result, err := feed.deliveries.ClaimParty(ctx, domain.PartyClaim{
		TokenID:      subscriber.TokenID,
		Repositories: repositories,
		DeliveredAt:  now,
		Day:          domain.LoadDay(now),
		ActiveSince:  now.Add(-domain.AwayAfter),
	})
	if err != nil {
		return domain.PartyClaimResult{}, fmt.Errorf("claim party pull requests: %w", err)
	}
	return result, nil
}

func (feed *PullRequests) recordPoll(ctx context.Context, subscriber domain.Subscriber, now time.Time) error {
	if err := feed.polls.Record(ctx, subscriber.TokenID, now); err != nil {
		return fmt.Errorf("record poll: %w", err)
	}
	return nil
}

func (feed *PullRequests) logDelivery(ctx context.Context, subscriber domain.Subscriber, solo []domain.PullRequest, party domain.PartyClaimResult) {
	if count := len(solo) + len(party.Delivered); count > 0 {
		feed.logger.InfoContext(ctx, "feed pull requests delivered",
			"user", subscriber.User, "token_id", subscriber.TokenID, "count", count, "party", len(party.Delivered))
	}
	if party.Taken > 0 || party.Waiting > 0 {
		feed.logger.DebugContext(ctx, "feed party pull requests held",
			"token_id", subscriber.TokenID, "taken", party.Taken, "waiting", party.Waiting)
	}
}

// Since implements domain.PullRequests.
func (feed *PullRequests) Since(ctx context.Context, subscriber domain.Subscriber, since time.Time) ([]domain.PullRequest, error) {
	repositories, err := feed.resolveRepositories(ctx, subscriber)
	if err != nil {
		return nil, err
	}
	since = since.UTC()
	var changed, received []domain.PullRequest
	if len(repositories.solo) > 0 {
		changed, err = feed.pullRequests.ChangedSince(ctx, domain.SinceQuery{
			TokenID:      subscriber.TokenID,
			Repositories: repositories.solo,
			Since:        since,
		})
		if err != nil {
			return nil, fmt.Errorf("pull requests changed since: %w", err)
		}
	}
	if len(repositories.party) > 0 {
		received, err = feed.pullRequests.ReceivedSince(ctx, domain.SinceQuery{
			TokenID:      subscriber.TokenID,
			Repositories: repositories.party,
			Since:        since,
		})
		if err != nil {
			return nil, fmt.Errorf("pull requests received since: %w", err)
		}
	}
	return inDeliveryOrder(changed, received), nil
}

func (feed *PullRequests) resolveRepositories(ctx context.Context, subscriber domain.Subscriber) (subscribedRepositories, error) {
	subscribed, err := feed.subscriptions.List(ctx, subscriber.TokenID)
	if err != nil {
		return subscribedRepositories{}, fmt.Errorf("list subscriptions: %w", err)
	}
	var repositories subscribedRepositories
	followsAll := false
	var allRepositoriesType domain.SubscriptionType
	for _, subscription := range subscribed {
		if subscription.Repository == domain.AllRepositories {
			followsAll = true
			allRepositoriesType = subscription.Type
			continue
		}
		repositories.add(subscription.Repository, subscription.Type)
	}
	if followsAll {
		stored, err := feed.pullRequests.Repositories(ctx)
		if err != nil {
			return subscribedRepositories{}, fmt.Errorf("list stored repositories: %w", err)
		}
		for _, repository := range stored {
			if !repositories.contains(repository) {
				repositories.add(repository, allRepositoriesType)
			}
		}
	}
	return subscribedRepositories{
		solo:  feed.catalog.Configured(repositories.solo),
		party: feed.catalog.Configured(repositories.party),
	}, nil
}

func (repositories *subscribedRepositories) add(repository string, subscriptionType domain.SubscriptionType) {
	if subscriptionType == domain.SubscriptionTypeParty {
		repositories.party = append(repositories.party, repository)
		return
	}
	repositories.solo = append(repositories.solo, repository)
}

func (repositories *subscribedRepositories) contains(repository string) bool {
	return slices.Contains(repositories.solo, repository) || slices.Contains(repositories.party, repository)
}

func inDeliveryOrder(first, second []domain.PullRequest) []domain.PullRequest {
	merged := make([]domain.PullRequest, 0, len(first)+len(second))
	merged = append(merged, first...)
	merged = append(merged, second...)
	slices.SortFunc(merged, func(left, right domain.PullRequest) int {
		return cmp.Or(
			left.UpdatedAt.Compare(right.UpdatedAt),
			cmp.Compare(left.Repository, right.Repository),
			cmp.Compare(left.Number, right.Number),
		)
	})
	return merged
}
