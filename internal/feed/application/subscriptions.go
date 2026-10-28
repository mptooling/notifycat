package application

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/mptooling/notifycat/internal/feed/domain"
)

// Subscriptions implements domain.Subscriptions.
type Subscriptions struct {
	store   domain.SubscriptionStore
	catalog domain.RepositoryCatalog
	clock   domain.Clock
	logger  *slog.Logger
}

var _ domain.Subscriptions = (*Subscriptions)(nil)

// NewSubscriptions builds Subscriptions.
func NewSubscriptions(params domain.SubscriptionsParams) *Subscriptions {
	return &Subscriptions{store: params.Subscriptions, catalog: params.Catalog, clock: params.Clock, logger: params.Logger}
}

// List implements domain.Subscriptions.
func (subscriptions *Subscriptions) List(ctx context.Context, subscriber domain.Subscriber) ([]domain.Subscription, error) {
	listed, err := subscriptions.store.List(ctx, subscriber.TokenID)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}
	return listed, nil
}

// Add implements domain.Subscriptions.
func (subscriptions *Subscriptions) Add(ctx context.Context, subscriber domain.Subscriber, repositories []string) ([]domain.Subscription, error) {
	requested := normalizeRepositories(repositories)
	if len(requested) == 0 {
		return nil, domain.ErrNoRepositories
	}
	if unknown := subscriptions.unknownRepositories(requested); len(unknown) > 0 {
		return nil, &domain.UnknownRepositoriesError{Repositories: unknown}
	}
	existing, err := subscriptions.store.List(ctx, subscriber.TokenID)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}
	newRepositories := repositoriesNotSubscribed(requested, existing)
	if len(existing)+len(newRepositories) > domain.MaxSubscriptionsPerToken {
		return nil, domain.ErrTooManySubscriptions
	}
	if len(newRepositories) > 0 {
		added, err := subscriptions.store.Add(ctx, domain.NewSubscriptions{
			TokenID:      subscriber.TokenID,
			Repositories: newRepositories,
			CreatedAt:    subscriptions.clock.Now().UTC(),
		})
		if err != nil {
			return nil, fmt.Errorf("add subscriptions: %w", err)
		}
		if added > 0 {
			subscriptions.logger.InfoContext(ctx, "feed subscriptions added", "user", subscriber.User, "token_id", subscriber.TokenID, "count", added)
		}
	}
	return subscriptions.List(ctx, subscriber)
}

// Remove implements domain.Subscriptions.
func (subscriptions *Subscriptions) Remove(ctx context.Context, subscriber domain.Subscriber, id int64) error {
	removed, err := subscriptions.store.Delete(ctx, subscriber.TokenID, id)
	if err != nil {
		return fmt.Errorf("delete subscription: %w", err)
	}
	subscriptions.logger.InfoContext(ctx, "feed subscription removed", "user", subscriber.User, "token_id", subscriber.TokenID, "repository", removed.Repository)
	return nil
}

func (subscriptions *Subscriptions) unknownRepositories(repositories []string) []string {
	var unknown []string
	for _, repository := range repositories {
		if repository != domain.AllRepositories && !subscriptions.catalog.IsConfigured(repository) {
			unknown = append(unknown, repository)
		}
	}
	slices.Sort(unknown)
	return unknown
}

func normalizeRepositories(repositories []string) []string {
	var normalized []string
	for _, repository := range repositories {
		repository = strings.ToLower(strings.TrimSpace(repository))
		if repository != "" && !slices.Contains(normalized, repository) {
			normalized = append(normalized, repository)
		}
	}
	return normalized
}

func repositoriesNotSubscribed(requested []string, existing []domain.Subscription) []string {
	var missing []string
	for _, repository := range requested {
		subscribed := slices.ContainsFunc(existing, func(subscription domain.Subscription) bool {
			return subscription.Repository == repository
		})
		if !subscribed {
			missing = append(missing, repository)
		}
	}
	return missing
}
