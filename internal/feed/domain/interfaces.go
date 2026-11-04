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
	// SetType returns how many subscriptions changed type; subscriptions that already have the type do not count.
	SetType(ctx context.Context, tokenID string, change TypeChange) (int, error)
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
	// ChangedSince returns pull requests that changed at or after Since, and those delivered to the token at or after Since.
	ChangedSince(ctx context.Context, query SinceQuery) ([]PullRequest, error)
	// Find returns false when the pull request is not stored.
	Find(ctx context.Context, key PullRequestKey) (PullRequest, bool, error)
	// ReceivedSince returns pull requests the token received, solo or party, that changed or reached it at or after Since.
	ReceivedSince(ctx context.Context, query SinceQuery) ([]PullRequest, error)
}

// DeliveryStore records which pull requests were delivered to which token.
type DeliveryStore interface {
	// Claim returns only pull requests not yet delivered to the token, marking them delivered at most once.
	Claim(ctx context.Context, claim Claim) ([]PullRequest, error)
	// ClaimParty delivers the pull requests the turn rule lets the token take, at most once each, in one atomic step.
	ClaimParty(ctx context.Context, claim PartyClaim) (PartyClaimResult, error)
}

// PollStore records when tokens poll the feed.
type PollStore interface {
	// Record stores the poll time of the token. It keeps a stored time that is less than PollResolution older.
	Record(ctx context.Context, tokenID string, polledAt time.Time) error
}

// PartySettingsResolver gives the party settings that apply to a pull request.
type PartySettingsResolver interface {
	Resolve(ctx context.Context, key PullRequestKey) (PartySettings, error)
}

// RepositoryCatalog answers which repositories the operator configured.
type RepositoryCatalog interface {
	IsConfigured(repository string) bool
	// HasRepositories reports whether any repository, explicit or through an org tier, is configured.
	HasRepositories() bool
	// Configured returns the subset of repositories that is configured.
	Configured(repositories []string) []string
	// List returns the explicitly configured repositories and the organizations configured through an org tier.
	List() ConfiguredRepositories
}

// Clock provides the current time.
type Clock interface {
	Now() time.Time
}

// Subscriptions manages a token's repository subscriptions.
type Subscriptions interface {
	List(ctx context.Context, subscriber Subscriber) ([]Subscription, error)
	// Add subscribes to the repositories with the requested type and returns the full list; it adds nothing when any request is rejected.
	Add(ctx context.Context, subscriber Subscriber, request SubscriptionRequest) ([]Subscription, error)
	// ChangeType sets the type of the subscriber's subscriptions (nil Repositories: every one) and returns the full list.
	// It returns ErrInvalidType for an unknown type, ErrNoRepositories for a non-nil empty list and NotSubscribedError when a named repository is not subscribed; nothing changes then.
	ChangeType(ctx context.Context, subscriber Subscriber, change TypeChange) ([]Subscription, error)
	// Remove returns ErrSubscriptionNotFound when the id is not the subscriber's.
	Remove(ctx context.Context, subscriber Subscriber, id int64) error
	// Available returns the repositories and organizations a subscriber can subscribe to.
	Available() ConfiguredRepositories
}

// PullRequests serves a token's feed of open pull requests.
type PullRequests interface {
	// Next returns pull requests not yet delivered to the subscriber and marks them delivered. It delivers party
	// repositories by the turn rule within the cap, and records the poll.
	Next(ctx context.Context, subscriber Subscriber) ([]PullRequest, error)
	// Since returns pull requests changed after the given time without marking anything delivered.
	// On party repositories it returns only pull requests the token received.
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
	// RemoveUnconfigured removes every token's subscriptions to repositories the catalog no longer configures and keeps the all-repositories subscription.
	// It removes nothing when the catalog configures no repository at all.
	RemoveUnconfigured(ctx context.Context) (RemovedSubscriptions, error)
}

// Recorder keeps the feed's pull-request store in sync with provider events.
type Recorder interface {
	Record(ctx context.Context, event kernel.Event) error
}

// TurnPolicy decides which waiting pull requests the poller may take in a party round.
type TurnPolicy interface {
	// Choose keeps the input order in Take. Taken counts pull requests at their cap; Waiting counts those
	// where another member has the turn. The caller's Loads map is not changed.
	Choose(round PartyRound) PartyDecision
}
