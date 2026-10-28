package application

import (
	"context"
	"fmt"

	"github.com/mptooling/notifycat/internal/feed/domain"
)

// SubscriptionCleaner implements domain.SubscriptionCleaner.
type SubscriptionCleaner struct {
	subscriptions domain.SubscriptionStore
	catalog       domain.RepositoryCatalog
}

var _ domain.SubscriptionCleaner = (*SubscriptionCleaner)(nil)

// NewSubscriptionCleaner builds a SubscriptionCleaner.
func NewSubscriptionCleaner(subscriptions domain.SubscriptionStore, catalog domain.RepositoryCatalog) *SubscriptionCleaner {
	return &SubscriptionCleaner{subscriptions: subscriptions, catalog: catalog}
}

// RemoveUnconfigured implements domain.SubscriptionCleaner.
func (cleaner *SubscriptionCleaner) RemoveUnconfigured(ctx context.Context) (int, error) {
	subscribed, err := cleaner.subscriptions.Repositories(ctx)
	if err != nil {
		return 0, fmt.Errorf("list subscribed repositories: %w", err)
	}
	var unconfigured []string
	for _, repository := range subscribed {
		if repository != domain.AllRepositories && !cleaner.catalog.IsConfigured(repository) {
			unconfigured = append(unconfigured, repository)
		}
	}
	if len(unconfigured) == 0 {
		return 0, nil
	}
	removed, err := cleaner.subscriptions.DeleteRepositories(ctx, unconfigured)
	if err != nil {
		return 0, fmt.Errorf("delete unconfigured subscriptions: %w", err)
	}
	return removed, nil
}
