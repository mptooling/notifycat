package domain

import "time"

// RecordTimeout caps how long a webhook waits for the feed record.
const RecordTimeout = 3 * time.Second

// AwayAfter is how long a party member stays a candidate after its last fetch.
const AwayAfter = 24 * time.Hour

// FetchResolution is how old a stored fetch time must be before a fetch writes a new one.
const FetchResolution = time.Minute

// Feed limits, the repository name rule, routes and parameter names.
const (
	AllRepositories          = "*"
	MaxSubscriptionsPerToken = 200
	MaxRequestBodyBytes      = 16 << 10
	RepositoryNamePattern    = `^[a-z0-9._-]+/[a-z0-9._-]+$`

	ListSubscriptionsPattern   = "GET /api/v1/subs"
	AddSubscriptionsPattern    = "POST /api/v1/subs"
	ChangeSubscriptionsPattern = "PATCH /api/v1/subs"
	RemoveSubscriptionPattern  = "DELETE /api/v1/subs/{id}"
	PullRequestsPattern        = "GET /api/v1/subs/prs"
	RepositoriesPattern        = "GET /api/v1/repos"
	SubscriptionIDPathValue    = "id"
	SinceQueryParameter        = "since"
)

// DayLayout formats the day of the party loads.
const DayLayout = "2006-01-02"
