package application_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/feed/application"
	"github.com/mptooling/notifycat/internal/feed/domain"
)

func newSubscriptionCleaner(store *fakeSubscriptionStore) *application.SubscriptionCleaner {
	return application.NewSubscriptionCleaner(store, &fakeCatalog{repositories: []string{"acme/api", "acme/new"}})
}

func TestSubscriptionCleaner_EmptyCatalogRemovesNothing(t *testing.T) {
	store := &fakeSubscriptionStore{}
	store.seed("old/gone", domain.AllRepositories)
	cleaner := application.NewSubscriptionCleaner(store, &fakeCatalog{})

	removed, err := cleaner.RemoveUnconfigured(t.Context())

	require.NoError(t, err)
	assert.Equal(t, domain.RemovedSubscriptions{}, removed)
	assert.Empty(t, store.deletedRepositories, "an empty mappings section must not wipe every subscription")
	assert.Len(t, store.subscriptions, 2)
}

func TestSubscriptionCleaner_RemoveUnconfigured(t *testing.T) {
	testCases := []struct {
		name        string
		subscribed  []string
		wantDeleted [][]string
		wantRemoved domain.RemovedSubscriptions
		wantKept    []string
	}{
		{
			name: "unmapped repositories removed", subscribed: []string{"acme/api", "old/gone", "acme/new", "beta/left"},
			wantDeleted: [][]string{{"beta/left", "old/gone"}}, wantRemoved: domain.RemovedSubscriptions{Count: 2, Repositories: []string{"beta/left", "old/gone"}}, wantKept: []string{"acme/api", "acme/new"},
		},
		{
			name: "all repositories subscription kept", subscribed: []string{domain.AllRepositories, "old/gone"},
			wantDeleted: [][]string{{"old/gone"}}, wantRemoved: domain.RemovedSubscriptions{Count: 1, Repositories: []string{"old/gone"}}, wantKept: []string{domain.AllRepositories},
		},
		{name: "every repository mapped", subscribed: []string{"acme/api", "acme/new"}, wantKept: []string{"acme/api", "acme/new"}},
		{name: "nothing subscribed"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &fakeSubscriptionStore{}
			store.seed(testCase.subscribed...)

			removed, err := newSubscriptionCleaner(store).RemoveUnconfigured(t.Context())

			require.NoError(t, err)
			assert.Equal(t, testCase.wantRemoved, removed)
			assert.Equal(t, testCase.wantDeleted, store.deletedRepositories)
			var kept []string
			for _, subscription := range store.subscriptions {
				kept = append(kept, subscription.Repository)
			}
			assert.Equal(t, testCase.wantKept, kept)
		})
	}
}

func TestSubscriptionCleaner_RemoveUnconfiguredErrors(t *testing.T) {
	testCases := []struct {
		name     string
		store    *fakeSubscriptionStore
		wantWrap string
	}{
		{name: "listing fails", store: &fakeSubscriptionStore{repositoriesErr: errStore}, wantWrap: "list subscribed repositories"},
		{name: "delete fails", store: &fakeSubscriptionStore{deleteRepositoriesErr: errStore}, wantWrap: "delete unconfigured subscriptions"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			testCase.store.seed("old/gone")

			_, err := newSubscriptionCleaner(testCase.store).RemoveUnconfigured(t.Context())

			require.ErrorIs(t, err, errStore)
			assert.ErrorContains(t, err, testCase.wantWrap)
		})
	}
}
