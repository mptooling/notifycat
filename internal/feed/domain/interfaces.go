package domain

import (
	"context"
	"time"

	"github.com/mptooling/notifycat/internal/kernel"
)

// SubscriptionStore persists token subscriptions.
type SubscriptionStore interface {
	List(ctx context.Context, tokenID string) ([]Subscription, error)
	// Add returns how many rows were newly inserted; already-subscribed repositories are skipped.
	Add(ctx context.Context, subscriptions NewSubscriptions) (int, error)
	// Delete returns ErrSubscriptionNotFound when the id is not the token's.
	Delete(ctx context.Context, tokenID string, id int64) (Subscription, error)
	// DeleteRepositories removes the repositories' subscriptions of every token and returns how many it removed.
	DeleteRepositories(ctx context.Context, repositories []string) (int, error)
	// Repositories returns every repository that has a subscription.
	Repositories(ctx context.Context) ([]string, error)
}

// PullRequestStore persists open pull requests for the feed.
type PullRequestStore interface {
	Upsert(ctx context.Context, pullRequest PullRequest) error
	Delete(ctx context.Context, repository string, number int) error
	// DeleteAll removes every pull request with its deliveries and returns how many pull requests it removed.
	DeleteAll(ctx context.Context) (int, error)
	// DeleteRepositories removes the pull requests of the repositories with their deliveries and returns how many pull requests it removed.
	DeleteRepositories(ctx context.Context, repositories []string) (int, error)
	// Repositories returns every repository that has a stored pull request.
	Repositories(ctx context.Context) ([]string, error)
	ChangedSince(ctx context.Context, repositories []string, since time.Time) ([]PullRequest, error)
}

// DeliveryStore records which pull requests were delivered to which token.
type DeliveryStore interface {
	// Claim returns only pull requests not yet delivered to the token, marking them delivered at most once.
	Claim(ctx context.Context, claim Claim) ([]PullRequest, error)
}

// RepositoryCatalog answers which repositories the operator configured.
type RepositoryCatalog interface {
	IsConfigured(repository string) bool
	// Configured returns the subset of repositories that is configured.
	Configured(repositories []string) []string
}

// Clock provides the current time.
type Clock interface {
	Now() time.Time
}

// Subscriptions manages a token's repository subscriptions.
type Subscriptions interface {
	List(ctx context.Context, subscriber Subscriber) ([]Subscription, error)
	// Add subscribes to the repositories and returns the full list; it adds nothing when any request is rejected.
	Add(ctx context.Context, subscriber Subscriber, repositories []string) ([]Subscription, error)
	// Remove returns ErrSubscriptionNotFound when the id is not the subscriber's.
	Remove(ctx context.Context, subscriber Subscriber, id int64) error
}

// PullRequests serves a token's feed of open pull requests.
type PullRequests interface {
	// Next returns pull requests not yet delivered to the subscriber and marks them delivered.
	Next(ctx context.Context, subscriber Subscriber) ([]PullRequest, error)
	// Since returns pull requests changed after the given time without marking anything delivered.
	Since(ctx context.Context, subscriber Subscriber, since time.Time) ([]PullRequest, error)
}

// PullRequestCleaner removes stored pull requests the feed can no longer keep in sync; subscriptions and tokens stay.
type PullRequestCleaner interface {
	// RemoveAll removes every stored pull request and returns how many it removed.
	RemoveAll(ctx context.Context) (int, error)
	// RemoveUnconfigured removes the pull requests of repositories the catalog no longer configures and returns how many it removed.
	RemoveUnconfigured(ctx context.Context) (int, error)
}

// SubscriptionCleaner removes subscriptions the feed can no longer serve; tokens and stored pull requests stay.
type SubscriptionCleaner interface {
	// RemoveUnconfigured removes every token's subscriptions to repositories the catalog no longer configures, keeps the all-repositories subscription, and returns how many it removed.
	RemoveUnconfigured(ctx context.Context) (int, error)
}

// Recorder keeps the feed's pull-request store in sync with provider events.
type Recorder interface {
	Record(ctx context.Context, event kernel.Event) error
}
