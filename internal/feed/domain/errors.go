package domain

import (
	"errors"
	"strings"
)

// Feed sentinel errors.
var (
	ErrSubscriptionNotFound = errors.New("feed: subscription not found")
	ErrTooManySubscriptions = errors.New("feed: too many subscriptions")
	ErrNoRepositories       = errors.New("feed: no repositories")
	ErrInvalidType          = errors.New("feed: invalid subscription type")
)

// NotSubscribedError lists named repositories the token does not follow.
type NotSubscribedError struct {
	Repositories []string
}

func (notSubscribed *NotSubscribedError) Error() string {
	return "feed: not subscribed: " + strings.Join(notSubscribed.Repositories, ", ")
}

// UnknownRepositoriesError lists requested repositories that are not configured.
type UnknownRepositoriesError struct {
	Repositories []string
}

func (unknown *UnknownRepositoriesError) Error() string {
	return "feed: unknown repositories: " + strings.Join(unknown.Repositories, ", ")
}
