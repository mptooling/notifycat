package application

import (
	"context"
	"fmt"

	"github.com/mptooling/notifycat/internal/feed/domain"
)

// PullRequestCleaner implements domain.PullRequestCleaner.
type PullRequestCleaner struct {
	pullRequests domain.PullRequestStore
	catalog      domain.RepositoryCatalog
}

var _ domain.PullRequestCleaner = (*PullRequestCleaner)(nil)

// NewPullRequestCleaner builds a PullRequestCleaner.
func NewPullRequestCleaner(pullRequests domain.PullRequestStore, catalog domain.RepositoryCatalog) *PullRequestCleaner {
	return &PullRequestCleaner{pullRequests: pullRequests, catalog: catalog}
}

// RemoveAll implements domain.PullRequestCleaner.
func (cleaner *PullRequestCleaner) RemoveAll(ctx context.Context) (int, error) {
	removed, err := cleaner.pullRequests.DeleteAll(ctx)
	if err != nil {
		return 0, fmt.Errorf("delete all pull requests: %w", err)
	}
	return removed, nil
}

// RemoveUnconfigured implements domain.PullRequestCleaner.
func (cleaner *PullRequestCleaner) RemoveUnconfigured(ctx context.Context) (int, error) {
	stored, err := cleaner.pullRequests.Repositories(ctx)
	if err != nil {
		return 0, fmt.Errorf("list stored repositories: %w", err)
	}
	var unconfigured []string
	for _, repository := range stored {
		if !cleaner.catalog.IsConfigured(repository) {
			unconfigured = append(unconfigured, repository)
		}
	}
	if len(unconfigured) == 0 {
		return 0, nil
	}
	removed, err := cleaner.pullRequests.DeleteRepositories(ctx, unconfigured)
	if err != nil {
		return 0, fmt.Errorf("delete unconfigured pull requests: %w", err)
	}
	return removed, nil
}
