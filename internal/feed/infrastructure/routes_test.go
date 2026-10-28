package infrastructure_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mptooling/notifycat/internal/feed/domain"
	"github.com/mptooling/notifycat/internal/feed/infrastructure"
)

func TestNewRoutes_Patterns(t *testing.T) {
	routes := infrastructure.NewRoutes(&fakeSubscriptions{}, &fakePullRequests{}, discardLogger())

	patterns := make([]string, 0, len(routes))
	for _, route := range routes {
		patterns = append(patterns, route.Pattern)
	}

	assert.ElementsMatch(t, []string{
		domain.ListSubscriptionsPattern,
		domain.AddSubscriptionsPattern,
		domain.RemoveSubscriptionPattern,
		domain.PullRequestsPattern,
		domain.RepositoriesPattern,
	}, patterns)
}
