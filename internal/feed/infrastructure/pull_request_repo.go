package infrastructure

import (
	"context"
	"errors"
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
func (repo *PullRequestRepo) ChangedSince(ctx context.Context, query domain.SinceQuery) ([]domain.PullRequest, error) {
	rows, err := repo.pullRequests.ChangedSince(ctx, sinceQueryToRow(query))
	if err != nil {
		return nil, fmt.Errorf("feed: changed pull requests: %w", err)
	}
	return pullRequestsFromRows(rows), nil
}

// Find implements domain.PullRequestStore.
func (repo *PullRequestRepo) Find(ctx context.Context, key domain.PullRequestKey) (domain.PullRequest, bool, error) {
	row, err := repo.pullRequests.Find(ctx, key.Repository, key.Number)
	if errors.Is(err, persistence.ErrNotFound) {
		return domain.PullRequest{}, false, nil
	}
	if err != nil {
		return domain.PullRequest{}, false, fmt.Errorf("feed: find pull request: %w", err)
	}
	return pullRequestFromRow(row), true, nil
}

// ReceivedSince implements domain.PullRequestStore.
func (repo *PullRequestRepo) ReceivedSince(ctx context.Context, query domain.SinceQuery) ([]domain.PullRequest, error) {
	rows, err := repo.pullRequests.ReceivedSince(ctx, sinceQueryToRow(query))
	if err != nil {
		return nil, fmt.Errorf("feed: received pull requests: %w", err)
	}
	return pullRequestsFromRows(rows), nil
}

func sinceQueryToRow(query domain.SinceQuery) persistence.FeedSinceQuery {
	return persistence.FeedSinceQuery{
		TokenID:      query.TokenID,
		Repositories: query.Repositories,
		Since:        query.Since,
	}
}

func pullRequestToRow(pullRequest domain.PullRequest) persistence.FeedPullRequest {
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
		CreatedAt:    timePointer(pullRequest.CreatedAt),
		UpdatedAt:    pullRequest.UpdatedAt,

		PartyReviewers:  pullRequest.Party.Reviewers,
		RecordedAt:      timePointer(pullRequest.Party.RecordedAt),
		PartyDeadlineAt: timePointer(pullRequest.Party.DeadlineAt),
	}
}

func timePointer(moment time.Time) *time.Time {
	if moment.IsZero() {
		return nil
	}
	return &moment
}

func timeFromPointer(moment *time.Time) time.Time {
	if moment == nil {
		return time.Time{}
	}
	return *moment
}

func pullRequestsFromRows(rows []persistence.FeedPullRequest) []domain.PullRequest {
	pullRequests := make([]domain.PullRequest, 0, len(rows))
	for _, row := range rows {
		pullRequests = append(pullRequests, pullRequestFromRow(row))
	}
	return pullRequests
}

func pullRequestFromRow(row persistence.FeedPullRequest) domain.PullRequest {
	return domain.PullRequest{
		Repository:   row.Repository,
		Number:       row.Number,
		Title:        row.Title,
		Description:  row.Description,
		URL:          row.URL,
		AuthorID:     row.AuthorID,
		SourceBranch: row.SourceBranch,
		TargetBranch: row.TargetBranch,
		HeadSHA:      row.HeadSHA,
		CreatedAt:    timeFromPointer(row.CreatedAt),
		UpdatedAt:    row.UpdatedAt,
		Party: domain.PartyState{
			Reviewers:  row.PartyReviewers,
			RecordedAt: timeFromPointer(row.RecordedAt),
			DeadlineAt: timeFromPointer(row.PartyDeadlineAt),
		},
	}
}
