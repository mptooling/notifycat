package infrastructure

import (
	"context"
	"fmt"
	"time"

	"github.com/mptooling/notifycat/internal/feed/domain"
	"github.com/mptooling/notifycat/internal/platform/persistence"
)

// PullRequestRepo adapts persistence.FeedPullRequests to domain.PullRequestStore.
type PullRequestRepo struct {
	pullRequests *persistence.FeedPullRequests
}

var _ domain.PullRequestStore = (*PullRequestRepo)(nil)

// NewPullRequestRepo wraps the pull-requests repository.
func NewPullRequestRepo(pullRequests *persistence.FeedPullRequests) *PullRequestRepo {
	return &PullRequestRepo{pullRequests: pullRequests}
}

// Upsert implements domain.PullRequestStore.
func (repo *PullRequestRepo) Upsert(ctx context.Context, pullRequest domain.PullRequest) error {
	if err := repo.pullRequests.Upsert(ctx, pullRequestToRow(pullRequest)); err != nil {
		return fmt.Errorf("feed: upsert pull request: %w", err)
	}
	return nil
}

// Delete implements domain.PullRequestStore.
func (repo *PullRequestRepo) Delete(ctx context.Context, repository string, number int) error {
	if err := repo.pullRequests.Delete(ctx, repository, number); err != nil {
		return fmt.Errorf("feed: delete pull request: %w", err)
	}
	return nil
}

// DeleteAll implements domain.PullRequestStore.
func (repo *PullRequestRepo) DeleteAll(ctx context.Context) (int, error) {
	removed, err := repo.pullRequests.DeleteAll(ctx)
	if err != nil {
		return 0, fmt.Errorf("feed: delete all pull requests: %w", err)
	}
	return removed, nil
}

// DeleteRepositories implements domain.PullRequestStore.
func (repo *PullRequestRepo) DeleteRepositories(ctx context.Context, repositories []string) (int, error) {
	removed, err := repo.pullRequests.DeleteRepositories(ctx, repositories)
	if err != nil {
		return 0, fmt.Errorf("feed: delete repository pull requests: %w", err)
	}
	return removed, nil
}

// Repositories implements domain.PullRequestStore.
func (repo *PullRequestRepo) Repositories(ctx context.Context) ([]string, error) {
	repositories, err := repo.pullRequests.Repositories(ctx)
	if err != nil {
		return nil, fmt.Errorf("feed: list repositories: %w", err)
	}
	return repositories, nil
}

// ChangedSince implements domain.PullRequestStore.
func (repo *PullRequestRepo) ChangedSince(ctx context.Context, repositories []string, since time.Time) ([]domain.PullRequest, error) {
	rows, err := repo.pullRequests.ChangedSince(ctx, repositories, since)
	if err != nil {
		return nil, fmt.Errorf("feed: changed pull requests: %w", err)
	}
	return pullRequestsFromRows(rows), nil
}

func pullRequestToRow(pullRequest domain.PullRequest) persistence.FeedPullRequest {
	var createdAt *time.Time
	if !pullRequest.CreatedAt.IsZero() {
		createdAt = &pullRequest.CreatedAt
	}
	return persistence.FeedPullRequest{
		Repository:   pullRequest.Repository,
		Number:       pullRequest.Number,
		Title:        pullRequest.Title,
		Description:  pullRequest.Description,
		URL:          pullRequest.URL,
		AuthorID:     pullRequest.AuthorID,
		SourceBranch: pullRequest.SourceBranch,
		TargetBranch: pullRequest.TargetBranch,
		HeadSHA:      pullRequest.HeadSHA,
		CreatedAt:    createdAt,
		UpdatedAt:    pullRequest.UpdatedAt,
	}
}

func pullRequestsFromRows(rows []persistence.FeedPullRequest) []domain.PullRequest {
	pullRequests := make([]domain.PullRequest, 0, len(rows))
	for _, row := range rows {
		var createdAt time.Time
		if row.CreatedAt != nil {
			createdAt = *row.CreatedAt
		}
		pullRequests = append(pullRequests, domain.PullRequest{
			Repository:   row.Repository,
			Number:       row.Number,
			Title:        row.Title,
			Description:  row.Description,
			URL:          row.URL,
			AuthorID:     row.AuthorID,
			SourceBranch: row.SourceBranch,
			TargetBranch: row.TargetBranch,
			HeadSHA:      row.HeadSHA,
			CreatedAt:    createdAt,
			UpdatedAt:    row.UpdatedAt,
		})
	}
	return pullRequests
}
