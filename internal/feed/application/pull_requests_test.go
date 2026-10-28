package application_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/feed/application"
	"github.com/mptooling/notifycat/internal/feed/domain"
)

type pullRequestsFixture struct {
	subscriptions *fakeSubscriptionStore
	pullRequests  *fakePullRequestStore
	deliveries    *fakeDeliveryStore
	catalog       *fakeCatalog
	logs          *bytes.Buffer
	useCase       *application.PullRequests
}

func newPullRequestsFixture(subscribed ...string) pullRequestsFixture {
	fixture := pullRequestsFixture{
		subscriptions: &fakeSubscriptionStore{},
		pullRequests:  &fakePullRequestStore{},
		deliveries:    &fakeDeliveryStore{},
		catalog:       &fakeCatalog{repositories: []string{"acme/api", "acme/new"}},
	}
	fixture.subscriptions.seed(subscribed...)
	logger, logs := captureLogger()
	fixture.logs = logs
	fixture.useCase = application.NewPullRequests(domain.PullRequestsParams{
		Subscriptions: fixture.subscriptions,
		PullRequests:  fixture.pullRequests,
		Deliveries:    fixture.deliveries,
		Catalog:       fixture.catalog,
		Clock:         &fakeClock{now: now},
		Logger:        logger,
	})
	return fixture
}

func TestPullRequests_Next(t *testing.T) {
	testCases := []struct {
		name        string
		subscribed  []string
		stored      []string
		wantClaimed []string
		wantNoClaim bool
	}{
		{name: "unconfigured repository dropped", subscribed: []string{"acme/api", "old/gone"}, wantClaimed: []string{"acme/api"}},
		{name: "all repositories includes later ones", subscribed: []string{"*"}, stored: []string{"acme/api", "acme/new"}, wantClaimed: []string{"acme/api", "acme/new"}},
		{name: "no subscriptions", wantNoClaim: true},
		{name: "all unconfigured", subscribed: []string{"old/gone"}, wantNoClaim: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newPullRequestsFixture(testCase.subscribed...)
			fixture.pullRequests.repositories = testCase.stored
			fixture.deliveries.claimed = []domain.PullRequest{{Repository: "acme/api", Number: 1}}

			delivered, err := fixture.useCase.Next(t.Context(), subscriber)

			require.NoError(t, err)
			if testCase.wantNoClaim {
				assert.Empty(t, fixture.deliveries.claims)
				assert.Empty(t, delivered)
				return
			}
			require.Len(t, fixture.deliveries.claims, 1)
			assert.ElementsMatch(t, testCase.wantClaimed, fixture.deliveries.claims[0].Repositories)
			assert.Equal(t, tokenID, fixture.deliveries.claims[0].TokenID)
			assert.Equal(t, now, fixture.deliveries.claims[0].DeliveredAt)
			assert.Len(t, delivered, 1)
		})
	}
}

func TestPullRequests_NextLogs(t *testing.T) {
	t.Run("delivered", func(t *testing.T) {
		fixture := newPullRequestsFixture("acme/api")
		fixture.deliveries.claimed = []domain.PullRequest{{Number: 1}, {Number: 2}, {Number: 3}}

		_, err := fixture.useCase.Next(t.Context(), subscriber)

		require.NoError(t, err)
		assert.Contains(t, fixture.logs.String(), "feed pull requests delivered")
		assert.Contains(t, fixture.logs.String(), "count=3")
		assert.Contains(t, fixture.logs.String(), "user=alice")
		assert.Contains(t, fixture.logs.String(), "token_id="+tokenID)
	})

	t.Run("nothing delivered", func(t *testing.T) {
		fixture := newPullRequestsFixture("acme/api")

		_, err := fixture.useCase.Next(t.Context(), subscriber)

		require.NoError(t, err)
		assert.NotContains(t, fixture.logs.String(), "feed pull requests delivered")
	})
}

func TestPullRequests_Since(t *testing.T) {
	since := time.Date(2026, 10, 28, 11, 0, 0, 0, time.FixedZone("plus2", 2*60*60))

	t.Run("resolves repositories and never claims", func(t *testing.T) {
		fixture := newPullRequestsFixture("*", "old/gone")
		fixture.pullRequests.repositories = []string{"acme/api", "acme/new", "old/gone"}
		fixture.pullRequests.changed = []domain.PullRequest{{Number: 5}}

		changed, err := fixture.useCase.Since(t.Context(), subscriber, since)

		require.NoError(t, err)
		assert.Len(t, changed, 1)
		require.Len(t, fixture.pullRequests.changedCalls, 1)
		call := fixture.pullRequests.changedCalls[0]
		assert.ElementsMatch(t, []string{"acme/api", "acme/new"}, call.repositories)
		assert.Equal(t, time.UTC, call.since.Location())
		assert.True(t, since.Equal(call.since))
		assert.Empty(t, fixture.deliveries.claims)
	})

	t.Run("nothing resolves", func(t *testing.T) {
		fixture := newPullRequestsFixture("old/gone")

		changed, err := fixture.useCase.Since(t.Context(), subscriber, since)

		require.NoError(t, err)
		assert.Empty(t, changed)
		assert.Empty(t, fixture.pullRequests.changedCalls)
	})
}
