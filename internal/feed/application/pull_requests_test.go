package application_test

import (
	"bytes"
	"errors"
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
	polls         *fakePollStore
	catalog       *fakeCatalog
	calendar      *fakeWorkCalendar
	calls         *callLog
	logs          *bytes.Buffer
	useCase       *application.PullRequests
}

func newPullRequestsFixture(subscribed ...string) pullRequestsFixture {
	calls := &callLog{}
	fixture := pullRequestsFixture{
		subscriptions: &fakeSubscriptionStore{},
		pullRequests:  &fakePullRequestStore{},
		deliveries:    &fakeDeliveryStore{calls: calls},
		polls:         &fakePollStore{calls: calls},
		catalog:       &fakeCatalog{repositories: []string{"acme/api", "acme/new", "acme/web", "acme/lib"}},
		calendar:      &fakeWorkCalendar{day: "2026-11-03"},
		calls:         calls,
	}
	fixture.subscriptions.seed(subscribed...)
	logger, logs := captureDebugLogger()
	fixture.logs = logs
	fixture.useCase = application.NewPullRequests(domain.PullRequestsParams{
		Subscriptions: fixture.subscriptions,
		PullRequests:  fixture.pullRequests,
		Deliveries:    fixture.deliveries,
		Polls:         fixture.polls,
		Catalog:       fixture.catalog,
		Calendar:      fixture.calendar,
		Clock:         &fakeClock{now: now},
		Logger:        logger,
	})
	return fixture
}

func pullRequestKeys(pullRequests []domain.PullRequest) []domain.PullRequestKey {
	keys := make([]domain.PullRequestKey, 0, len(pullRequests))
	for _, pullRequest := range pullRequests {
		keys = append(keys, domain.PullRequestKey{Repository: pullRequest.Repository, Number: pullRequest.Number})
	}
	return keys
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
			assert.Empty(t, fixture.deliveries.partyClaims)
			assert.Equal(t, []pollRecord{{tokenID: tokenID, polledAt: now}}, fixture.polls.records, "every poll counts, also one with nothing to claim")
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

func TestPullRequests_NextOnADayOff(t *testing.T) {
	fixture := newPullRequestsFixture("acme/api")
	fixture.subscriptions.seedOfType(domain.SubscriptionTypeParty, "acme/web")
	fixture.calendar.dayOff = true
	fixture.deliveries.claimed = []domain.PullRequest{{Repository: "acme/api", Number: 1}}

	delivered, err := fixture.useCase.Next(t.Context(), subscriber)

	require.NoError(t, err)
	assert.NotNil(t, delivered)
	assert.Empty(t, delivered)
	assert.Empty(t, fixture.deliveries.claims)
	assert.Empty(t, fixture.deliveries.partyClaims)
	assert.Equal(t, []time.Time{now}, fixture.calendar.dayOffChecks)
	assert.Equal(t, []pollRecord{{tokenID: tokenID, polledAt: now}}, fixture.polls.records)
	assert.Contains(t, fixture.logs.String(), `level=DEBUG msg="feed day off" token_id=`+tokenID+"\n")
}

func TestPullRequests_NextSplitsSoloAndParty(t *testing.T) {
	fixture := newPullRequestsFixture("acme/api")
	fixture.subscriptions.seedOfType(domain.SubscriptionTypeParty, "acme/web", "*")
	fixture.pullRequests.repositories = []string{"acme/api", "acme/web", "acme/lib"}

	_, err := fixture.useCase.Next(t.Context(), subscriber)

	require.NoError(t, err)
	require.Len(t, fixture.deliveries.claims, 1)
	assert.Equal(t, []string{"acme/api"}, fixture.deliveries.claims[0].Repositories)
	require.Len(t, fixture.deliveries.partyClaims, 1)
	partyClaim := fixture.deliveries.partyClaims[0]
	assert.ElementsMatch(t, []string{"acme/web", "acme/lib"}, partyClaim.Repositories)
	assert.Equal(t, tokenID, partyClaim.TokenID)
	assert.Equal(t, now, partyClaim.DeliveredAt)
	assert.Equal(t, "2026-11-03", partyClaim.Day)
	assert.Equal(t, []time.Time{now}, fixture.calendar.dayMoments)
	assert.Equal(t, now.Add(-24*time.Hour), partyClaim.ActiveSince)
}

func TestPullRequests_NextDropsUnconfiguredPartyRepositories(t *testing.T) {
	fixture := newPullRequestsFixture()
	fixture.subscriptions.seedOfType(domain.SubscriptionTypeParty, "*")
	fixture.pullRequests.repositories = []string{"acme/web", "old/gone"}

	_, err := fixture.useCase.Next(t.Context(), subscriber)

	require.NoError(t, err)
	require.Len(t, fixture.deliveries.partyClaims, 1)
	assert.Equal(t, []string{"acme/web"}, fixture.deliveries.partyClaims[0].Repositories)
}

func TestPullRequests_NextWithOnlyPartyRepositoriesMakesNoSoloClaim(t *testing.T) {
	fixture := newPullRequestsFixture()
	fixture.subscriptions.seedOfType(domain.SubscriptionTypeParty, "acme/web")

	_, err := fixture.useCase.Next(t.Context(), subscriber)

	require.NoError(t, err)
	assert.Empty(t, fixture.deliveries.claims)
	assert.Len(t, fixture.deliveries.partyClaims, 1)
}

func TestPullRequests_NextOwnSubscriptionOverridesAll(t *testing.T) {
	testCases := []struct {
		name      string
		ownType   domain.SubscriptionType
		allType   domain.SubscriptionType
		wantSolo  []string
		wantParty []string
	}{
		{name: "own solo in a party of all", ownType: domain.SubscriptionTypeSolo, allType: domain.SubscriptionTypeParty, wantSolo: []string{"acme/api"}, wantParty: []string{"acme/web"}},
		{name: "own party in a solo of all", ownType: domain.SubscriptionTypeParty, allType: domain.SubscriptionTypeSolo, wantSolo: []string{"acme/web"}, wantParty: []string{"acme/api"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newPullRequestsFixture()
			fixture.subscriptions.seedOfType(testCase.allType, "*")
			fixture.subscriptions.seedOfType(testCase.ownType, "acme/api")
			fixture.pullRequests.repositories = []string{"acme/api", "acme/web"}

			_, err := fixture.useCase.Next(t.Context(), subscriber)

			require.NoError(t, err)
			require.Len(t, fixture.deliveries.claims, 1)
			require.Len(t, fixture.deliveries.partyClaims, 1)
			assert.Equal(t, testCase.wantSolo, fixture.deliveries.claims[0].Repositories)
			assert.Equal(t, testCase.wantParty, fixture.deliveries.partyClaims[0].Repositories)
		})
	}
}

func TestPullRequests_NextMergesInDeliveryOrder(t *testing.T) {
	early := now.Add(-2 * time.Hour)
	late := now.Add(-time.Hour)
	fixture := newPullRequestsFixture("acme/api")
	fixture.subscriptions.seedOfType(domain.SubscriptionTypeParty, "acme/web")
	fixture.deliveries.claimed = []domain.PullRequest{
		{Repository: "acme/api", Number: 7, UpdatedAt: late},
		{Repository: "acme/api", Number: 3, UpdatedAt: late},
		{Repository: "acme/api", Number: 9, UpdatedAt: early},
	}
	fixture.deliveries.partyResult = domain.PartyClaimResult{Delivered: []domain.PullRequest{
		{Repository: "acme/web", Number: 1, UpdatedAt: late},
		{Repository: "acme/web", Number: 2, UpdatedAt: early},
	}}

	delivered, err := fixture.useCase.Next(t.Context(), subscriber)

	require.NoError(t, err)
	assert.Equal(t, []domain.PullRequestKey{
		{Repository: "acme/api", Number: 9},
		{Repository: "acme/web", Number: 2},
		{Repository: "acme/api", Number: 3},
		{Repository: "acme/api", Number: 7},
		{Repository: "acme/web", Number: 1},
	}, pullRequestKeys(delivered))
}

func TestPullRequests_NextLogs(t *testing.T) {
	t.Run("delivered", func(t *testing.T) {
		fixture := newPullRequestsFixture("acme/api")
		fixture.subscriptions.seedOfType(domain.SubscriptionTypeParty, "acme/web")
		fixture.deliveries.claimed = []domain.PullRequest{{Repository: "acme/api", Number: 1}, {Repository: "acme/api", Number: 2}}
		fixture.deliveries.partyResult = domain.PartyClaimResult{Delivered: []domain.PullRequest{{Repository: "acme/web", Number: 3}}}

		_, err := fixture.useCase.Next(t.Context(), subscriber)

		require.NoError(t, err)
		assert.Contains(t, fixture.logs.String(), `level=INFO msg="feed pull requests delivered" user=alice token_id=`+tokenID+" count=3 party=1\n")
	})

	t.Run("nothing delivered or held", func(t *testing.T) {
		fixture := newPullRequestsFixture("acme/api")
		fixture.subscriptions.seedOfType(domain.SubscriptionTypeParty, "acme/web")

		_, err := fixture.useCase.Next(t.Context(), subscriber)

		require.NoError(t, err)
		assert.NotContains(t, fixture.logs.String(), "feed pull requests delivered")
		assert.NotContains(t, fixture.logs.String(), "feed party pull requests held")
	})
}

func TestPullRequests_NextLogsHeldPartyPullRequests(t *testing.T) {
	testCases := []struct {
		name     string
		taken    int
		waiting  int
		wantLine string
	}{
		{name: "taken and waiting", taken: 1, waiting: 2, wantLine: `level=DEBUG msg="feed party pull requests held" token_id=` + tokenID + " taken=1 waiting=2\n"},
		{name: "only taken", taken: 1, wantLine: `level=DEBUG msg="feed party pull requests held" token_id=` + tokenID + " taken=1 waiting=0\n"},
		{name: "only waiting", waiting: 1, wantLine: `level=DEBUG msg="feed party pull requests held" token_id=` + tokenID + " taken=0 waiting=1\n"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newPullRequestsFixture()
			fixture.subscriptions.seedOfType(domain.SubscriptionTypeParty, "acme/web")
			fixture.deliveries.partyResult = domain.PartyClaimResult{Taken: testCase.taken, Waiting: testCase.waiting}

			_, err := fixture.useCase.Next(t.Context(), subscriber)

			require.NoError(t, err)
			assert.Contains(t, fixture.logs.String(), testCase.wantLine)
		})
	}
}

func TestPullRequests_NextRecordsThePollAfterClaiming(t *testing.T) {
	fixture := newPullRequestsFixture("acme/api")
	fixture.subscriptions.seedOfType(domain.SubscriptionTypeParty, "acme/web")

	_, err := fixture.useCase.Next(t.Context(), subscriber)

	require.NoError(t, err)
	assert.Equal(t, []string{"claim", "claim party", "record poll"}, fixture.calls.calls)
	assert.Equal(t, []pollRecord{{tokenID: tokenID, polledAt: now}}, fixture.polls.records)
}

func TestPullRequests_NextClaimErrorRecordsNoPoll(t *testing.T) {
	errClaim := errors.New("database is locked")
	testCases := []struct {
		name      string
		claimErr  error
		partyErr  error
		wantCalls []string
	}{
		{name: "solo claim fails", claimErr: errClaim, wantCalls: []string{"claim"}},
		{name: "party claim fails", partyErr: errClaim, wantCalls: []string{"claim", "claim party"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newPullRequestsFixture("acme/api")
			fixture.subscriptions.seedOfType(domain.SubscriptionTypeParty, "acme/web")
			fixture.deliveries.claimErr = testCase.claimErr
			fixture.deliveries.partyErr = testCase.partyErr

			delivered, err := fixture.useCase.Next(t.Context(), subscriber)

			require.ErrorIs(t, err, errClaim)
			assert.Nil(t, delivered)
			assert.Equal(t, testCase.wantCalls, fixture.calls.calls)
			assert.Empty(t, fixture.polls.records)
		})
	}
}

func TestPullRequests_NextReturnsPollError(t *testing.T) {
	errPoll := errors.New("database is locked")
	testCases := []struct {
		name   string
		dayOff bool
	}{
		{name: "working day"},
		{name: "day off", dayOff: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newPullRequestsFixture("acme/api")
			fixture.calendar.dayOff = testCase.dayOff
			fixture.polls.err = errPoll

			delivered, err := fixture.useCase.Next(t.Context(), subscriber)

			require.ErrorIs(t, err, errPoll)
			assert.Nil(t, delivered)
		})
	}
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
		assert.Equal(t, tokenID, call.TokenID)
		assert.ElementsMatch(t, []string{"acme/api", "acme/new"}, call.Repositories)
		assert.Equal(t, time.UTC, call.Since.Location())
		assert.True(t, since.Equal(call.Since))
		assert.Empty(t, fixture.pullRequests.receivedCalls)
		assert.Empty(t, fixture.deliveries.claims)
		assert.Empty(t, fixture.deliveries.partyClaims)
		assert.Empty(t, fixture.polls.records)
	})

	t.Run("nothing resolves", func(t *testing.T) {
		fixture := newPullRequestsFixture("old/gone")

		changed, err := fixture.useCase.Since(t.Context(), subscriber, since)

		require.NoError(t, err)
		assert.Empty(t, changed)
		assert.Empty(t, fixture.pullRequests.changedCalls)
		assert.Empty(t, fixture.pullRequests.receivedCalls)
	})
}

func TestPullRequests_SinceOnPartyRepositories(t *testing.T) {
	since := time.Date(2026, 11, 2, 11, 0, 0, 0, time.FixedZone("plus2", 2*60*60))
	early := now.Add(-2 * time.Hour)
	late := now.Add(-time.Hour)
	fixture := newPullRequestsFixture("acme/api")
	fixture.subscriptions.seedOfType(domain.SubscriptionTypeParty, "acme/web")
	fixture.calendar.dayOff = true
	fixture.pullRequests.changed = []domain.PullRequest{{Repository: "acme/api", Number: 2, UpdatedAt: late}}
	fixture.pullRequests.received = []domain.PullRequest{
		{Repository: "acme/web", Number: 1, UpdatedAt: late},
		{Repository: "acme/web", Number: 5, UpdatedAt: early},
	}

	changed, err := fixture.useCase.Since(t.Context(), subscriber, since)

	require.NoError(t, err)
	assert.Equal(t, []domain.PullRequestKey{
		{Repository: "acme/web", Number: 5},
		{Repository: "acme/api", Number: 2},
		{Repository: "acme/web", Number: 1},
	}, pullRequestKeys(changed))
	require.Len(t, fixture.pullRequests.changedCalls, 1)
	assert.Equal(t, []string{"acme/api"}, fixture.pullRequests.changedCalls[0].Repositories)
	require.Len(t, fixture.pullRequests.receivedCalls, 1)
	received := fixture.pullRequests.receivedCalls[0]
	assert.Equal(t, tokenID, received.TokenID)
	assert.Equal(t, []string{"acme/web"}, received.Repositories)
	assert.Equal(t, time.UTC, received.Since.Location())
	assert.True(t, since.Equal(received.Since))
	assert.Empty(t, fixture.polls.records, "a since read is not a poll")
	assert.Empty(t, fixture.calendar.dayOffChecks, "a since read is a recovery read and works on a day off")
	assert.Empty(t, fixture.deliveries.claims)
	assert.Empty(t, fixture.deliveries.partyClaims)
}
