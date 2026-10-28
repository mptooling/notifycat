package domain

import "time"

// RecordTimeout caps how long a webhook waits for the feed record.
const RecordTimeout = 3 * time.Second

// Feed limits, the repository name rule, routes and parameter names.
const (
	AllRepositories          = "*"
	MaxSubscriptionsPerToken = 200
	MaxRequestBodyBytes      = 16 << 10
	RepositoryNamePattern    = `^[a-z0-9._-]+/[a-z0-9._-]+$`

	ListSubscriptionsPattern  = "GET /api/v1/subs"
	AddSubscriptionsPattern   = "POST /api/v1/subs"
	RemoveSubscriptionPattern = "DELETE /api/v1/subs/{id}"
	PullRequestsPattern       = "GET /api/v1/subs/prs"
	RepositoriesPattern       = "GET /api/v1/repos"
	SubscriptionIDPathValue   = "id"
	SinceQueryParameter       = "since"
)
