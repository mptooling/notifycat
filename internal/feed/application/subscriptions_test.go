package application_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/feed/application"
	"github.com/mptooling/notifycat/internal/feed/domain"
)

func subscriptionsWithLog(store *fakeSubscriptionStore) (*application.Subscriptions, *bytes.Buffer) {
	logger, buffer := captureLogger()
	return application.NewSubscriptions(domain.SubscriptionsParams{
		Subscriptions: store,
		Catalog:       &fakeCatalog{repositories: []string{"acme/api", "acme/web", "acme/ops"}},
		Clock:         &fakeClock{now: now},
		Logger:        logger,
	}), buffer
}

func repositoriesOf(subscriptions []domain.Subscription) []string {
	repositories := make([]string, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		repositories = append(repositories, subscription.Repository)
	}
	return repositories
}

func TestSubscriptions_Add(t *testing.T) {
	testCases := []struct {
		name         string
		existing     []string
		requested    []string
		wantAdded    []string
		wantListed   []string
		wantAddCalls int
	}{
		{name: "exact repository", requested: []string{"acme/api"}, wantAdded: []string{"acme/api"}, wantListed: []string{"acme/api"}, wantAddCalls: 1},
		{name: "surrounding whitespace trimmed", requested: []string{" acme/api "}, wantAdded: []string{"acme/api"}, wantListed: []string{"acme/api"}, wantAddCalls: 1},
		{name: "all repositories", requested: []string{"*"}, wantAdded: []string{"*"}, wantListed: []string{"*"}, wantAddCalls: 1},
		{name: "stored lower case", requested: []string{"ACME/Api"}, wantAdded: []string{"acme/api"}, wantListed: []string{"acme/api"}, wantAddCalls: 1},
		{name: "duplicates add once", requested: []string{"acme/api", "ACME/API", "acme/api"}, wantAdded: []string{"acme/api"}, wantListed: []string{"acme/api"}, wantAddCalls: 1},
		{name: "already subscribed is kept", existing: []string{"acme/api"}, requested: []string{"acme/api", "acme/web"}, wantAdded: []string{"acme/web"}, wantListed: []string{"acme/api", "acme/web"}, wantAddCalls: 1},
		{name: "only already subscribed", existing: []string{"acme/api"}, requested: []string{"acme/api"}, wantListed: []string{"acme/api"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &fakeSubscriptionStore{}
			store.seed(testCase.existing...)
			subscriptions, _ := subscriptionsWithLog(store)

			listed, err := subscriptions.Add(t.Context(), subscriber, testCase.requested)

			require.NoError(t, err)
			assert.Equal(t, testCase.wantListed, repositoriesOf(listed))
			require.Len(t, store.addCalls, testCase.wantAddCalls)
			if testCase.wantAddCalls == 1 {
				assert.Equal(t, domain.NewSubscriptions{TokenID: tokenID, Repositories: testCase.wantAdded, CreatedAt: now}, store.addCalls[0])
			}
		})
	}
}

func TestSubscriptions_AddRejects(t *testing.T) {
	testCases := []struct {
		name      string
		existing  int
		requested []string
		wantErr   error
		wantAs    *domain.UnknownRepositoriesError
	}{
		{
			name: "unknown repositories", requested: []string{"Other/X", "acme/api", "acme/nope"},
			wantAs: &domain.UnknownRepositoriesError{Repositories: []string{"acme/nope", "other/x"}},
		},
		{name: "empty list", requested: nil, wantErr: domain.ErrNoRepositories},
		{name: "only blank entries", requested: []string{" ", ""}, wantErr: domain.ErrNoRepositories},
		{name: "all repositories counts as one over the limit", existing: 200, requested: []string{"*"}, wantErr: domain.ErrTooManySubscriptions},
		{name: "too many", existing: 199, requested: []string{"acme/api", "acme/web"}, wantErr: domain.ErrTooManySubscriptions},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &fakeSubscriptionStore{}
			for index := range testCase.existing {
				store.seed("acme/existing-" + string(rune('a'+index%26)) + string(rune('a'+index/26)))
			}
			subscriptions, _ := subscriptionsWithLog(store)

			_, err := subscriptions.Add(t.Context(), subscriber, testCase.requested)

			require.Error(t, err)
			if testCase.wantErr != nil {
				require.ErrorIs(t, err, testCase.wantErr)
			}
			if testCase.wantAs != nil {
				var unknown *domain.UnknownRepositoriesError
				require.ErrorAs(t, err, &unknown)
				assert.Equal(t, testCase.wantAs, unknown)
				assert.NotEmpty(t, unknown.Error())
			}
			assert.Empty(t, store.addCalls, "nothing is added when the request is rejected")
		})
	}
}

func TestSubscriptions_AddAcceptsAtTheLimitWhenOneIsAlreadySubscribed(t *testing.T) {
	store := &fakeSubscriptionStore{}
	for index := range 198 {
		store.seed("acme/existing-" + string(rune('a'+index%26)) + string(rune('a'+index/26)))
	}
	store.seed("acme/api")
	subscriptions, _ := subscriptionsWithLog(store)

	listed, err := subscriptions.Add(t.Context(), subscriber, []string{"acme/api", "acme/web"})

	require.NoError(t, err)
	assert.Len(t, listed, domain.MaxSubscriptionsPerToken)
}

func TestSubscriptions_AddAllRepositoriesCountsAsOne(t *testing.T) {
	store := &fakeSubscriptionStore{}
	for index := range 199 {
		store.seed("acme/existing-" + string(rune('a'+index%26)) + string(rune('a'+index/26)))
	}
	subscriptions, _ := subscriptionsWithLog(store)

	listed, err := subscriptions.Add(t.Context(), subscriber, []string{"*"})

	require.NoError(t, err)
	assert.Len(t, listed, domain.MaxSubscriptionsPerToken)
}

func TestSubscriptions_AddLogs(t *testing.T) {
	t.Run("newly added", func(t *testing.T) {
		store := &fakeSubscriptionStore{}
		subscriptions, logs := subscriptionsWithLog(store)

		_, err := subscriptions.Add(t.Context(), subscriber, []string{"acme/api", "acme/web"})

		require.NoError(t, err)
		assert.Contains(t, logs.String(), "feed subscriptions added")
		assert.Contains(t, logs.String(), "user=alice")
		assert.Contains(t, logs.String(), "token_id="+tokenID)
		assert.Contains(t, logs.String(), "count=2")
	})

	t.Run("all already subscribed", func(t *testing.T) {
		store := &fakeSubscriptionStore{}
		store.seed("acme/api")
		subscriptions, logs := subscriptionsWithLog(store)

		_, err := subscriptions.Add(t.Context(), subscriber, []string{"acme/api"})

		require.NoError(t, err)
		assert.NotContains(t, logs.String(), "feed subscriptions added")
	})
}

func TestSubscriptions_List(t *testing.T) {
	store := &fakeSubscriptionStore{}
	store.seed("acme/api")
	subscriptions, _ := subscriptionsWithLog(store)

	listed, err := subscriptions.List(t.Context(), subscriber)

	require.NoError(t, err)
	assert.Equal(t, []string{"acme/api"}, repositoriesOf(listed))
}

func TestSubscriptions_Remove(t *testing.T) {
	t.Run("own subscription", func(t *testing.T) {
		store := &fakeSubscriptionStore{}
		store.seed("acme/api")
		subscriptions, logs := subscriptionsWithLog(store)

		err := subscriptions.Remove(t.Context(), subscriber, 1)

		require.NoError(t, err)
		assert.Contains(t, logs.String(), "feed subscription removed")
		assert.Contains(t, logs.String(), "repository=acme/api")
		assert.Contains(t, logs.String(), "user=alice")
	})

	t.Run("not found", func(t *testing.T) {
		store := &fakeSubscriptionStore{deleteErr: domain.ErrSubscriptionNotFound}
		subscriptions, logs := subscriptionsWithLog(store)

		err := subscriptions.Remove(t.Context(), subscriber, 7)

		require.ErrorIs(t, err, domain.ErrSubscriptionNotFound)
		assert.NotContains(t, logs.String(), "feed subscription removed")
	})

	t.Run("store failure", func(t *testing.T) {
		storeErr := errors.New("boom")
		store := &fakeSubscriptionStore{deleteErr: storeErr}
		subscriptions, _ := subscriptionsWithLog(store)

		err := subscriptions.Remove(t.Context(), subscriber, 7)

		require.ErrorIs(t, err, storeErr)
	})
}

func TestSubscriptions_Available(t *testing.T) {
	subscriptions, _ := subscriptionsWithLog(&fakeSubscriptionStore{})

	available := subscriptions.Available()

	assert.Equal(t, domain.ConfiguredRepositories{Repositories: []string{"acme/api", "acme/web", "acme/ops"}}, available)
}
