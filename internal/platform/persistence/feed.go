package persistence

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// FeedSubscriptions persists which repositories each token follows.
type FeedSubscriptions struct {
	db *gorm.DB
}

// NewFeedSubscriptions constructs a FeedSubscriptions repository bound to db.
func NewFeedSubscriptions(db *gorm.DB) *FeedSubscriptions {
	return &FeedSubscriptions{db: db}
}

// List returns the token's subscriptions in id order.
func (r *FeedSubscriptions) List(ctx context.Context, tokenID string) ([]FeedSubscription, error) {
	var subscriptions []FeedSubscription
	err := r.db.WithContext(ctx).Where("token_id = ?", tokenID).Order("id").Find(&subscriptions).Error
	if err != nil {
		return nil, fmt.Errorf("store: list feed subscriptions: %w", err)
	}
	return subscriptions, nil
}

// Add inserts the repositories not yet subscribed and returns how many were new.
func (r *FeedSubscriptions) Add(ctx context.Context, batch FeedSubscriptionBatch) (int, error) {
	if len(batch.Repositories) == 0 {
		return 0, nil
	}
	createdAt := batch.CreatedAt.UTC()
	rows := make([]FeedSubscription, 0, len(batch.Repositories))
	for _, repository := range batch.Repositories {
		rows = append(rows, FeedSubscription{TokenID: batch.TokenID, Repository: repository, CreatedAt: createdAt})
	}
	result := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "token_id"}, {Name: "repository"}},
			DoNothing: true,
		}).
		Create(&rows)
	if result.Error != nil {
		return 0, fmt.Errorf("store: add feed subscriptions: %w", result.Error)
	}
	return int(result.RowsAffected), nil
}

// Delete removes the token's subscription and returns it, or ErrNotFound.
func (r *FeedSubscriptions) Delete(ctx context.Context, tokenID string, id int64) (FeedSubscription, error) {
	var deleted []FeedSubscription
	err := r.db.WithContext(ctx).Raw(
		"DELETE FROM feed_subscriptions WHERE id = ? AND token_id = ? RETURNING id, token_id, repository, created_at",
		id, tokenID,
	).Scan(&deleted).Error
	if err != nil {
		return FeedSubscription{}, fmt.Errorf("store: delete feed subscription: %w", err)
	}
	if len(deleted) == 0 {
		return FeedSubscription{}, ErrNotFound
	}
	return deleted[0], nil
}

// DeleteRepositories removes the repositories' subscriptions of every token and returns how many rows went.
func (r *FeedSubscriptions) DeleteRepositories(ctx context.Context, repositories []string) (int, error) {
	if len(repositories) == 0 {
		return 0, nil
	}
	result := r.db.WithContext(ctx).Where("repository IN ?", repositories).Delete(&FeedSubscription{})
	if result.Error != nil {
		return 0, fmt.Errorf("store: delete feed subscriptions of repositories: %w", result.Error)
	}
	return int(result.RowsAffected), nil
}

// Repositories returns the distinct repositories that have a subscription, sorted.
func (r *FeedSubscriptions) Repositories(ctx context.Context) ([]string, error) {
	var repositories []string
	err := r.db.WithContext(ctx).Model(&FeedSubscription{}).
		Distinct().Order("repository").Pluck("repository", &repositories).Error
	if err != nil {
		return nil, fmt.Errorf("store: list subscribed repositories: %w", err)
	}
	return repositories, nil
}

// FeedPullRequests persists the latest state of open pull requests.
type FeedPullRequests struct {
	db *gorm.DB
}

// NewFeedPullRequests constructs a FeedPullRequests repository bound to db.
func NewFeedPullRequests(db *gorm.DB) *FeedPullRequests {
	return &FeedPullRequests{db: db}
}

// Upsert inserts or updates the pull request in place, keeping its deliveries and a known created_at.
func (r *FeedPullRequests) Upsert(ctx context.Context, pullRequest FeedPullRequest) error {
	pullRequest.UpdatedAt = pullRequest.UpdatedAt.UTC()
	if pullRequest.CreatedAt != nil {
		createdAt := pullRequest.CreatedAt.UTC()
		pullRequest.CreatedAt = &createdAt
	}
	assignments := append(
		clause.AssignmentColumns([]string{
			"title", "description", "url", "author_id", "source_branch",
			"target_branch", "head_sha", "updated_at",
		}),
		clause.Assignment{
			Column: clause.Column{Name: "created_at"},
			Value:  gorm.Expr("COALESCE(excluded.created_at, feed_pull_requests.created_at)"),
		},
	)
	err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "repository"}, {Name: "number"}},
			DoUpdates: assignments,
		}).
		Create(&pullRequest).Error
	if err != nil {
		return fmt.Errorf("store: upsert feed pull request: %w", err)
	}
	return nil
}

// Delete removes the pull request and its deliveries; a missing row is fine.
func (r *FeedPullRequests) Delete(ctx context.Context, repository string, number int) error {
	err := r.db.WithContext(ctx).
		Where("repository = ? AND number = ?", repository, number).
		Delete(&FeedPullRequest{}).Error
	if err != nil {
		return fmt.Errorf("store: delete feed pull request: %w", err)
	}
	return nil
}

// DeleteAll removes every pull request and its deliveries and returns how many pull requests went.
func (r *FeedPullRequests) DeleteAll(ctx context.Context) (int, error) {
	result := r.db.WithContext(ctx).Exec("DELETE FROM feed_pull_requests")
	if result.Error != nil {
		return 0, fmt.Errorf("store: delete all feed pull requests: %w", result.Error)
	}
	return int(result.RowsAffected), nil
}

// DeleteRepositories removes the pull requests of the repositories and their deliveries.
func (r *FeedPullRequests) DeleteRepositories(ctx context.Context, repositories []string) (int, error) {
	if len(repositories) == 0 {
		return 0, nil
	}
	result := r.db.WithContext(ctx).Where("repository IN ?", repositories).Delete(&FeedPullRequest{})
	if result.Error != nil {
		return 0, fmt.Errorf("store: delete feed pull requests of repositories: %w", result.Error)
	}
	return int(result.RowsAffected), nil
}

// Repositories returns the distinct repositories that have a pull request, sorted.
func (r *FeedPullRequests) Repositories(ctx context.Context) ([]string, error) {
	var repositories []string
	err := r.db.WithContext(ctx).Model(&FeedPullRequest{}).
		Distinct().Order("repository").Pluck("repository", &repositories).Error
	if err != nil {
		return nil, fmt.Errorf("store: list feed repositories: %w", err)
	}
	return repositories, nil
}

// ChangedSince returns pull requests of the repositories updated at or after since.
func (r *FeedPullRequests) ChangedSince(ctx context.Context, repositories []string, since time.Time) ([]FeedPullRequest, error) {
	if len(repositories) == 0 {
		return []FeedPullRequest{}, nil
	}
	var pullRequests []FeedPullRequest
	err := r.db.WithContext(ctx).
		Where("repository IN ? AND updated_at >= ?", repositories, since.UTC()).
		Order("updated_at, repository, number").
		Find(&pullRequests).Error
	if err != nil {
		return nil, fmt.Errorf("store: feed pull requests changed since: %w", err)
	}
	return pullRequests, nil
}

// FeedDeliveries records which pull requests each token has received.
type FeedDeliveries struct {
	db *gorm.DB
}

// NewFeedDeliveries constructs a FeedDeliveries repository bound to db.
func NewFeedDeliveries(db *gorm.DB) *FeedDeliveries {
	return &FeedDeliveries{db: db}
}

// Claim writes first so SQLite never upgrades a read lock, which fails at once with SQLITE_BUSY.
func (r *FeedDeliveries) Claim(ctx context.Context, claim FeedClaim) ([]FeedPullRequest, error) {
	if len(claim.Repositories) == 0 {
		return []FeedPullRequest{}, nil
	}
	claimed := []FeedPullRequest{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var inserted []FeedDelivery
		insertErr := tx.Raw(
			`INSERT INTO feed_deliveries (token_id, repository, number, delivered_at)
			 SELECT ?, repository, number, ? FROM feed_pull_requests WHERE repository IN ?
			 ON CONFLICT DO NOTHING RETURNING repository, number`,
			claim.TokenID, claim.DeliveredAt.UTC(), claim.Repositories,
		).Scan(&inserted).Error
		if insertErr != nil {
			return fmt.Errorf("insert deliveries: %w", insertErr)
		}
		if len(inserted) == 0 {
			return nil
		}
		keys := make([][]any, 0, len(inserted))
		for _, delivery := range inserted {
			keys = append(keys, []any{delivery.Repository, delivery.Number})
		}
		readErr := tx.Where("(repository, number) IN ?", keys).
			Order("updated_at, repository, number").
			Find(&claimed).Error
		if readErr != nil {
			return fmt.Errorf("read claimed pull requests: %w", readErr)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("store: claim feed pull requests: %w", err)
	}
	return claimed, nil
}
