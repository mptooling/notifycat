package domain

// Feed limits, routes and parameter names.
const (
	AllRepositories          = "*"
	MaxSubscriptionsPerToken = 200
	MaxRequestBodyBytes      = 16 << 10

	ListSubscriptionsPattern  = "GET /api/v1/subs"
	AddSubscriptionsPattern   = "POST /api/v1/subs"
	RemoveSubscriptionPattern = "DELETE /api/v1/subs/{id}"
	PullRequestsPattern       = "GET /api/v1/subs/prs"
	SubscriptionIDPathValue   = "id"
	SinceQueryParameter       = "since"
)
