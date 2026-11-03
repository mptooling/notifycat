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
	Type       SubscriptionType
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
	Party        PartyState
}

// ConfiguredRepositories is what a token can subscribe to.
type ConfiguredRepositories struct {
	// Repositories are the explicitly configured repositories, sorted.
	Repositories []string
	// Organizations have an org tier, so each of their repositories is configured too; sorted.
	Organizations []string
}

// SubscriptionRequest asks to subscribe to repositories; an empty Type means solo.
type SubscriptionRequest struct {
	Repositories []string
	Type         SubscriptionType
}

// NewSubscriptions asks the store to subscribe a token to repositories.
type NewSubscriptions struct {
	TokenID      string
	Repositories []string
	Type         SubscriptionType
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

// PullRequestKey names one pull request.
type PullRequestKey struct {
	Repository string
	Number     int
}

// PartyState is the party record of a pull request. A zero DeadlineAt means the timer ran out.
type PartyState struct {
	Reviewers  int
	RecordedAt time.Time
	DeadlineAt time.Time
}

// PartyMember is one token inside one repository.
type PartyMember struct {
	TokenID    string
	Repository string
}

// WaitingPullRequest is a pull request a party poller could take.
type WaitingPullRequest struct {
	Key             PullRequestKey
	Party           PartyState
	PartyDeliveries int
	// Candidates are the other members of the repository, not away, without a delivery of this pull request.
	Candidates []string
}

// PartyRound is the input of one party claim.
type PartyRound struct {
	TokenID      string
	Now          time.Time
	PullRequests []WaitingPullRequest
	Loads        map[PartyMember]int
}

// PartyDecision is the outcome of one party claim.
type PartyDecision struct {
	Take    []PullRequestKey
	Taken   int
	Waiting int
}

// PartySettings are the global review party settings.
type PartySettings struct {
	Reviewers int
	Deadline  time.Duration
}

// Settings are the feed settings built from the config.
type Settings struct {
	Party    PartySettings
	Location *time.Location
}

// WorkCalendarParams are the dependencies of the work calendar.
type WorkCalendarParams struct {
	DaysOff  DayOffCalendar
	Location *time.Location
}

// TypeChange sets the type of a token's subscriptions; nil Repositories means every subscription.
type TypeChange struct {
	Repositories []string
	Type         SubscriptionType
}

// ReceivedQuery asks for the pull requests a token received that changed since a time.
type ReceivedQuery struct {
	TokenID      string
	Repositories []string
	Since        time.Time
}

// PartyClaim asks the store to deliver party pull requests to a token.
type PartyClaim struct {
	TokenID      string
	Repositories []string
	DeliveredAt  time.Time
	Day          string
	ActiveSince  time.Time
}

// PartyClaimResult is what a party claim delivered and how the turn rule judged the rest.
type PartyClaimResult struct {
	Delivered []PullRequest
	Taken     int
	Waiting   int
}
