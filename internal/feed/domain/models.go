package domain

import (
	"log/slog"
	"time"
)

// Subscriber identifies the agent token making a request.
type Subscriber struct {
	TokenID string
	User    string
}

// Subscription is one repository a token follows.
type Subscription struct {
	ID         int64
	TokenID    string
	Repository string
	CreatedAt  time.Time
}

// PullRequest is the feed's view of an open pull request; it carries no author name.
type PullRequest struct {
	Repository   string
	Number       int
	Title        string
	Description  string
	URL          string
	AuthorID     string
	SourceBranch string
	TargetBranch string
	HeadSHA      string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// NewSubscriptions asks the store to subscribe a token to repositories.
type NewSubscriptions struct {
	TokenID      string
	Repositories []string
	CreatedAt    time.Time
}

// Claim asks the store to mark pull requests delivered to a token and return the newly claimed ones.
type Claim struct {
	TokenID      string
	Repositories []string
	DeliveredAt  time.Time
}

// SubscriptionsParams are the dependencies of the subscriptions use case.
type SubscriptionsParams struct {
	Subscriptions SubscriptionStore
	Catalog       RepositoryCatalog
	Clock         Clock
	Logger        *slog.Logger
}

// PullRequestsParams are the dependencies of the pull-requests use case.
type PullRequestsParams struct {
	Subscriptions SubscriptionStore
	PullRequests  PullRequestStore
	Deliveries    DeliveryStore
	Catalog       RepositoryCatalog
	Clock         Clock
	Logger        *slog.Logger
}

// RemovedSubscriptions reports what a subscription cleanup removed.
type RemovedSubscriptions struct {
	// Count is the number of removed subscription rows, across all tokens.
	Count int
	// Repositories are the repositories whose subscriptions were removed, sorted.
	Repositories []string
}
